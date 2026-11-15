package inventory

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/consize-oss/consize/internal/policy"
	"github.com/consize-oss/consize/internal/store"
	"github.com/consize-oss/consize/pkg/evidence"
	qualitymodel "github.com/consize-oss/consize/pkg/evidence/quality"
	"github.com/consize-oss/consize/pkg/plugin"
	"github.com/consize-oss/consize/pkg/resource"
)

type fixedPolicy struct{ status string }

func (p fixedPolicy) Coverage(resource.Resource) policy.Coverage {
	ids := []string{}
	if p.status == "covered" {
		ids = append(ids, "matching-rule-v2")
	}
	return policy.Coverage{Status: p.status, PolicyIDs: ids, Reason: "test_resolver"}
}

func manifestFixture() []plugin.Manifest {
	return []plugin.Manifest{
		{ID: "metrics", Category: plugin.CategoryMetrics, Capabilities: []string{plugin.CapabilityMetricsRead}, SupportedResourceTypes: []string{resource.TypeKubernetesDeployment}},
		{ID: "actions", Category: plugin.CategoryAction, Capabilities: []string{plugin.CapabilityActionPlan, plugin.CapabilityActionExecute}, SupportedResourceTypes: []string{resource.TypeKubernetesDeployment}, SupportedActionTypes: []string{"resize"}},
	}
}
func recordFixture(t *testing.T, now time.Time, quality string) evidence.Record {
	t.Helper()
	r := evidence.Record{ResourceID: "a", PluginID: "metrics", Signal: "cpu", Query: "private-query-not-for-the-api", Kind: evidence.Aggregated, Aggregation: "p95", Unit: "cores", OriginalUnit: "cores", Scale: 1, Value: evidence.Number(0), SampleCount: 31, WindowStart: now.Add(-61 * time.Minute), WindowEnd: now.Add(-time.Minute), CollectedAt: now.Add(-30 * time.Second), FreshUntil: now.Add(time.Minute), Quality: quality}
	if quality == evidence.Missing || quality == evidence.Unavailable || quality == evidence.Invalid {
		r.Value = nil
		r.SampleCount = 0
	}
	observed := r.WindowEnd
	first := now.Add(-61 * time.Minute)
	sourcePoints := 31
	facts, _ := qualitymodel.EncodeContext(qualitymodel.Context{Mapping: "exact", LastObservationAt: &observed, ScrapeInterval: "2m", ObservedSamples: &sourcePoints, FirstObservationAt: &first, SourceMaxGap: "2m"})
	actual := "31"
	if r.SampleCount == 0 {
		actual = "0"
	}
	r.Extensions = map[string]string{qualitymodel.ContextKey: facts, "step": "2m", "max_gap": "2m", "expected_points": "31", "actual_points": actual, "first_sample": r.WindowStart.Format(time.RFC3339Nano), "last_sample": r.WindowEnd.Format(time.RFC3339Nano)}
	r, err := evidence.Seal(r)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func buildFixture(t *testing.T, at time.Time, records []evidence.Record, manifests []plugin.Manifest, p PolicyReader) Row {
	t.Helper()
	in := store.InventorySnapshot{Resources: []resource.Resource{{ID: "a", Type: resource.TypeKubernetesDeployment, Owner: " unassigned ", LastSeenAt: at, Metadata: map[string]any{"inventory": map[string]any{"telemetry": "healthy", "policy": "covered"}}}}, Recommendations: []store.Recommendation{{ID: 1, ResourceID: "a", Status: store.RecommendationPending, EvidenceSnapshot: records}}}
	rows, err := Build(context.Background(), in, manifests, p, []Signal{{Name: "cpu", Unit: "cores"}}, at)
	if err != nil {
		t.Fatal(err)
	}
	return rows[0]
}

func TestTelemetryUsesTrustedEvidenceAndWallClock(t *testing.T) {
	now := time.Now().UTC()
	valid := recordFixture(t, now, evidence.Valid)
	for _, tc := range []struct {
		name      string
		records   []evidence.Record
		manifests []plugin.Manifest
		at        time.Time
		status    string
	}{
		{"discovery and forged metadata are not telemetry", nil, manifestFixture(), now, "unknown"},
		{"measured zero is usable", []evidence.Record{valid}, manifestFixture(), now, "usable"},
		{"ages without rediscovery", []evidence.Record{valid}, manifestFixture(), now.Add(2 * time.Minute), "stale"},
		{"partial samples", []evidence.Record{recordFixture(t, now, evidence.Partial)}, manifestFixture(), now, "unknown"},
		{"explicitly missing measurements", []evidence.Record{recordFixture(t, now, evidence.Missing)}, manifestFixture(), now, "unmonitored"},
		{"unavailable is not missing", []evidence.Record{recordFixture(t, now, evidence.Unavailable)}, manifestFixture(), now, "unknown"},
		{"metrics disabled", []evidence.Record{valid}, []plugin.Manifest{}, now, "unmonitored"},
		{"capability resolver unavailable", []evidence.Record{valid}, nil, now, "unknown"},
		{"future collection rejected", []evidence.Record{valid}, manifestFixture(), now.Add(-time.Hour), "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := buildFixture(t, tc.at, tc.records, tc.manifests, fixedPolicy{"covered"})
			if r.Assessment.Telemetry.Status != tc.status {
				t.Fatalf("%+v", r.Assessment.Telemetry)
			}
			if r.Assessment.Owner != nil {
				t.Fatal("unassigned sentinel treated as owner")
			}
		})
	}
	for _, mutate := range []func(*evidence.Record){func(r *evidence.Record) { r.ID = "forged" }, func(r *evidence.Record) { r.ResourceID = "other" }, func(r *evidence.Record) { r.PluginID = "unregistered" }, func(r *evidence.Record) { r.Unit = "bytes" }} {
		r := valid
		mutate(&r)
		if buildFixture(t, now, []evidence.Record{r}, manifestFixture(), nil).Assessment.Telemetry.Status == "usable" {
			t.Fatal("untrusted evidence accepted")
		}
	}
}

