package prometheus

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// This starts an isolated official server with no external scrape targets, no
// production configuration and a private temporary TSDB. CI must supply the
// checksum-verified pinned binary; workstation runs can opt in with the same path.
func TestPrometheusServerIntegration(t *testing.T) {
	binary := os.Getenv("CONSIZE_PROMETHEUS_BIN")
	if binary == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("CI requires the pinned Prometheus test binary")
		}
		t.Skip("set CONSIZE_PROMETHEUS_BIN from scripts/ci/install-prometheus-test.sh")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	dir := t.TempDir()
	config := filepath.Join(dir, "prometheus.yml")
	if err := os.WriteFile(config, []byte("global:\n  scrape_interval: 1s\nscrape_configs: []\n"), 0600); err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(ctx, binary, "--config.file="+config, "--storage.tsdb.path="+filepath.Join(dir, "data"), "--web.listen-address="+address, "--storage.tsdb.wal-segment-size=16MB", "--storage.tsdb.retention.size=32MB")
	logPath := filepath.Join(dir, "server.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer logFile.Close()
	command.Stdout = logFile
	command.Stderr = logFile
	if err = command.Start(); err != nil {
		t.Fatal("start isolated Prometheus:", err)
	}
	t.Cleanup(func() { _ = command.Process.Kill(); _ = command.Wait() })
	client, err := NewConfiguredHTTPClient("http://"+address, TransportConfig{RequestTimeout: "1s"})
	if err != nil {
		t.Fatal(err)
	}
	ready := false
	for i := 0; i < 100; i++ {
		if client.Ready(ctx) == nil {
			ready = true
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("Prometheus readiness deadline")
		case <-time.After(50 * time.Millisecond):
		}
	}
	if !ready {
		_ = command.Process.Kill()
		_ = command.Wait()
		output, _ := os.ReadFile(logPath)
		if len(output) > 8192 {
			output = output[len(output)-8192:]
		}
		t.Fatalf("Prometheus did not become ready: %s", output)
	}
	fractionalTimeout, err := NewConfiguredHTTPClient("http://"+address, TransportConfig{RequestTimeout: "2s", QueryTimeout: "1.5s"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = fractionalTimeout.QueryInstant(ctx, "1", time.Now()); err != nil {
		t.Fatal("fractional query timeout rejected:", err)
	}
	scalar, err := client.QueryInstant(ctx, "1", time.Now())
	if err != nil || scalar.Scalar == nil || scalar.Scalar.Value != 1 {
		t.Fatalf("real instant query failed: %+v %v", scalar, err)
	}
	cfg, _ := resolveMapping(MappingConfig{})
	queries := scopedMappingQueries(cfg, `cluster="test",namespace="test"`)
	queries["inventory"] = mappingInventoryQuery(`cluster="test",namespace="test"`)
	for signal, query := range queries {
		if _, err := client.QueryRange(ctx, query, time.Now().Add(-time.Minute), time.Now(), time.Second); err != nil {
			t.Fatalf("real mapping query %s failed: %v", signal, err)
		}
	}
	end := time.Now().UTC()
	start := end.Add(-10 * time.Second)
	result, err := client.QueryRangeResult(ctx, "vector(1)", start, end, 250*time.Millisecond)
	if err != nil || len(result.Series) != 1 || len(result.Series[0].Points) < 39 {
		t.Fatalf("real range query failed: %+v %v", result, err)
	}
	for _, point := range result.Series[0].Points {
		if point.Value != 1 || point.Timestamp.Before(start) || point.Timestamp.After(end) {
			t.Fatal("invalid real sample")
		}
	}
	_, err = client.QueryInstant(ctx, "invalid query (", time.Now())
	requireCode(t, err, "query")
	t.Log(fmt.Sprintf("official Prometheus: instant, fractional range, readiness and query-error classification passed (%d points)", len(result.Series[0].Points)))
}
