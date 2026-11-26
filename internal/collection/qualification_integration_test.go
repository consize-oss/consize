package collection_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/consize-oss/consize/internal/collection"
	"github.com/consize-oss/consize/internal/discovery"
	"github.com/consize-oss/consize/internal/store"
	"github.com/consize-oss/consize/pkg/evidence"
	"github.com/consize-oss/consize/pkg/evidence/quality"
	"github.com/consize-oss/consize/pkg/plugin"
	k8splugin "github.com/consize-oss/consize/pkg/plugins/kubernetes"
	prom "github.com/consize-oss/consize/pkg/plugins/prometheus"
	"github.com/consize-oss/consize/pkg/resource"
)

type evidenceQualificationReport struct {
	CleanupVerified bool                    `json:"cleanup_verified"`
	Version         string                  `json:"version"`
	Passed          bool                    `json:"passed"`
	Source          string                  `json:"source"`
	Steps           []string                `json:"steps"`
	Snapshot        *plugin.MetricsSnapshot `json:"snapshot,omitempty"`
}

// Native cgroup identity is parsed from the kubelet's actual series, then joined
// against API-observed UID membership. Pod/deployment names never supply a UID.
func evidenceQueries(cpuLookback string) map[string]string {
	selector := `{scope},container!="",container!="POD"`

	numerator := fmt.Sprintf(`rate(container_cpu_cfs_throttled_periods_total{%s}[5m])`, selector)
	denominator := fmt.Sprintf(`rate(container_cpu_cfs_periods_total{%s}[5m])`, selector)
	return map[string]string{"cpu_cores": enrichEvidence(fmt.Sprintf(`rate(container_cpu_usage_seconds_total{%s}[%s])`, selector, cpuLookback)), "memory_working_set_bytes": enrichEvidence(fmt.Sprintf(`container_memory_working_set_bytes{%s}`, selector)), "cpu_throttling_ratio": enrichEvidence(fmt.Sprintf(`(((%s)/(%s)) and ((%s)>0)) or (0*(((%s)==0) and ((%s)==0)))`, numerator, denominator, denominator, numerator, denominator)), "oom_events_30m": enrichEvidence(fmt.Sprintf(`increase(container_oom_events_total{%s}[30m])`, selector))}
}
func enrichEvidence(expression string) string {
	return fmt.Sprintf(`label_replace((%s),"uid","$1-$2-$3-$4-$5","id",".*pod([0-9a-f]{8})[-_]([0-9a-f]{4})[-_]([0-9a-f]{4})[-_]([0-9a-f]{4})[-_]([0-9a-f]{12})([.]slice)?(/.*)?") * on(cluster,namespace,pod,uid,container) group_left(deployment,deployment_uid) kube_pod_container_info{{scope}}`, expression)
}
func liveObservationWindow() time.Duration {
	if os.Getenv("CONSIZE_EVIDENCE_LONG_PROFILE") == "1" {
		return 5 * time.Minute
	}
	return 2 * time.Minute
}
func evidenceSourceQueries() map[string]string {
	selector := `{scope},container!="",container!="POD"`
	phase := `kube_pod_status_phase{{scope},phase=~"Running|Pending|Succeeded"}`
	return map[string]string{
		"cpu_cores":                enrichEvidence(fmt.Sprintf(`timestamp(container_cpu_usage_seconds_total{%s})`, selector)),
		"memory_working_set_bytes": enrichEvidence(fmt.Sprintf(`timestamp(container_memory_working_set_bytes{%s})`, selector)),
		"cpu_throttling_ratio":     enrichEvidence(fmt.Sprintf(`timestamp(container_cpu_cfs_periods_total{%s}) and timestamp(container_cpu_cfs_throttled_periods_total{%s})`, selector, selector)),
		"oom_events_30m":           enrichEvidence(fmt.Sprintf(`timestamp(container_oom_events_total{%s})`, selector)),
		"restarts_30m":             fmt.Sprintf(`timestamp(kube_pod_container_status_restarts_total{%s})`, selector),
		"evicted_pods":             fmt.Sprintf(`timestamp(%s) and (%s == 1)`, phase, phase),
	}
}
func evidenceConfig(address string) prom.Config {
	return prom.Config{BaseURL: address, Window: liveObservationWindow(), SourceTimestampQueries: evidenceSourceQueries(), SourceScrapeInterval: 3 * time.Second, Step: 10 * time.Second, MinimumSamples: 2, Queries: evidenceQueries("5m"), Units: map[string]string{"cpu_cores": "cores", "memory_working_set_bytes": "bytes", "cpu_throttling_ratio": "ratio", "oom_events_30m": "count"}, Transport: prom.TransportConfig{RequestTimeout: "5s", QueryTimeout: "3s", MaxResponseBytes: 8 << 20}}
}
func evidenceProvider(t *testing.T, cfg prom.Config) *prom.Plugin {
	t.Helper()
	p, err := prom.New(cfg)
	if err != nil {
		t.Fatal("construct qualification metrics provider", err)
	}
	return p
}
func evidenceKeys() []string {
	return []string{"cpu_cores_p95", "memory_working_set_bytes_p95", "cpu_request_cores_current", "cpu_limit_cores_current", "memory_request_bytes_current", "memory_limit_bytes_current", "restarts_30m_p95", "cpu_throttling_ratio_p95", "oom_events_30m_p95", "evicted_pods_p95"}
}
func completeEvidence(snapshot plugin.MetricsSnapshot) bool {
	if snapshot.ValidateRecords() != nil {
		return false
	}
	records := map[string]evidence.Record{}
	for _, r := range snapshot.Records {
		records[r.Signal] = r
	}
	for _, key := range evidenceKeys() {
		r, ok := records[key]
		if !ok || r.Quality != evidence.Valid || r.Value == nil {
			return false
		}
	}
	return true
}
func readLive(t *testing.T, ctx context.Context, p *prom.Plugin, target resource.Resource) plugin.MetricsSnapshot {
	t.Helper()
	end := time.Now().UTC().Add(-time.Second).Truncate(time.Millisecond)
	snapshot, err := p.ReadMetricsBetween(ctx, target, end.Add(-liveObservationWindow()), end)
	if err != nil {
		t.Fatal("read real qualification evidence", err)
	}
	if snapshot.ValidateRecords() != nil {
		t.Fatal("real source returned invalid provenance")
	}
	return snapshot
}
func reportingAssessment(snapshot plugin.MetricsSnapshot, at time.Time) quality.Assessment {
	thresholds, _ := quality.Resolve(quality.Config{}, quality.Reporting, false, false)
	return snapshot.AssessQuality([]quality.Signal{{Name: "cpu_cores_p95", Unit: "cores"}, {Name: "memory_working_set_bytes_p95", Unit: "bytes"}}, quality.Reporting, thresholds, at)
}