func TestPolicyAndCapabilitiesRefreshAndUnknownIsNotGap(t *testing.T) {
	now := time.Now().UTC()
	for _, status := range []string{"covered", "uncovered", "unknown", "malformed"} {
		r := buildFixture(t, now, nil, manifestFixture(), fixedPolicy{status})
		gap := false
		for _, reason := range r.Assessment.AttentionReasons {
			gap = gap || reason == PolicyGap
		}
		if gap != (status == "uncovered") {
			t.Fatalf("%s: %+v", status, r.Assessment)
		}
	}
	r := buildFixture(t, now, nil, manifestFixture(), nil)
	if r.Assessment.Actions.Status != "supported" {
		t.Fatal(r.Assessment.Actions)
	}
	ms := manifestFixture()
	ms[1].Capabilities = []string{plugin.CapabilityActionPlan}
	if buildFixture(t, now, nil, ms, nil).Assessment.Actions.Status != "unsupported" {
		t.Fatal("plan-only plugin counted as execution path")
	}
	ms[1].Capabilities = append(ms[1].Capabilities, plugin.CapabilityActionExecute)
	ms[1].SupportedResourceTypes = []string{"other"}
	if buildFixture(t, now, nil, ms, nil).Assessment.Actions.Status != "unsupported" {
		t.Fatal("wrong resource type counted")
	}
}

func TestNoMixingSparseSourcesAndLatestFailureWins(t *testing.T) {
	now := time.Now().UTC()
	r := recordFixture(t, now, evidence.Valid)
	ms := manifestFixture()
	other := ms[0]
	other.ID = "other-metrics"
	ms = append(ms, other)
	second := r
	second.Signal = "memory"
	second.PluginID = other.ID
	second.ID = ""
	second, _ = evidence.Seal(second)
	in := store.InventorySnapshot{Resources: []resource.Resource{{ID: "a", Type: resource.TypeKubernetesDeployment}}, Recommendations: []store.Recommendation{{ResourceID: "a", EvidenceSnapshot: []evidence.Record{r, second}}}}
	rows, err := Build(context.Background(), in, ms, nil, []Signal{{Name: "cpu"}, {Name: "memory"}}, now)
	if err != nil {
		t.Fatal(err)
	}
	if rows[0].Assessment.Telemetry.Status != "unknown" {
		t.Fatal("mixed sparse sources became usable")
	}
	later := recordFixture(t, now.Add(10*time.Second), evidence.Partial)
	if buildFixture(t, now, []evidence.Record{r, later}, ms, nil).Assessment.Telemetry.Status != "unknown" {
		t.Fatal("older good sample masked newer partial evidence")
	}
}

