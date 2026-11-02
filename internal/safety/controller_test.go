package safety

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/consize-oss/consize/internal/bootstrap"
	"github.com/consize-oss/consize/internal/policy"
	"github.com/consize-oss/consize/internal/recommender"
	"github.com/consize-oss/consize/internal/store"
	"github.com/consize-oss/consize/internal/testevidence"
	"github.com/consize-oss/consize/pkg/plugin"
	"github.com/consize-oss/consize/pkg/resource"
)

type testAction struct {
	preflightBlocked                bool
	state                           string
	applies, rollbacks              int
	lostAck, rollbackError, unready bool
}

func (a *testAction) ID() string { return "test-action" }
func (a *testAction) Manifest() plugin.Manifest {
	return plugin.Manifest{ID: a.ID(), Category: plugin.CategoryAction, SupportedResourceTypes: []string{"test.resource"}, SupportedActionTypes: []string{"resize"}, Capabilities: []string{plugin.CapabilityActionPlan, plugin.CapabilityActionExecute}, CanMutateInfrastructure: true, RequiresApproval: true}
}
func (a *testAction) Health(context.Context) plugin.Health { return plugin.Health{Status: "healthy"} }
func (a *testAction) Preflight(context.Context, plugin.ActionPlan) ([]plugin.PreflightCheck, error) {
	if a.preflightBlocked {
		return []plugin.PreflightCheck{{ID: "test", Status: "blocked", Message: "Controller conflict"}}, nil
	}
	return []plugin.PreflightCheck{{ID: "test", Status: "passed", Message: "Test resource preflight passed"}}, nil
}

func TestQueuedActionRechecksPreflightBeforeMutation(t *testing.T) {
	h := newHarness(t)
	h.submit(t)
	h.action.preflightBlocked = true
	h.tick(t)
	h.tick(t)
	if job := h.job(t); job.State != "cancelled" || h.action.applies != 0 || h.action.rollbacks != 0 {
		t.Fatalf("unsafe queued action: %+v", job)
	}
}

func TestBlockedPreflightDoesNotQueueJob(t *testing.T) {
	h := newHarness(t)
	h.action.preflightBlocked = true
	if _, err := h.c.Submit(h.ctx, h.id, "operator"); err == nil {
		t.Fatal("unsafe submission allowed")
	}
	jobs, err := h.st.ListJobs(h.ctx)
	if err != nil || len(jobs) != 0 {
		t.Fatal("unsafe submission created job")
	}
}
func (a *testAction) Plan(context.Context, plugin.ActionInput) (plugin.ActionPlan, error) {
	return plugin.ActionPlan{PluginID: a.ID(), ResourceID: "test", ActionType: "resize", OriginalState: map[string]any{"size": 100}, AppliedState: map[string]any{"size": 75}, RollbackAvailable: true, Diff: map[string]any{"original": 100, "target": 75}}, nil
}
func (a *testAction) Execute(context.Context, plugin.ActionPlan) (plugin.ActionResult, error) {
	a.applies++
	a.state = plugin.StateApplied
	if a.lostAck {
		a.lostAck = false
		return plugin.ActionResult{}, errors.New("response lost after apply")
	}
	return plugin.ActionResult{Applied: true}, nil
}
func (a *testAction) Inspect(context.Context, plugin.ActionPlan) (string, error) { return a.state, nil }
func (a *testAction) Rollback(context.Context, plugin.ActionPlan) (plugin.ActionResult, error) {
	a.rollbacks++
	if a.rollbackError {
		return plugin.ActionResult{}, errors.New("provider unavailable")
	}
	a.state = plugin.StateOriginal
	return plugin.ActionResult{Applied: true}, nil
}
func (a *testAction) Ready(context.Context, plugin.ActionPlan) error {
	if a.unready {
		return errors.New("unhealthy")
	}
	return nil
}

type testMetrics struct {
	now             *time.Time
	action          *testAction
	failed, missing bool
}