func TestIsolatedEvidencePipelineQualification(t *testing.T) {
	if os.Getenv("CONSIZE_EVIDENCE_TEST_FIXTURES") != "1" {
		if os.Getenv("CI") != "" {
			t.Fatal("CI requires isolated real-source evidence qualification")
		}
		t.Skip("run scripts/test/evidence.sh for actual Kubernetes/kubelet-to-Prometheus qualification")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	report := evidenceQualificationReport{Version: "evidence-qualification-v1", Source: "actual kubelet cAdvisor and Kubernetes ownership/spec/status APIs", Steps: []string{}}
	var source *liveEvidenceSource
	t.Cleanup(func() {
		report.CleanupVerified = source != nil && source.cleaned
		report.Passed = !t.Failed() && report.CleanupVerified
		if path := os.Getenv("CONSIZE_EVIDENCE_QUALIFICATION_REPORT"); path != "" {
			raw, _ := json.MarshalIndent(report, "", "  ")
			if err := os.WriteFile(path, append(raw, '\n'), 0600); err != nil {
				t.Error("write qualification report")
			}
		}
	})
	entropy := make([]byte, 6)
	if _, err := rand.Read(entropy); err != nil {
		t.Fatal(err)
	}
	suffix := hex.EncodeToString(entropy)
	source = liveSource(t, ctx, suffix)
	report.Steps = append(report.Steps, "two namespaces, two replicas and two containers running actual CPU/memory workloads")
	server := newQualificationPrometheus(t, source)
	if err := server.start(ctx); err != nil {
		t.Fatal("start pinned Prometheus")
	}
	cfg := evidenceConfig("http://" + server.address)
	p := evidenceProvider(t, cfg)
	client, err := prom.NewConfiguredHTTPClient(cfg.BaseURL, prom.TransportConfig{})
	if err != nil {
		t.Fatal(err)
	}
	waitLive(t, ctx, "Prometheus startup", func() bool { return client.Ready(ctx) == nil })
	path := filepath.Join(t.TempDir(), "state.json")
	st, err := store.OpenDurable(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	kube, err := k8splugin.New(k8splugin.Config{Kubeconfig: source.kubeconfig, Context: source.contextName, ClusterID: source.cluster, Location: "kind", Discovery: k8splugin.DiscoveryConfig{Namespaces: source.namespaces, LabelSelector: source.selector}})
	if err != nil {
		t.Fatal("construct scoped discovery")
	}
	manager := plugin.NewManager()
	if err := manager.RegisterDiscovery(kube); err != nil {
		t.Fatal(err)
	}
	if err := manager.RegisterMetrics(p); err != nil {
		t.Fatal(err)
	}
	if _, err := discovery.New(st, manager).Run(ctx); err != nil {
		t.Fatal("discover actual qualification Deployments", err)
	}
	resources, err := st.ListResources(ctx)
	if err != nil || len(resources) != 2 || resources[0].ID == resources[1].ID {
		t.Fatal("same-name discovered resources were not separated", err)
	}
	target := resources[0]
	report.Steps = append(report.Steps, "actual Deployment discovery produced separate canonical identities")
	warmup, stopWarmup := context.WithTimeout(ctx, liveObservationWindow()+90*time.Second)
	defer stopWarmup()
	var baseline plugin.MetricsSnapshot
	waitLive(t, warmup, "all MVP signals from actual samples", func() bool {
		end := time.Now().UTC().Add(-time.Second).Truncate(time.Millisecond)
		snapshot, e := p.ReadMetricsBetween(warmup, target, end.Add(-liveObservationWindow()-15*time.Second), end)
		if e != nil {
			return false
		}
		baseline = snapshot
		report.Snapshot = &baseline
		return completeEvidence(snapshot)
	})
	report.Snapshot = &baseline
	report.Steps = append(report.Steps, "all ten MVP signals have measured values, units, coverage and sealed provenance")
	t.Run("valid provenance and scope", func(t *testing.T) {
		for _, r := range resources {
			snapshot := readLive(t, ctx, p, r)
			if !completeEvidence(snapshot) {
				t.Fatal("incomplete real-source snapshot")
			}
			for _, record := range snapshot.Records {
				if record.ResourceID != r.ID || record.PluginID != p.ID() || record.Extensions["query_version"] == "" || record.Extensions["mapping_cluster"] != source.cluster || record.Extensions["mapping_deployment_uid"] != r.Metadata["uid"] || record.SampleCount < 1 {
					t.Fatal("missing real-source attribution", record.Signal)
				}
				if record.Value != nil && (math.IsNaN(*record.Value) || math.IsInf(*record.Value, 0)) {
					t.Fatal("non-finite measured value")
				}
			}
			for _, key := range []string{"cpu_cores_p95", "memory_working_set_bytes_p95"} {
				value, ok := snapshot.Signals[key].(float64)
				if !ok || value <= 0 {
					t.Fatal("workload usage was not measured", key)
				}
			}
			if !reportingAssessment(snapshot, snapshot.CollectedAt).Eligible {
				raw, _ := json.Marshal(reportingAssessment(snapshot, snapshot.CollectedAt))
				t.Fatal("complete live evidence failed reporting gates", string(raw))
			}
		}
	})
	t.Run("fractional window", func(t *testing.T) {
		end := time.Now().UTC().Add(-time.Second).Truncate(time.Millisecond).Add(750 * time.Microsecond)
		start := end.Add(-liveObservationWindow())
		snapshot, err := p.ReadMetricsBetween(ctx, target, start, end)
		if err != nil || snapshot.ValidateRecords() != nil {
			t.Fatal("fractional interval produced invalid evidence", err)
		}
		for _, record := range snapshot.Records {
			if record.WindowStart.Before(start) || record.WindowEnd.After(end) || record.WindowStart.Nanosecond()%1000000 != 0 || record.WindowEnd.Nanosecond()%1000000 != 0 {
				t.Fatal("fractional bounds escaped the requested window")
			}
		}
	})
	t.Run("stale actual snapshot", func(t *testing.T) {
		snapshot := readLive(t, ctx, p, target)
		latest := snapshot.CollectedAt
		for _, r := range snapshot.Records {
			if r.FreshUntil.After(latest) {
				latest = r.FreshUntil
			}
		}
		delay := time.Until(latest.Add(100 * time.Millisecond))
		if delay > 0 {
			timer := time.NewTimer(delay)
			defer timer.Stop()
			select {
			case <-timer.C:
			case <-ctx.Done():
				t.Fatal("staleness observation interrupted")
			}
		}
		if reportingAssessment(snapshot, time.Now().UTC()).Eligible {
			t.Fatal("naturally expired evidence remained eligible")
		}
	})
	t.Run("missing metrics", func(t *testing.T) {
		missing := cfg
		missing.Queries = evidenceQueries("5m")
		missing.Queries["memory_working_set_bytes"] = `container_memory_working_set_bytes{{scope},pod="fixture-nonexistent"}`
		snapshot := readLive(t, ctx, evidenceProvider(t, missing), target)
		for _, r := range snapshot.Records {
			if strings.HasPrefix(r.Signal, "memory_working_set_bytes_") && r.Value != nil {
				t.Fatal("missing memory fabricated a measured value")
			}
		}
		if reportingAssessment(snapshot, snapshot.CollectedAt).Eligible {
			t.Fatal("missing source passed mandatory gates")
		}
	})
	t.Run("sparse source", func(t *testing.T) {
		sparse := cfg
		sparse.Queries = evidenceQueries("4s")
		snapshot := readLive(t, ctx, evidenceProvider(t, sparse), target)
		if reportingAssessment(snapshot, snapshot.CollectedAt).Eligible {
			t.Fatal("a four-second CPU rate window over three-second scrapes passed coverage gates")
		}
	})
	t.Run("durable scheduled collection and restart", func(t *testing.T) {
		settings := collection.Settings{Config: collection.Config{Enabled: true, PluginID: p.ID(), Interval: "10s", Window: cfg.Window.String(), MaxJobsPerTick: 2}, ProviderConfig: cfg}
		collector, err := collection.New(st, st, manager, settings)
		if err != nil {
			t.Fatal(err)
		}
		if err := collector.Tick(ctx); err != nil {
			t.Fatal(err)
		}
		jobs, err := st.ListCollections(ctx, store.CollectionQuery{ResourceID: target.ID, Limit: 100})
		if err != nil || len(jobs) == 0 {
			t.Fatal("scheduler produced no durable collection", err)
		}
		captured := jobs[0]
		waitLive(t, ctx, "scheduled collection", func() bool {
			if err := collector.Tick(ctx); err != nil {
				t.Fatal(err)
			}
			job, e := st.GetCollection(ctx, captured.ID)
			if e == nil && store.CollectionTerminal(job.State) && job.State != "succeeded" {
				raw, _ := json.Marshal(job.Assessment)
				t.Fatal("scheduled collection rejected live evidence", job.State, job.ErrorCode, string(raw))
			}
			return e == nil && job.State == "succeeded"
		})
		manual, err := collector.Request(ctx, target.ID, "qualification", captured.WindowEnd)
		if err != nil || manual.ID != captured.ID {
			t.Fatal("manual and scheduled windows duplicated", err)
		}
		restartEnd := time.Now().UTC().Add(-time.Second).Truncate(time.Millisecond)
		pending, err := collector.Request(ctx, target.ID, "qualification", restartEnd)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = st.ClaimCollection(ctx, pending.ID, "qualification-interrupted", time.Second); err != nil {
			t.Fatal(err)
		}
		if err = st.Close(); err != nil {
			t.Fatal(err)
		}
		st, err = store.OpenDurable(path)
		if err != nil {
			t.Fatal(err)
		}
		collector, err = collection.New(st, st, manager, settings)
		if err != nil {
			t.Fatal(err)
		}
		waitLive(t, ctx, "restart resumes one captured collection", func() bool {
			if err := collector.Tick(ctx); err != nil {
				t.Fatal(err)
			}
			job, e := st.GetCollection(ctx, pending.ID)
			if e == nil && store.CollectionTerminal(job.State) && job.State != "succeeded" {
				raw, _ := json.Marshal(job.Assessment)
				t.Fatal("scheduled collection rejected live evidence", job.State, job.ErrorCode, string(raw))
			}
			return e == nil && job.State == "succeeded" && job.Attempts == 2
		})
		rows, err := st.ListResources(ctx)
		if err != nil || len(rows) != 2 {
			t.Fatal("restart changed inventory identities")
		}
	})
	t.Run("query and access failures", func(t *testing.T) {
		for _, mode := range []string{"query", "access"} {
			t.Run(mode, func(t *testing.T) {
				broken := cfg
				expected := "provider_query"
				fixtureAllowed := "fixture-allowed-" + suffix
				fixtureRejected := "fixture-rejected-" + suffix
				if mode == "query" {
					broken.Queries = evidenceQueries("5m")
					broken.Queries["cpu_cores"] = "sum("
				} else {
					targetURL, _ := url.Parse(cfg.BaseURL)
					proxy := httputil.NewSingleHostReverseProxy(targetURL)
					auth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						if r.Header.Get("Authorization") != "Bearer "+fixtureAllowed {
							http.Error(w, "not authorized", 401)
							return
						}
						proxy.ServeHTTP(w, r)
					}))
					defer auth.Close()
					t.Setenv("CON20_QUALIFICATION_AUTH_ENV", fixtureRejected)
					broken.BaseURL = auth.URL
					broken.Transport.Auth = prom.AuthConfig{BearerTokenEnv: "CON20_QUALIFICATION_AUTH_ENV"}
					expected = "provider_access"
				}
				failing := evidenceProvider(t, broken)
				pm := plugin.NewManager()
				if err := pm.RegisterMetrics(failing); err != nil {
					t.Fatal(err)
				}
				disk := store.NewMemory()
				if _, err := disk.UpsertResource(ctx, target); err != nil {
					t.Fatal(err)
				}
				collector, err := collection.New(disk, disk, pm, collection.Settings{Config: collection.Config{PluginID: failing.ID(), Window: cfg.Window.String()}, ProviderConfig: broken})
				if err != nil {
					t.Fatal(err)
				}
				end := time.Now().UTC().Add(-time.Second).Truncate(time.Millisecond)
				job, err := collector.Request(ctx, target.ID, "qualification", end)
				if err != nil {
					t.Fatal(err)
				}
				if err = collector.Tick(ctx); err != nil {
					t.Fatal(err)
				}
				done, err := disk.GetCollection(ctx, job.ID)
				if err != nil || done.State != "failed" || done.ErrorCode != expected || done.Snapshot != nil {
					t.Fatal("failure became measured evidence", done.State, done.ErrorCode, err)
				}
				raw, _ := json.Marshal(done)
				if strings.Contains(string(raw), fixtureAllowed) || strings.Contains(string(raw), fixtureRejected) {
					t.Fatal("credential leaked into durable diagnostics")
				}
			})
		}
	})
	t.Run("actual Prometheus outage and recovery", func(t *testing.T) {
		disk := store.NewMemory()
		if _, err := disk.UpsertResource(ctx, target); err != nil {
			t.Fatal(err)
		}
		collector, err := collection.New(disk, disk, manager, collection.Settings{Config: collection.Config{PluginID: p.ID(), Window: cfg.Window.String()}, ProviderConfig: cfg})
		if err != nil {
			t.Fatal(err)
		}
		end := time.Now().UTC().Add(-time.Second).Truncate(time.Millisecond)
		job, err := collector.Request(ctx, target.ID, "qualification", end)
		if err != nil {
			t.Fatal(err)
		}
		server.stop()
		if err = collector.Tick(ctx); err != nil {
			t.Fatal(err)
		}
		failed, err := disk.GetCollection(ctx, job.ID)
		if err != nil || failed.State != "retry_wait" || failed.Snapshot != nil {
			t.Fatal("outage fabricated a snapshot", failed.State, err)
		}
		if err := server.start(ctx); err != nil {
			t.Fatal("restart real Prometheus")
		}
		waitLive(t, ctx, "same real-source collection recovers after server restart", func() bool {
			if err := collector.Tick(ctx); err != nil {
				t.Fatal(err)
			}
			done, e := disk.GetCollection(ctx, job.ID)
			if e == nil && store.CollectionTerminal(done.State) && done.State != "succeeded" {
				raw, _ := json.Marshal(done.Assessment)
				t.Fatal("recovered source rejected evidence", done.State, done.ErrorCode, string(raw))
			}
			return e == nil && done.State == "succeeded" && done.ID == job.ID
		})
	})
	report.Steps = append(report.Steps, "namespace/replica/container mapping, fractional windows, natural staleness, sparse/missing data, durable restart/deduplication, access/query failures and real server outage/recovery")
}
