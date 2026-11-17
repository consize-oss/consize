package prometheus

import (
	"context"
	"fmt"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/consize-oss/consize/pkg/evidence"
)

func TestCatalogCollectionThroughRealPrometheus(t *testing.T) {
	binary := os.Getenv("CONSIZE_PROMETHEUS_BIN")
	if binary == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("CI requires pinned Prometheus")
		}
		t.Skip("set CONSIZE_PROMETHEUS_BIN from the pinned installer")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	began := time.Now()
	labels := `cluster="fixture-cluster",namespace="test",pod="test-rs-pod",uid="fixture-pod-uid",container="app",deployment="test",deployment_uid="fixture-deployment-uid"`
	exporter := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		elapsed := time.Since(began).Seconds()
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		declared := map[string]bool{}
		for _, m := range []struct {
			name, kind, extra string
			value             float64
		}{
			{"kube_pod_container_info", "gauge", "", 1},
			{"container_cpu_usage_seconds_total", "counter", "", elapsed * .5},
			{"container_memory_working_set_bytes", "gauge", "", 2097152},
			{"container_cpu_cfs_throttled_periods_total", "counter", "", elapsed},
			{"container_cpu_cfs_periods_total", "counter", "", elapsed * 10},
			{"kube_pod_container_status_restarts_total", "counter", "", 3},
			{"container_oom_events_total", "counter", "", 0},
			{"kube_pod_status_phase", "gauge", `,phase="Running"`, 1},
			{"kube_pod_container_resource_requests", "gauge", `,resource="cpu",unit="core"`, .25},
			{"kube_pod_container_resource_requests", "gauge", `,resource="memory",unit="byte"`, 134217728},
			{"kube_pod_container_resource_limits", "gauge", `,resource="cpu",unit="core"`, 1},
			{"kube_pod_container_resource_limits", "gauge", `,resource="memory",unit="byte"`, 268435456},
		} {
			if !declared[m.name] {
				fmt.Fprintf(w, "# TYPE %s %s\n", m.name, m.kind)
				declared[m.name] = true
			}
			fmt.Fprintf(w, "%s{%s%s} %.9f\n", m.name, labels, m.extra, m.value)
		}
	}))
	defer exporter.Close()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	dir := t.TempDir()
	config := filepath.Join(dir, "prometheus.yml")
	body := "global:\n  scrape_interval: 1s\n  scrape_timeout: 500ms\nscrape_configs:\n  - job_name: fixture\n    static_configs:\n      - targets: [\"" + strings.TrimPrefix(exporter.URL, "http://") + "\"]\n"
	if err = os.WriteFile(config, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(ctx, binary, "--config.file="+config, "--storage.tsdb.path="+filepath.Join(dir, "data"), "--web.listen-address="+address, "--storage.tsdb.wal-segment-size=16MB", "--storage.tsdb.retention.size=32MB")
	log, err := os.OpenFile(filepath.Join(dir, "server.log"), os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	command.Stdout = log
	command.Stderr = log
	if err = command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = command.Process.Kill(); _ = command.Wait() })
	client, err := NewConfiguredHTTPClient("http://"+address, TransportConfig{})
	if err != nil {
		t.Fatal(err)
	}
	c, _ := resolveMapping(MappingConfig{})
	scope := mappingMatcher(c.Cluster, "=", "fixture-cluster") + "," + mappingMatcher(c.Namespace, "=", "test")
	query := scopedMappingQueries(c, scope)["cpu_cores"]
	ready := false
	firstMetric := time.Time{}
	for i := 0; i < 300; i++ {
		result, e := client.QueryInstant(ctx, query, time.Now().UTC())
		if e == nil && len(result.Series) > 0 {
			if firstMetric.IsZero() {
				firstMetric = time.Now()
			}
			if time.Since(firstMetric) > 4*time.Second {
				ready = true
				break
			}
		}
		select {
		case <-ctx.Done():
			t.Fatal("isolated workload scrape did not become ready")
		case <-time.After(100 * time.Millisecond):
		}
	}
	if !ready {
		t.Fatal("isolated workload scrape never produced CPU evidence")
	}
	// A short-lookback override keeps this fixture bounded; the default 5m query
	// is evaluated against full histories in the promtool catalog fixtures.
	shortCPU := strings.ReplaceAll(query, "[5m]", "[2s]")
	p, err := New(Config{Units: map[string]string{"cpu_cores": "cores"}, Queries: map[string]string{"cpu_cores": shortCPU}, BaseURL: "http://" + address, Window: 2 * time.Second, Step: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := p.ReadMetrics(ctx, collectionResource())
	if err != nil {
		t.Fatal(err)
	}
	if err = snapshot.ValidateRecords(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		signal           string
		value, tolerance float64
	}{{"cpu_cores_p95", .5, .05}, {"memory_working_set_bytes_p95", 2097152, 0}, {"cpu_request_cores_current", .25, 0}, {"cpu_limit_cores_current", 1, 0}, {"memory_request_bytes_current", 134217728, 0}, {"memory_limit_bytes_current", 268435456, 0}, {"restarts_30m_max", 0, 0}} {
		record := findRecord(t, snapshot, tc.signal)
		if record.Quality != evidence.Valid || record.Value == nil || math.Abs(*record.Value-tc.value) > tc.tolerance {
			t.Fatalf("real source %s: quality=%s value=%v provenance=%v", tc.signal, record.Quality, record.Value, record.Extensions)
		}
		if record.Extensions["query_version"] == "" || record.Extensions["aggregation_version"] != AggregationVersion {
			t.Fatal("real source provenance incomplete")
		}
	}
	t.Log("known isolated workload: CPU, memory, requests, limits, restart delta and versioned provenance passed")
}