func (m *testMetrics) ID() string { return "test-metrics" }
func (m *testMetrics) Manifest() plugin.Manifest {
	return plugin.Manifest{ID: m.ID(), Category: plugin.CategoryMetrics, SupportedResourceTypes: []string{"test.resource"}, Capabilities: []string{plugin.CapabilityMetricsRead}}
}
func (m *testMetrics) Health(context.Context) plugin.Health { return plugin.Health{Status: "healthy"} }
func (m *testMetrics) ReadMetrics(context.Context, resource.Resource) (plugin.MetricsSnapshot, error) {
	return m.snapshot(100), nil
}
func (m *testMetrics) ReadMetricsBetween(_ context.Context, _ resource.Resource, start time.Time, end time.Time) (plugin.MetricsSnapshot, error) {
	s := m.snapshot(100)
	if m.action.state == plugin.StateApplied {
		if m.failed {
			s = m.snapshot(200)
		}
		if m.missing {
			s.Signals["cpu_cores_points"] = 0
		}
	}
	for key, coverage := range s.Coverage {
		coverage.FirstSample = start
		coverage.LastSample = end
		s.Coverage[key] = coverage
	}
	return s, nil
}
func (m *testMetrics) snapshot(memory float64) plugin.MetricsSnapshot {
	coverage := map[string]plugin.MetricCoverage{}
	for _, key := range []string{"memory_working_set_bytes", "cpu_cores", "restarts_30m"} {
		coverage[key] = plugin.MetricCoverage{FirstSample: m.now.Add(-10 * time.Minute), LastSample: *m.now, ExpectedPoints: 20, ActualPoints: 20, Step: 30 * time.Second}
	}
	return testevidence.Attach(plugin.MetricsSnapshot{Coverage: coverage, PluginID: m.ID(), ResourceID: "test", CollectedAt: *m.now, Signals: map[string]any{"memory_working_set_bytes_p95": memory, "cpu_cores_p95": 1.0, "restarts_30m_max": 0.0, "memory_working_set_bytes_points": 20, "cpu_cores_points": 20, "restarts_30m_points": 20}})
}

type harness struct {
	ctx     context.Context
	path    string
	st      *store.Memory
	c       *Controller
	plugins *plugin.Manager
	action  *testAction
	metrics *testMetrics
	now     time.Time
	id      int64
}

func TestSubmissionReassessesLegacyConfidence(t *testing.T) {
	h := newHarness(t)
	rec, err := h.st.CreateRecommendation(h.ctx, store.Recommendation{ResourceID: "test", PluginID: h.action.ID(), ActionType: "resize", AlgorithmID: recommender.BuiltInHeadroomAlgorithmID, EvidenceRefs: []string{"metrics:test:baseline"}, Confidence: "high"})
	if err != nil {
		t.Fatal(err)
	}
	h.c.recommendationCfg = bootstrap.RecommenderConfig{MetricsPluginID: h.metrics.ID(), ConfidenceProfile: "lab"}
	if _, err = h.c.Submit(h.ctx, rec.ID, "operator"); err == nil {
		t.Fatal("legacy high label bypassed fresh history assessment")
	}
	jobs, err := h.st.ListJobs(h.ctx)
	if err != nil || len(jobs) != 0 || h.action.applies != 0 {
		t.Fatalf("rejected evidence created work: %v %v", jobs, err)
	}
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{ctx: context.Background(), path: filepath.Join(t.TempDir(), "state.json"), now: time.Now().UTC()}
	var err error
	h.st, err = store.OpenDurable(h.path)
	if err != nil {
		t.Fatal(err)
	}
	h.action = &testAction{state: plugin.StateOriginal}
	h.metrics = &testMetrics{now: &h.now, action: h.action}
	h.plugins = plugin.NewManager()
	if err = h.plugins.RegisterAction(h.action); err != nil {
		t.Fatal(err)
	}
	if err = h.plugins.RegisterMetrics(h.metrics); err != nil {
		t.Fatal(err)
	}
	_, err = h.st.UpsertResource(h.ctx, resource.Resource{
		ID: "test", Type: "test.resource", Provider: "test-provider",
		ProviderResourceID: "resources/test", Name: "test", Account: "test-account", Region: "local",
		Environment: resource.EnvDevelopment, Owner: "team", Criticality: resource.CriticalityLow,
		CurrentState: map[string]any{"size": 100},
	})
	if err != nil {
		t.Fatal(err)
	}
	rec, err := h.st.CreateRecommendation(h.ctx, store.Recommendation{ResourceID: "test", PluginID: h.action.ID(), ActionType: "resize", AlgorithmID: "test-algorithm", EvidenceRefs: []string{"metrics:test:baseline"}, Confidence: "medium"})
	if err != nil {
		t.Fatal(err)
	}
	h.id = rec.ID
	h.rebuild()
	t.Cleanup(func() { h.st.Close() })
	return h
}
func (h *harness) rebuild() {
	h.c = New(h.st, h.plugins, policy.NewEngine(), bootstrap.VerificationConfig{Enabled: true, MetricsPluginID: h.metrics.ID(), Wait: "5m", Timeout: "1m", RollbackOnFailure: true, RollbackOnTimeout: true, MaxMemoryP95IncreaseRatio: 1.25, MaxCPUP95IncreaseRatio: 1.5})
	h.c.now = func() time.Time { return h.now }
}
func (h *harness) submit(t *testing.T) {
	t.Helper()
	if _, err := h.c.Submit(h.ctx, h.id, "operator"); err != nil {
		t.Fatal(err)
	}
}
func (h *harness) job(t *testing.T) store.Job {
	t.Helper()
	jobs, err := h.st.ListJobs(h.ctx)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("jobs: %v %v", jobs, err)
	}
	return jobs[0]
}
func (h *harness) tick(t *testing.T) {
	t.Helper()
	h.now = h.now.Add(6 * time.Second)
	if err := h.c.Tick(h.ctx); err != nil {
		t.Fatal(err)
	}
}
func (h *harness) toWindow(t *testing.T) {
	t.Helper()
	for i := 0; i < 10; i++ {
		job := h.job(t)
		if job.State == "verifying" || job.State == "rollback_verifying" {
			return
		}
		h.tick(t)
	}
	t.Fatal("did not reach verification")
}
func (h *harness) observe(t *testing.T) {
	job := h.job(t)
	h.now = job.WindowStart.Add(5*time.Minute + time.Second)
	if err := h.c.Tick(h.ctx); err != nil {
		t.Fatal(err)
	}
}

