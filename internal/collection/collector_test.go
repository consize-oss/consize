package collection

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/consize-oss/consize/internal/audit"
	"github.com/consize-oss/consize/internal/store"
	"github.com/consize-oss/consize/pkg/evidence"
	"github.com/consize-oss/consize/pkg/evidence/quality"
	"github.com/consize-oss/consize/pkg/plugin"
	"github.com/consize-oss/consize/pkg/plugins/prometheus"
	"github.com/consize-oss/consize/pkg/resource"
)

type fixtureProvider struct {
	calls atomic.Int32
	err   error
	mode  string
}

func (*fixtureProvider) ID() string { return "fixture-metrics" }
func (*fixtureProvider) Manifest() plugin.Manifest {
	return plugin.Manifest{ID: "fixture-metrics", Version: "1", Category: plugin.CategoryMetrics, Capabilities: []string{plugin.CapabilityMetricsRead}, SupportedResourceTypes: []string{resource.TypeKubernetesDeployment}}
}
func (*fixtureProvider) Health(context.Context) plugin.Health {
	return plugin.Health{Status: "healthy"}
}
func (p *fixtureProvider) ReadMetrics(ctx context.Context, r resource.Resource) (plugin.MetricsSnapshot, error) {
	return p.ReadMetricsBetween(ctx, r, time.Now().Add(-2*time.Minute), time.Now())
}
func (p *fixtureProvider) ReadMetricsBetween(ctx context.Context, r resource.Resource, start, end time.Time) (plugin.MetricsSnapshot, error) {
	p.calls.Add(1)
	if p.mode == "block" {
		<-ctx.Done()
		return plugin.MetricsSnapshot{}, ctx.Err()
	}
	if p.err != nil {
		return plugin.MetricsSnapshot{Signals: map[string]any{"cpu_cores_p95": 0.}}, p.err
	}
	now := time.Now().UTC()
	s := plugin.MetricsSnapshot{PluginID: p.ID(), ResourceID: r.ID, Window: end.Sub(start).String(), CollectedAt: now, Signals: map[string]any{}}
	count := int(end.Sub(start)/time.Minute) + 1
	contextJSON, _ := quality.EncodeContext(quality.Context{Mapping: "exact", FirstObservationAt: &start, LastObservationAt: &end, SourceMaxGap: "1m", ScrapeInterval: "1m", ObservedSamples: &count})
	for _, signal := range required {
		if p.mode == "missing" && signal == "memory_limit_bytes_current" {
			continue
		}
		unit := "count"
		if strings.Contains(signal, "cpu_") {
			unit = "cores"
		}
		if strings.Contains(signal, "bytes") {
			unit = "bytes"
		}
		if strings.Contains(signal, "ratio") {
			unit = "ratio"
		}
		stat := "p95"
		sampleCount := count
		if strings.HasSuffix(signal, "_current") {
			stat = "last"
			sampleCount = 1
		}
		value := .5
		qual := evidence.Valid
		if p.mode == "partial" && signal == "cpu_cores_p95" {
			qual = evidence.Partial
		}
		rec, err := evidence.Seal(evidence.Record{ResourceID: r.ID, PluginID: p.ID(), Signal: signal, Query: "fixture_query", Kind: evidence.Aggregated, Aggregation: stat, Unit: unit, OriginalUnit: unit, Scale: 1, Value: evidence.Number(value), SampleCount: sampleCount, WindowStart: start, WindowEnd: end, CollectedAt: now, FreshUntil: now.Add(time.Minute), Quality: qual, Extensions: map[string]string{"step": "1m", "expected_points": fmtInt(count), "actual_points": fmtInt(count), "first_sample": start.Format(time.RFC3339Nano), "last_sample": end.Format(time.RFC3339Nano), "max_gap": "1m", quality.ContextKey: contextJSON}})
		if err != nil {
			return s, err
		}
		s.Records = append(s.Records, rec)
		s.Signals[signal] = value
	}
	if p.mode == "negative_current" {
		for i, r := range s.Records {
			if r.Signal == "cpu_request_cores_current" {
				r.Value = evidence.Number(-1)
				r, _ = evidence.Seal(r)
				s.Records[i] = r
				s.Signals[r.Signal] = -1.
			}
		}
	}
	if p.mode == "wrong_unit" {
		r := s.Records[0]
		r.Unit = "bytes"
		r.OriginalUnit = "bytes"
		r, _ = evidence.Seal(r)
		s.Records[0] = r
	}
	if p.mode == "forged_projection" {
		s.Signals[s.Records[0].Signal] = 999.
	}
	if p.mode == "invalid" {
		s.Records[0].Query = "tampered"
	}
	if p.mode == "wrong_window" {
		for i := range s.Records {
			rec := s.Records[i]
			rec.WindowStart = start.Add(-time.Second)
			rec.WindowEnd = end.Add(-time.Second)
			rec, _ = evidence.Seal(rec)
			s.Records[i] = rec
		}
	}
	return s, nil
}
func fmtInt(n int) string { b, _ := json.Marshal(n); return string(b) }
func fixture(t *testing.T, p *fixtureProvider, cfg Config) (*Collector, *store.Memory, resource.Resource) {
	t.Helper()
	st := store.NewMemory()
	r, err := st.UpsertResource(context.Background(), resource.Resource{ID: "fixture-resource", Provider: resource.ProviderKubernetes, Type: resource.TypeKubernetesDeployment, ProviderResourceID: "fixture-namespace/app", Name: "app", Account: "fixture-cluster", Region: "fixture-region", Environment: resource.EnvDevelopment, Owner: "fixture-team", Criticality: resource.CriticalityLow})
	if err != nil {
		t.Fatal(err)
	}
	plugins := plugin.NewManager()
	if err = plugins.RegisterMetrics(p); err != nil {
		t.Fatal(err)
	}
	cfg.PluginID = p.ID()
	if cfg.Window == "" {
		cfg.Window = "2m"
	}
	if cfg.Interval == "" {
		cfg.Interval = "1m"
	}
	c, err := New(st, st, plugins, Settings{Config: cfg, ProviderConfig: map[string]string{"fixture": "config"}})
	if err != nil {
		t.Fatal(err)
	}
	return c, st, r
}
func TestManualAndScheduledDeduplicationAndQuality(t *testing.T) {
	ctx := context.Background()
	p := &fixtureProvider{}
	c, st, r := fixture(t, p, Config{Enabled: true})
	end := time.Now().UTC().Truncate(time.Minute)
	job, err := c.Request(ctx, r.ID, "fixture-operator", end)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err = c.Tick(ctx); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := st.GetCollection(ctx, job.ID)
	if got.State != "succeeded" || got.Assessment == nil || !got.Assessment.Eligible || got.Snapshot == nil {
		t.Fatalf("operation=%+v", got)
	}
	if p.calls.Load() != 1 {
		t.Fatalf("duplicate provider calls=%d", p.calls.Load())
	}
	jobs, _ := st.ListCollections(ctx, store.CollectionQuery{})
	if len(jobs) != 1 {
		t.Fatalf("jobs=%d", len(jobs))
	}
	events, err := st.QueryAuditEvents(ctx, audit.Query{Limit: 100})
	if err != nil || len(events.Events) != 3 {
		t.Fatalf("audit lifecycle: %+v %v", events, err)
	}
}
func TestFailureClassificationAndNoFabricatedEvidence(t *testing.T) {
	cases := []struct {
		name              string
		err               error
		mode, state, code string
	}{
		{"outage", &prometheus.Error{Code: "server", Retryable: true}, "", "retry_wait", "provider_unavailable"},
		{"access", &prometheus.Error{Code: "authentication"}, "", "failed", "provider_access"},
		{"query", &prometheus.Error{Code: "query"}, "", "failed", "provider_query"},
		{"unknown", errors.New("fixture-private-do-not-persist"), "", "failed", "provider_failure"},
		{"invalid", nil, "invalid", "failed", "invalid_evidence"},
		{"wrong unit", nil, "wrong_unit", "failed", "invalid_evidence"},
		{"negative current", nil, "negative_current", "failed", "invalid_evidence"},
		{"forged projection", nil, "forged_projection", "failed", "invalid_evidence"},
		{"wrong window", nil, "wrong_window", "failed", "invalid_evidence"},
		{"partial", nil, "partial", "degraded", ""}, {"missing", nil, "missing", "degraded", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := &fixtureProvider{err: tc.err, mode: tc.mode}
			c, st, r := fixture(t, p, Config{})
			job, err := c.Request(context.Background(), r.ID, "fixture-actor", time.Time{})
			if err != nil {
				t.Fatal(err)
			}
			if err = c.Tick(context.Background()); err != nil {
				t.Fatal(err)
			}
			got, _ := st.GetCollection(context.Background(), job.ID)
			if got.State != tc.state || got.ErrorCode != tc.code {
				t.Fatalf("state=%s code=%s", got.State, got.ErrorCode)
			}
			if tc.state == "degraded" && (got.Assessment == nil || got.Assessment.Eligible) {
				t.Fatal("incomplete collection passed complete-evidence gates")
			}
			if tc.err != nil && got.Snapshot != nil {
				t.Fatal("partial error result persisted as success")
			}
			raw, _ := json.Marshal(got)
			if strings.Contains(string(raw), "fixture-private-do-not-persist") {
				t.Fatal("raw error leaked")
			}
			if got.State == "retry_wait" {
				delay := got.NextRun.Sub(got.UpdatedAt)
				if delay < 4*time.Second || delay > 6*time.Second {
					t.Fatalf("retry delay=%s", delay)
				}
			}
		})
	}
}
func TestConfigJitterAndInstanceChanges(t *testing.T) {
	for _, cfg := range []Config{{MaxAttempts: 11}, {MaxJobsPerTick: 101}, {Interval: "0s"}, {Window: "1ns"}, {Timeout: "6m"}, {Retention: "1h"}} {
		p := &fixtureProvider{}
		plugins := plugin.NewManager()
		plugins.RegisterMetrics(p)
		if _, err := New(store.NewMemory(), store.NewMemory(), plugins, Settings{Config: cfg}); err == nil {
			t.Fatal("accepted invalid config", cfg)
		}
	}
	p := &fixtureProvider{}
	c, st, r := fixture(t, p, Config{})
	job, err := c.Request(context.Background(), r.ID, "actor", time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		j := stableJitter(fmtInt(i), time.Second)
		if j < 0 || j >= time.Second || j != stableJitter(fmtInt(i), time.Second) {
			t.Fatal("jitter not bounded/deterministic")
		}
	}
	c.digest = strings.Repeat("b", 64)
	if err = c.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, _ := st.GetCollection(context.Background(), job.ID)
	if got.ErrorCode != "instance_changed" || p.calls.Load() != 0 {
		t.Fatal("changed instance executed old operation")
	}
}