func TestCountsOverlapFiltersAndRecommendationLifecycle(t *testing.T) {
	now := time.Now().UTC()
	in := store.InventorySnapshot{Resources: []resource.Resource{{ID: "a", Type: resource.TypeKubernetesDeployment, Owner: "unassigned", Environment: "production", Labels: map[string]string{"team": "payments"}}, {ID: "b", Type: resource.TypeKubernetesDeployment, Owner: "owner", Environment: "staging"}, {ID: "deleted", LifecycleState: resource.LifecycleDeleted}}, Recommendations: []store.Recommendation{
		{ID: 3, ResourceID: "a", Status: store.RecommendationExecuting},
		{ID: 1, ResourceID: "a", Status: store.RecommendationPending, ExpiresAt: now},
		{ID: 2, ResourceID: "a", Status: store.RecommendationSuperseded, SupersededBy: 3},
		{ID: 4, ResourceID: "a", Status: store.RecommendationVerified},
	}}
	rows, err := Build(context.Background(), in, []plugin.Manifest{}, fixedPolicy{"uncovered"}, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(rows[0].Assessment.ActiveRecommendationIDs, []int64{3}) || rows[0].Assessment.OptimizationStatus != "executing" {
		t.Fatal(rows[0].Assessment)
	}
	all, s := Filter(rows, Query{Tab: "all"})
	attention, a := Filter(rows, Query{Tab: "attention"})
	unmonitored, u := Filter(rows, Query{Tab: "unmonitored"})
	if len(all) != 2 || len(attention) != 2 || len(unmonitored) != 2 || s.Attention != 2 || s.Unmonitored != 2 || s.Reasons[Unowned] != 1 || !reflect.DeepEqual(s, a) || !reflect.DeepEqual(s, u) {
		t.Fatalf("counts: %+v %+v %+v", s, a, u)
	}
	_, scoped := Filter(rows, Query{Search: "payments", Environment: "production", Tab: "all"})
	if scoped.Discovered != 1 {
		t.Fatal(scoped)
	}
	_, empty := Filter(rows, Query{Search: "absent"})
	if empty.SupportedActionPercent != nil {
		t.Fatal("empty denominator reported as measured 0%")
	}
}

func TestBuildHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Build(ctx, store.InventorySnapshot{Resources: []resource.Resource{{ID: "a"}}}, nil, nil, nil, time.Now())
	if err == nil {
		t.Fatal("cancelled assessment succeeded")
	}
}

func TestSparseConflictingAndRecoveredEvidence(t *testing.T) {
	now := time.Now().UTC()
	valid := recordFixture(t, now, evidence.Valid)
	sparse := valid
	sparse.SampleCount = 1
	sparse, _ = evidence.Seal(sparse)
	if buildFixture(t, now, []evidence.Record{sparse}, manifestFixture(), nil).Assessment.Telemetry.Status != "unknown" {
		t.Fatal("single sample promoted to usable telemetry")
	}
	conflicting := valid
	conflicting.Value = evidence.Number(2)
	conflicting, _ = evidence.Seal(conflicting)
	for _, records := range [][]evidence.Record{{valid, conflicting}, {conflicting, valid}} {
		if buildFixture(t, now, records, manifestFixture(), nil).Assessment.Telemetry.Status != "unknown" {
			t.Fatal("equal-time conflicting samples were selected arbitrarily")
		}
	}
	partial := recordFixture(t, now.Add(10*time.Second), evidence.Partial)
	r := buildFixture(t, now, []evidence.Record{valid, partial}, manifestFixture(), nil)
	if r.Assessment.Telemetry.LastMeasurementAt == nil || !r.Assessment.Telemetry.LastMeasurementAt.Equal(valid.WindowEnd) {
		t.Fatal("latest partial evidence erased the last successful measurement")
	}
	recovered := recordFixture(t, now.Add(20*time.Second), evidence.Valid)
	if buildFixture(t, now, []evidence.Record{partial, recovered}, manifestFixture(), nil).Assessment.Telemetry.Status != "usable" {
		t.Fatal("new complete evidence did not recover reporting")
	}
}