func TestRestartResumesWithoutReapplying(t *testing.T) {
	h := newHarness(t)
	h.submit(t)
	h.toWindow(t)
	if err := h.st.Close(); err != nil {
		t.Fatal(err)
	}
	var err error
	h.st, err = store.OpenDurable(h.path)
	if err != nil {
		t.Fatal(err)
	}
	h.rebuild()
	h.observe(t)
	if job := h.job(t); job.State != "verified" || h.action.applies != 1 || job.Result == nil {
		t.Fatalf("restart result: %#v applies=%d", job, h.action.applies)
	}
}

func TestFailedVerificationRollsBackAndVerifiesRestoration(t *testing.T) {
	h := newHarness(t)
	h.metrics.failed = true
	h.submit(t)
	h.toWindow(t)
	h.observe(t)
	if h.job(t).State != "rollback_pending" {
		t.Fatal("failure did not authorize rollback")
	}
	h.toWindow(t)
	h.observe(t)
	if job := h.job(t); job.State != "rolled_back" || h.action.rollbacks != 1 || h.action.state != plugin.StateOriginal {
		t.Fatalf("rollback: %#v", job)
	}
}

func TestLostApplyAcknowledgementDoesNotDuplicateMutation(t *testing.T) {
	h := newHarness(t)
	h.action.lostAck = true
	h.submit(t)
	h.toWindow(t)
	h.observe(t)
	if h.action.applies != 1 || h.job(t).State != "verified" {
		t.Fatal("lost acknowledgement duplicated apply")
	}
}

func TestMissingEvidenceTimesOutIntoRollback(t *testing.T) {
	h := newHarness(t)
	h.metrics.missing = true
	h.submit(t)
	h.toWindow(t)
	h.observe(t)
	if h.job(t).State != "verifying" {
		t.Fatal("missing metrics should retry")
	}
	h.now = h.job(t).Deadline.Add(time.Second)
	if err := h.c.Tick(h.ctx); err != nil {
		t.Fatal(err)
	}
	if h.job(t).State != "rollback_pending" {
		t.Fatal("timeout did not authorize rollback")
	}
	h.toWindow(t)
	h.observe(t)
	if h.job(t).State != "rolled_back" {
		t.Fatal("timeout recovery failed")
	}
}

func TestDriftStopsWithoutRollbackOverwrite(t *testing.T) {
	h := newHarness(t)
	h.submit(t)
	h.toWindow(t)
	h.action.state = plugin.StateDrifted
	h.tick(t)
	if h.job(t).State != "manual_intervention" || h.action.rollbacks != 0 {
		t.Fatal("drift was overwritten")
	}
}

func TestRollbackFailureRequiresManualIntervention(t *testing.T) {
	h := newHarness(t)
	h.metrics.failed = true
	h.action.rollbackError = true
	h.submit(t)
	h.toWindow(t)
	h.observe(t)
	h.tick(t)
	h.tick(t)
	h.now = h.job(t).Deadline.Add(time.Second)
	if err := h.c.Tick(h.ctx); err != nil {
		t.Fatal(err)
	}
	if h.job(t).State != "manual_intervention" {
		t.Fatal("rollback failure was reported as success")
	}
}