func TestConcurrentCollectorsClaimOnlyOnce(t *testing.T) {
	p := &fixtureProvider{}
	one, st, r := fixture(t, p, Config{})
	two, err := New(st, st, one.plugins, Settings{Config: one.cfg, ProviderConfig: map[string]string{"fixture": "config"}})
	if err != nil {
		t.Fatal(err)
	}
	job, err := one.Request(context.Background(), r.ID, "actor", time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 2)
	go func() { done <- one.Tick(context.Background()) }()
	go func() { done <- two.Tick(context.Background()) }()
	for i := 0; i < 2; i++ {
		if err = <-done; err != nil {
			t.Fatal(err)
		}
	}
	stored, _ := st.GetCollection(context.Background(), job.ID)
	if p.calls.Load() != 1 || stored.Attempts != 1 || stored.State != "succeeded" {
		t.Fatalf("calls=%d state=%s attempts=%d", p.calls.Load(), stored.State, stored.Attempts)
	}
}
func TestShutdownLeavesRecoverableClaimAndNoEvidence(t *testing.T) {
	p := &fixtureProvider{mode: "block"}
	c, st, r := fixture(t, p, Config{})
	job, err := c.Request(context.Background(), r.ID, "actor", time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- c.Tick(ctx) }()
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	poll := time.NewTicker(time.Millisecond)
	defer poll.Stop()
waiting:
	for {
		select {
		case <-deadline.C:
			t.Fatal("provider was not started")
		case <-poll.C:
			if p.calls.Load() > 0 {
				break waiting
			}
		}
	}
	cancel()
	if err = <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("shutdown=%v", err)
	}
	stored, _ := st.GetCollection(context.Background(), job.ID)
	if stored.State != "running" || stored.Lease == nil || stored.Snapshot != nil {
		t.Fatal("shutdown corrupted claimed work")
	}
}
func TestDisabledCollectorHasValidRecoveryIdentityAndRetentionDefaults(t *testing.T) {
	st := store.NewMemory()
	c, err := New(st, st, plugin.NewManager(), Settings{})
	if err != nil || c.owner == "" || c.retention != 0 {
		t.Fatal("disabled scheduler cannot recover/quarantine old jobs safely", err)
	}
}