func TestDurableJobEvidenceFeedsInventory(t *testing.T) {
	now := time.Now().UTC()
	r := recordFixture(t, now, evidence.Valid)
	snapshot := plugin.MetricsSnapshot{ResourceID: "a", PluginID: "metrics", CollectedAt: r.CollectedAt, Window: r.WindowEnd.Sub(r.WindowStart).String(), Records: []evidence.Record{r}}
	in := store.InventorySnapshot{Resources: []resource.Resource{{ID: "a", Type: resource.TypeKubernetesDeployment}}, Jobs: []store.Job{{Resource: resource.Resource{ID: "a"}, Baseline: snapshot}}}
	rows, err := Build(context.Background(), in, manifestFixture(), nil, []Signal{{Name: "cpu", Unit: "cores"}}, now)
	if err != nil || rows[0].Assessment.Telemetry.Status != "usable" {
		t.Fatalf("%+v %v", rows, err)
	}
	in.Jobs[0].Resource.ID = "other"
	rows, err = Build(context.Background(), in, manifestFixture(), nil, []Signal{{Name: "cpu", Unit: "cores"}}, now)
	if err != nil || rows[0].Assessment.Telemetry.Status != "unknown" {
		t.Fatal("job evidence crossed resource boundaries")
	}
}

func TestUnavailableSupportIsNotMeasuredZeroAndBlankActionsAreNotSupported(t *testing.T) {
	now := time.Now().UTC()
	r := buildFixture(t, now, nil, nil, nil)
	_, summary := Filter([]Row{r}, Query{Tab: "all"})
	if summary.SupportedActionPercent != nil || summary.Reasons[ActionUnknown] != 1 {
		t.Fatal("unavailable action assessment became measured 0%")
	}
	ms := manifestFixture()
	ms[1].SupportedActionTypes = []string{"", " "}
	r = buildFixture(t, now, nil, ms, nil)
	if r.Assessment.Actions.Status != "unsupported" {
		t.Fatal("blank action type established support")
	}
}

func TestInventoryUsesSharedQualityAndDoesNotTrustQueryTime(t *testing.T) {
	now := time.Now().UTC()
	valid := recordFixture(t, now, evidence.Valid)
	row := buildFixture(t, now, []evidence.Record{valid}, manifestFixture(), nil)
	if row.Assessment.Telemetry.Quality == nil || row.Assessment.Telemetry.Quality.Version != qualitymodel.Version || !row.Assessment.Telemetry.Quality.Eligible {
		t.Fatal("shared assessment missing")
	}
	// A valid, recently collected aggregate without source timing/mapping facts
	// must not become reporting merely because its query finished recently.
	delete(valid.Extensions, qualitymodel.ContextKey)
	valid, err := evidence.Seal(valid)
	if err != nil {
		t.Fatal(err)
	}
	row = buildFixture(t, now, []evidence.Record{valid}, manifestFixture(), nil)
	if row.Assessment.Telemetry.Status != "unknown" || row.Assessment.Telemetry.LastMeasurementAt != nil || row.Assessment.Telemetry.Quality == nil || row.Assessment.Telemetry.Quality.Eligible {
		t.Fatal("query time substituted for source observation")
	}
	_, summary := Filter([]Row{row}, Query{Tab: "all"})
	if summary.Reporting != 0 {
		t.Fatal("unproven observations counted as reporting")
	}
}