func TestDuplicateSubmissionReturnsSameJob(t *testing.T) {
	h := newHarness(t)
	h.submit(t)
	h.submit(t)
	h.toWindow(t)
	if h.action.applies != 1 {
		t.Fatal("duplicate action applied")
	}
}

func TestRollbackPolicyIsCapturedAtSubmission(t *testing.T) {
	h := newHarness(t)
	h.metrics.failed = true
	h.c.cfg.RollbackOnFailure = false
	h.submit(t)
	h.c.cfg.RollbackOnFailure = true
	h.toWindow(t)
	h.observe(t)
	if h.job(t).State != "manual_intervention" || h.action.rollbacks != 0 {
		t.Fatal("captured policy was ignored")
	}
}

func TestExplicitRecoveryCanResumeAfterProviderReturns(t *testing.T) {
	h := newHarness(t)
	h.metrics.failed = true
	h.c.cfg.RollbackOnFailure = false
	h.submit(t)
	h.toWindow(t)
	h.observe(t)
	if h.job(t).State != "manual_intervention" {
		t.Fatal("expected manual recovery")
	}
	if _, err := h.c.Recover(h.ctx, h.id, "operator"); err != nil {
		t.Fatal(err)
	}
	h.toWindow(t)
	h.observe(t)
	if h.job(t).State != "rolled_back" {
		t.Fatal("operator-approved recovery failed")
	}
}

func TestExplicitRecoveryStillRejectsDrift(t *testing.T) {
	h := newHarness(t)
	h.submit(t)
	h.toWindow(t)
	h.action.state = plugin.StateDrifted
	h.tick(t)
	if _, err := h.c.Recover(h.ctx, h.id, "operator"); err == nil {
		t.Fatal("manual recovery bypassed drift guard")
	}
}

func TestStorageFailurePreventsMutation(t *testing.T) {
	h := newHarness(t)
	h.submit(t)
	h.st.Close()
	if err := h.c.Tick(h.ctx); err == nil {
		t.Fatal("closed store allowed execution")
	}
	if h.action.applies != 0 {
		t.Fatal("infrastructure changed with unavailable storage")
	}
}

func TestActiveActionReservesResourceAcrossRecommendations(t *testing.T) {
	h := newHarness(t)
	h.submit(t)
	rec, err := h.st.CreateRecommendation(h.ctx, store.Recommendation{ResourceID: "test", PluginID: h.action.ID(), ActionType: "resize", AlgorithmID: "test-algorithm", EvidenceRefs: []string{"metrics:test:baseline"}, Confidence: "medium"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.c.Submit(h.ctx, rec.ID, "operator"); err == nil {
		t.Fatal("two active actions reserved same resource")
	}
}

func TestRestartBetweenApplyAndAcknowledgementReconcilesIntent(t *testing.T) {
	h := newHarness(t)
	h.submit(t)
	h.tick(t)
	if h.job(t).State != "applying" {
		t.Fatal("apply intent not persisted")
	}
	if _, err := h.action.Execute(h.ctx, h.job(t).Plan); err != nil {
		t.Fatal(err)
	}
	h.st.Close()
	var err error
	h.st, err = store.OpenDurable(h.path)
	if err != nil {
		t.Fatal(err)
	}
	h.rebuild()
	h.toWindow(t)
	h.observe(t)
	if h.action.applies != 1 || h.job(t).State != "verified" {
		t.Fatal("crash recovery duplicated apply")
	}
}

func TestRestartBetweenRollbackAndAcknowledgementReconcilesRestoration(t *testing.T) {
	h := newHarness(t)
	h.metrics.failed = true
	h.submit(t)
	h.toWindow(t)
	h.observe(t)
	h.tick(t)
	if h.job(t).State != "rolling_back" {
		t.Fatal("rollback intent not persisted")
	}
	if _, err := h.action.Rollback(h.ctx, h.job(t).Plan); err != nil {
		t.Fatal(err)
	}
	h.st.Close()
	var err error
	h.st, err = store.OpenDurable(h.path)
	if err != nil {
		t.Fatal(err)
	}
	h.rebuild()
	h.toWindow(t)
	h.observe(t)
	if h.action.rollbacks != 1 || h.job(t).State != "rolled_back" {
		t.Fatal("crash recovery duplicated rollback")
	}
}