func TestDurableCollectorResumesAfterProcessRestart(t *testing.T) {
	p := &fixtureProvider{}
	template, _, r := fixture(t, p, Config{})
	path := filepath.Join(t.TempDir(), "state.json")
	disk, err := store.OpenDurable(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = disk.UpsertResource(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	settings := Settings{Config: template.cfg, ProviderConfig: map[string]string{"fixture": "config"}}
	first, err := New(disk, disk, template.plugins, settings)
	if err != nil {
		t.Fatal(err)
	}
	job, err := first.Request(context.Background(), r.ID, "actor", time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	claim, err := disk.ClaimCollection(context.Background(), job.ID, "fixture-crashed-process", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err = disk.Close(); err != nil {
		t.Fatal(err)
	}
	disk, err = store.OpenDurable(path)
	if err != nil {
		t.Fatal(err)
	}
	defer disk.Close()
	restarted, err := New(disk, disk, template.plugins, settings)
	if err != nil {
		t.Fatal(err)
	}
	timer := time.NewTimer(time.Until(claim.Lease.Until) + 20*time.Millisecond)
	defer timer.Stop()
	<-timer.C
	if err = restarted.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	done, err := disk.GetCollection(context.Background(), job.ID)
	if err != nil || done.State != "succeeded" || done.Attempts != 2 || done.History[0].Outcome != "interrupted" || p.calls.Load() != 1 {
		t.Fatalf("recovery=%+v calls=%d error=%v", done, p.calls.Load(), err)
	}
	if err = restarted.Tick(context.Background()); err != nil || p.calls.Load() != 1 {
		t.Fatal("completed operation repeated", err)
	}
}

func TestRetryDelayAndUpstreamHintAreBounded(t *testing.T) {
	for attempt := 1; attempt <= 10; attempt++ {
		job := store.Collection{ID: "fixture-operation", Attempts: attempt}
		delay := retryDelay(job, errors.New("fixture"))
		base := 5 * time.Second * time.Duration(1<<uint(attempt-1))
		if base > 5*time.Minute {
			base = 5 * time.Minute
		}
		if delay < base || delay >= base+base/5 {
			t.Fatal(attempt, delay)
		}
	}
	delay := retryDelay(store.Collection{ID: "fixture", Attempts: 1}, &prometheus.Error{RetryAfterSeconds: 120})
	if delay < 2*time.Minute || delay >= 144*time.Second {
		t.Fatal("ignored Retry-After", delay)
	}
}
