package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/consize-oss/consize/pkg/evidence"
	"github.com/consize-oss/consize/pkg/resource"
)

type runtimeFixture struct {
	manifest Manifest
	plan     func(context.Context, ActionInput) (ActionPlan, error)
	init     func(context.Context) error
}

func (f *runtimeFixture) ID() string         { return f.manifest.ID }
func (f *runtimeFixture) Manifest() Manifest { return f.manifest }
func (f *runtimeFixture) Health(context.Context) Health {
	return Health{Status: "healthy", Message: "fixture-private-do-not-publish", Details: map[string]any{"token": "fake-private-do-not-publish"}}
}
func (f *runtimeFixture) Plan(ctx context.Context, in ActionInput) (ActionPlan, error) {
	if f.plan != nil {
		return f.plan(ctx, in)
	}
	return ActionPlan{PluginID: f.ID(), ResourceID: in.Resource.ID, ActionType: in.ActionType}, nil
}
func (f *runtimeFixture) Execute(_ context.Context, p ActionPlan) (ActionResult, error) {
	return ActionResult{PluginID: f.ID(), ResourceID: p.ResourceID, ActionType: p.ActionType}, nil
}
func (f *runtimeFixture) Initialize(ctx context.Context, _ json.RawMessage, _ map[string]string) error {
	if f.init != nil {
		return f.init(ctx)
	}
	return nil
}
func fixtureManifest() Manifest {
	return Manifest{ID: "fixture", DisplayName: "Fixture", Version: "1.2.3", Category: CategoryAction, CanMutateInfrastructure: true, Capabilities: []string{CapabilityActionPlan, CapabilityActionExecute}, SupportedResourceTypes: []string{"fixture.resource"}, SupportedActionTypes: []string{"resize"}, Runtime: &RuntimeManifest{SDKRange: ">=1.0.0 <2.0.0", CoreRange: ">=0.3.0-alpha <1.0.0", Provider: "fixture", ConfigurationVersion: "1", Distribution: "builtin", Capabilities: []CapabilityDescriptor{{Name: CapabilityActionPlan, Version: "1.0.0", ResourceTypes: []string{"fixture.resource"}, Providers: []string{"fixture"}, Operations: []string{"resize"}, Modes: []string{"pr"}}, {Name: CapabilityActionExecute, Version: "1.0.0"}}}}
}
func fixtureInput() ActionInput {
	return ActionInput{Resource: resource.Resource{ID: "resource-1", Type: "fixture.resource", Provider: "fixture", Account: "cluster-a"}, ActionType: "resize", Mode: "pr"}
}
func registered(t *testing.T, f *runtimeFixture, timeout time.Duration) (*Manager, ActionPlugin) {
	t.Helper()
	m := NewManager()
	if err := m.RegisterInstance(context.Background(), InstanceConfig{ID: "instance-a", PackageID: "fixture", Provider: "fixture", Account: "cluster-a", Required: true, Timeout: timeout}, f, nil); err != nil {
		t.Fatal(err)
	}
	p, err := m.ActionPlugin("instance-a", "fixture.resource", "resize")
	if err != nil {
		t.Fatal(err)
	}
	return m, p
}

func TestRuntimeManifestValidation(t *testing.T) {
	if err := ValidateRuntimeManifest(fixtureManifest()); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*Manifest){"version": func(m *Manifest) { m.Version = "1" }, "identity": func(m *Manifest) { m.ID = "../secret" }, "duplicate": func(m *Manifest) { m.Runtime.Capabilities = append(m.Runtime.Capabilities, m.Runtime.Capabilities[0]) }, "incompatible": func(m *Manifest) { m.Runtime.SDKRange = ">=2.0.0" }, "unsupported range": func(m *Manifest) { m.Runtime.CoreRange = "^0.3.0" }, "missing descriptor": func(m *Manifest) { m.Runtime.Capabilities = m.Runtime.Capabilities[:1] }, "external integrity": func(m *Manifest) { m.Runtime.Distribution = "external"; m.Runtime.Integrity = "unverified" }}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			m := fixtureManifest()
			mutate(&m)
			if ValidateRuntimeManifest(m) == nil {
				t.Fatal("accepted invalid manifest")
			}
		})
	}
	for _, tc := range []struct {
		v, r string
		want bool
	}{{"1.2.3", ">=1.0.0 <2.0.0", true}, {"2.0.0", ">=1.0.0 <2.0.0", false}, {"1.0.0-alpha.2", "<1.0.0-alpha.10", true}, {"1.2.3+build", "=1.2.3", true}} {
		got, err := MatchVersionRange(tc.v, tc.r)
		if err != nil || got != tc.want {
			t.Fatalf("%s %s: %v %v", tc.v, tc.r, got, err)
		}
	}
}
func TestRuntimeMultiInstanceRoutingAndScope(t *testing.T) {
	m, p := registered(t, &runtimeFixture{manifest: fixtureManifest()}, time.Second)
	err := m.RegisterInstance(context.Background(), InstanceConfig{ID: "instance-b", PackageID: "fixture", Provider: "fixture", Account: "cluster-b"}, &runtimeFixture{manifest: fixtureManifest()}, nil)
	if err != nil {
		t.Fatal(err)
	}
	route := Route{Capability: CapabilityActionPlan, Provider: "fixture", Account: "cluster-a", ResourceType: "fixture.resource", Operation: "resize", Mode: "pr", VersionRange: ">=1.0.0 <2.0.0"}
	id, err := m.Resolve(route)
	if err != nil || id != "instance-a" {
		t.Fatalf("%s %v", id, err)
	}
	route.Account = "cluster-b"
	id, err = m.Resolve(route)
	if err != nil || id != "instance-b" {
		t.Fatalf("%s %v", id, err)
	}
	in := fixtureInput()
	in.Resource.Account = "cluster-b"
	if _, err = p.Plan(context.Background(), in); err == nil {
		t.Fatal("cross-cluster call accepted")
	}
	plan, err := p.Plan(context.Background(), fixtureInput())
	if err != nil {
		t.Fatal(err)
	}
	if plan.Runtime == nil || plan.Runtime.InstanceID != "instance-a" || plan.Runtime.PackageID != "fixture" || plan.PluginVersion != "1.2.3" {
		t.Fatal("missing provenance")
	}
	result, err := p.Execute(context.Background(), plan)
	if err != nil || result.Runtime == nil || result.Runtime.InstanceID != "instance-a" {
		t.Fatalf("invalid result: %+v %v", result, err)
	}
	health, err := m.Health(context.Background(), "instance-a")
	if err != nil || strings.Contains(health.Message, "fixture-private") || health.Details["token"] != nil {
		t.Fatal("health leaked secrets")
	}
}
func TestRuntimeRejectAmbiguityAndDuplicate(t *testing.T) {
	m, _ := registered(t, &runtimeFixture{manifest: fixtureManifest()}, time.Second)
	for _, id := range []string{"instance-b", "instance-c"} {
		if err := m.RegisterInstance(context.Background(), InstanceConfig{ID: id, PackageID: "fixture", Provider: "fixture", Account: "cluster-a"}, &runtimeFixture{manifest: fixtureManifest()}, nil); err != nil {
			t.Fatal(err)
		}
	}
	_, err := m.Resolve(Route{Capability: CapabilityActionPlan, Provider: "fixture", Account: "cluster-a", ResourceType: "fixture.resource", Operation: "resize", Mode: "pr"})
	if !errors.Is(err, ErrAmbiguous) {
		t.Fatal(err)
	}
	if m.RegisterInstance(context.Background(), InstanceConfig{ID: "instance-a", PackageID: "fixture"}, &runtimeFixture{manifest: fixtureManifest()}, nil) == nil {
		t.Fatal("duplicate accepted")
	}
}
func TestRuntimeFailureContainment(t *testing.T) {
	for name, fn := range map[string]func(context.Context, ActionInput) (ActionPlan, error){"panic": func(context.Context, ActionInput) (ActionPlan, error) { panic("fixture-private-secret") }, "invalid": func(context.Context, ActionInput) (ActionPlan, error) { return ActionPlan{PluginID: "other"}, nil }, "raw failure": func(context.Context, ActionInput) (ActionPlan, error) {
		return ActionPlan{}, errors.New("fixture-private-secret")
	}, "timeout": func(ctx context.Context, _ ActionInput) (ActionPlan, error) {
		<-ctx.Done()
		return ActionPlan{}, ctx.Err()
	}} {
		t.Run(name, func(t *testing.T) {
			m, p := registered(t, &runtimeFixture{manifest: fixtureManifest(), plan: fn}, 10*time.Millisecond)
			_, err := p.Plan(context.Background(), fixtureInput())
			var safe *RuntimeError
			if !errors.As(err, &safe) || strings.Contains(err.Error(), "secret") {
				t.Fatalf("unsafe error: %v", err)
			}
			if m.InstanceStatuses()[0].LastFailure == nil {
				t.Fatal("failure not recorded")
			}
		})
	}
	m, p := registered(t, &runtimeFixture{manifest: fixtureManifest()}, time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Plan(ctx, fixtureInput()); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	_ = m
}
func TestRuntimeDrainWaitsForUncooperativeProvider(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	m, p := registered(t, &runtimeFixture{manifest: fixtureManifest(), plan: func(context.Context, ActionInput) (ActionPlan, error) {
		close(started)
		<-release
		return ActionPlan{}, nil
	}}, 10*time.Millisecond)
	done := make(chan error, 1)
	go func() { _, err := p.Plan(context.Background(), fixtureInput()); done <- err }()
	<-started
	if err := <-done; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := m.Drain(ctx, "instance-a"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if _, err := p.Plan(context.Background(), fixtureInput()); err == nil {
		t.Fatal("accepted call while draining")
	}
	close(release)
	if err := m.Drain(context.Background(), "instance-a"); err != nil {
		t.Fatal(err)
	}
	if m.InstanceStatuses()[0].State != StateStopped {
		t.Fatal("not stopped")
	}
}
func TestRuntimeLifecycleSoak(t *testing.T) {
	for i := 0; i < 100; i++ {
		m, p := registered(t, &runtimeFixture{manifest: fixtureManifest()}, time.Second)
		var wg sync.WaitGroup
		for j := 0; j < 8; j++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if _, err := p.Plan(context.Background(), fixtureInput()); err != nil {
					t.Error(err)
				}
			}()
		}
		wg.Wait()
		if err := m.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
		if m.InstanceStatuses()[0].Active != 0 {
			t.Fatal("leaked invocation")
		}
	}
}

func TestRuntimeStartupIsolationConfigurationAndSecrets(t *testing.T) {
	for _, required := range []bool{false, true} {
		t.Run(map[bool]string{false: "optional", true: "required"}[required], func(t *testing.T) {
			m := NewManager()
			f := &runtimeFixture{manifest: fixtureManifest(), init: func(context.Context) error { return errors.New("fixture-private-must-never-escape") }}
			err := m.RegisterInstance(context.Background(), InstanceConfig{ID: "broken", PackageID: "fixture", Required: required}, f, nil)
			if (err != nil) != required {
				t.Fatalf("startup error: %v", err)
			}
			if err != nil && strings.Contains(err.Error(), "fixture-private") {
				t.Fatal("leaked initializer error")
			}
			status := m.InstanceStatuses()[0]
			if status.State != StateFailed || status.LastFailure == nil {
				t.Fatalf("missing failed instance: %+v", status)
			}
			if _, err := m.ActionPlugin("broken", "fixture.resource", "resize"); !errors.Is(err, ErrNotFound) {
				t.Fatal("failed capability exposed")
			}
			if err := m.RegisterInstance(context.Background(), InstanceConfig{ID: "working", PackageID: "fixture"}, &runtimeFixture{manifest: fixtureManifest()}, nil); err != nil {
				t.Fatal(err)
			}
			p, _ := m.ActionPlugin("working", "fixture.resource", "resize")
			if _, err := p.Plan(context.Background(), fixtureInput()); err != nil {
				t.Fatal("unrelated capability failed", err)
			}
		})
	}
	fixtureInvalidRefs := map[string]string{"access": "../invalid-reference"}
	for name, cfg := range map[string]InstanceConfig{"null configuration": {Configuration: json.RawMessage(`null`)}, "array configuration": {Configuration: json.RawMessage(`[]`)}, "bad timeout": {CallTimeout: "-1s"}, "bad reference": {SecretRefs: fixtureInvalidRefs}, "wrong package": {PackageID: "other"}} {
		t.Run(name, func(t *testing.T) {
			cfg.ID = "invalid"
			if cfg.PackageID == "" {
				cfg.PackageID = "fixture"
			}
			cfg.Required = true
			if NewManager().RegisterInstance(context.Background(), cfg, &runtimeFixture{manifest: fixtureManifest()}, nil) == nil {
				t.Fatal("invalid configuration accepted")
			}
		})
	}
}

func TestRuntimeInvocationIdentityAndOptionalInterfaces(t *testing.T) {
	expected := InvocationIdentity{Actor: "operator", OperationID: "operation", WorkflowID: "workflow", CorrelationID: "request"}
	f := &runtimeFixture{manifest: fixtureManifest(), plan: func(ctx context.Context, in ActionInput) (ActionPlan, error) {
		if InvocationFrom(ctx) != expected {
			t.Error("identity lost")
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Error("deadline missing")
		}
		return ActionPlan{PluginID: "fixture", ResourceID: in.Resource.ID, ActionType: in.ActionType}, nil
	}}
	_, p := registered(t, f, time.Second)
	if _, ok := p.(RecoverableActionPlugin); ok {
		t.Fatal("invented recovery support")
	}
	if _, ok := p.(PreflightActionPlugin); ok {
		t.Fatal("invented preflight support")
	}
	if _, ok := p.(ArtifactIdentity); ok {
		t.Fatal("invented artifact identity")
	}
	if _, err := p.Plan(WithInvocation(context.Background(), expected), fixtureInput()); err != nil {
		t.Fatal(err)
	}
	in := fixtureInput()
	in.Mode = "automatic"
	if _, err := p.Plan(context.Background(), in); !errors.Is(err, ErrUnsupported) {
		t.Fatal("mode constraint bypassed", err)
	}
}

func TestRuntimeReloadDoesNotReplaceActiveOrApprovedInstance(t *testing.T) {
	m := NewManager()
	manifest := fixtureManifest()
	if err := m.RegisterPackage(PackageFactory{Manifest: manifest, Configure: func(context.Context, json.RawMessage) (Plugin, error) {
		return &runtimeFixture{manifest: manifest}, nil
	}}); err != nil {
		t.Fatal(err)
	}
	cfg := InstanceConfig{ID: "original", PackageID: "fixture", Account: "cluster-a"}
	if err := m.Install(context.Background(), cfg, nil); err != nil {
		t.Fatal(err)
	}
	if err := m.Reload(context.Background(), []InstanceConfig{cfg}, nil); err != nil {
		t.Fatal("unchanged reload", err)
	}
	changed := cfg
	changed.Account = "cluster-b"
	if m.Reload(context.Background(), []InstanceConfig{changed}, nil) == nil {
		t.Fatal("replaced instance configuration")
	}
	if m.Reload(context.Background(), nil, nil) == nil {
		t.Fatal("removed instance")
	}
	added := cfg
	added.ID = "additional"
	if err := m.Reload(context.Background(), []InstanceConfig{cfg, added}, nil); err != nil {
		t.Fatal(err)
	}
	p, _ := m.ActionPlugin("original", "fixture.resource", "resize")
	plan, err := p.Plan(context.Background(), fixtureInput())
	if err != nil {
		t.Fatal(err)
	}
	plan.Runtime.ConfigurationID = "changed"
	if _, err := p.Execute(context.Background(), plan); err == nil {
		t.Fatal("approved generation mismatch accepted")
	}
}

type shutdownFixture struct {
	*runtimeFixture
	shutdown func(context.Context) error
}

func (f *shutdownFixture) Shutdown(ctx context.Context) error { return f.shutdown(ctx) }
func TestRuntimeShutdownFailureIsContained(t *testing.T) {
	for _, mode := range []string{"panic", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			f := &shutdownFixture{runtimeFixture: &runtimeFixture{manifest: fixtureManifest()}, shutdown: func(ctx context.Context) error {
				if mode == "panic" {
					panic("secret")
				}
				<-ctx.Done()
				return ctx.Err()
			}}
			m := NewManager()
			if err := m.RegisterInstance(context.Background(), InstanceConfig{ID: "shutdown", PackageID: "fixture", Timeout: time.Millisecond}, f, nil); err != nil {
				t.Fatal(err)
			}
			err := m.Shutdown(context.Background())
			var failure *RuntimeError
			if !errors.As(err, &failure) || strings.Contains(err.Error(), "secret") {
				t.Fatal(err)
			}
			if m.InstanceStatuses()[0].State == StateStopped {
				t.Fatal("falsely reported successful shutdown")
			}
		})
	}
}

type runtimeMetricsFixture struct{ manifest Manifest }

func (f *runtimeMetricsFixture) ID() string                    { return f.manifest.ID }
func (f *runtimeMetricsFixture) Manifest() Manifest            { return f.manifest }
func (f *runtimeMetricsFixture) Health(context.Context) Health { return Health{Status: "healthy"} }
func (f *runtimeMetricsFixture) ReadMetrics(_ context.Context, r resource.Resource) (MetricsSnapshot, error) {
	now := time.Now().UTC()
	value := 2.0
	record, err := evidence.Seal(evidence.Record{ResourceID: r.ID, PluginID: f.ID(), Signal: "cpu", Query: "fixture", Kind: evidence.Aggregated, Aggregation: "p95", Unit: "cores", OriginalUnit: "cores", Scale: 1, Value: &value, SampleCount: 12, WindowStart: now.Add(-time.Hour), WindowEnd: now, CollectedAt: now, FreshUntil: now.Add(time.Minute), Quality: evidence.Valid})
	if err != nil {
		return MetricsSnapshot{}, err
	}
	derived := record
	derived.ID = ""
	derived.Kind = evidence.Derived
	derived.Signal = "cpu_peak"
	derived.Parents = []string{record.ID}
	derived, err = evidence.Seal(derived)
	return MetricsSnapshot{PluginID: f.ID(), ResourceID: r.ID, Window: "1h", CollectedAt: now, Signals: map[string]any{"cpu": value, "cpu_peak": value}, Records: []evidence.Record{record, derived}}, err
}
func TestRuntimeResealsEvidenceWithInstanceProvenance(t *testing.T) {
	manifest := fixtureManifest()
	manifest.Category = CategoryMetrics
	manifest.CanMutateInfrastructure = false
	manifest.Capabilities = []string{CapabilityMetricsRead}
	manifest.SupportedActionTypes = nil
	manifest.Runtime.Capabilities = []CapabilityDescriptor{{Name: CapabilityMetricsRead, Version: "1.0.0", ResourceTypes: manifest.SupportedResourceTypes}}
	m := NewManager()
	if err := m.RegisterInstance(context.Background(), InstanceConfig{ID: "metrics-a", PackageID: "fixture", Provider: "fixture", Account: "cluster-a"}, &runtimeMetricsFixture{manifest: manifest}, nil); err != nil {
		t.Fatal(err)
	}
	p, err := m.MetricsPlugin("metrics-a", "fixture.resource")
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := p.ReadMetrics(context.Background(), fixtureInput().Resource)
	if err != nil {
		t.Fatal(err)
	}
	if err := snapshot.ValidateRecords(); err != nil {
		t.Fatal(err)
	}
	for _, r := range snapshot.Records {
		if r.Producer == nil || r.Producer.InstanceID != "metrics-a" || r.PluginID != "metrics-a" || r.Validate() != nil {
			t.Fatal("missing sealed provenance", r)
		}
	}
	if snapshot.Records[1].Parents[0] != snapshot.Records[0].ID {
		t.Fatal("derived provenance still points to the old identity")
	}
	snapshot.Records[0].Producer.InstanceID = "tampered"
	if snapshot.Records[0].Validate() == nil {
		t.Fatal("provenance is not part of the content hash")
	}
}
func TestRuntimeIncompatibleOptionalInstanceRemainsVisible(t *testing.T) {
	m := NewManager()
	manifest := fixtureManifest()
	manifest.Runtime.SDKRange = ">=2.0.0"
	if err := m.RegisterInstance(context.Background(), InstanceConfig{ID: "incompatible", PackageID: "fixture"}, &runtimeFixture{manifest: manifest}, nil); err != nil {
		t.Fatal(err)
	}
	if m.InstanceStatuses()[0].State != StateIncompatible {
		t.Fatal("incompatible package disappeared")
	}
	if _, err := m.ActionPlugin("incompatible", "fixture.resource", "resize"); !errors.Is(err, ErrNotFound) {
		t.Fatal("incompatible code can execute")
	}
}

type panickingDiagnostic struct{}

func (panickingDiagnostic) Error() string                   { return "fixture-private-error" }
func (panickingDiagnostic) ProviderFailure() FailureDetails { panic("fixture-private-diagnostic") }
func TestRuntimeContainsPanickingErrorMethods(t *testing.T) {
	_, p := registered(t, &runtimeFixture{manifest: fixtureManifest(), plan: func(context.Context, ActionInput) (ActionPlan, error) { return ActionPlan{}, panickingDiagnostic{} }}, time.Second)
	_, err := p.Plan(context.Background(), fixtureInput())
	var failure *RuntimeError
	if !errors.As(err, &failure) || failure.Code != FailureInternal || strings.Contains(err.Error(), "private") {
		t.Fatal("diagnostic callback escaped containment", err)
	}
}

type blockingJournal struct {
	hold    atomic.Bool
	once    sync.Once
	started chan struct{}
	release chan struct{}
}

func (*blockingJournal) LoadPluginStatuses(context.Context) ([]InstanceStatus, error) {
	return nil, nil
}
func (j *blockingJournal) SavePluginStatus(ctx context.Context, s InstanceStatus) error {
	if j.hold.Load() && s.State == StateReady {
		j.once.Do(func() { close(j.started) })
		select {
		case <-j.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}
func TestDrainIncludesInvocationStatusCommit(t *testing.T) {
	j := &blockingJournal{started: make(chan struct{}), release: make(chan struct{})}
	m := NewManager()
	if err := m.SetJournal(context.Background(), j); err != nil {
		t.Fatal(err)
	}
	if err := m.RegisterInstance(context.Background(), InstanceConfig{ID: "instance-a", PackageID: "fixture"}, &runtimeFixture{manifest: fixtureManifest()}, nil); err != nil {
		t.Fatal(err)
	}
	p, _ := m.ActionPlugin("instance-a", "fixture.resource", "resize")
	j.hold.Store(true)
	done := make(chan error, 1)
	go func() { _, err := p.Plan(context.Background(), fixtureInput()); done <- err }()
	<-j.started
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := m.Drain(ctx, "instance-a"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("drain overtook the invocation journal commit", err)
	}
	close(j.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := m.Drain(context.Background(), "instance-a"); err != nil {
		t.Fatal(err)
	}
}

type artifactFixture struct{ *runtimeFixture }

func (*artifactFixture) ArtifactDigest() string { return strings.Repeat("a", 64) }
func TestRuntimeRejectsManifestArtifactMismatch(t *testing.T) {
	manifest := fixtureManifest()
	manifest.Runtime.Distribution = "external"
	manifest.Runtime.Integrity = "sha256:" + strings.Repeat("b", 64)
	m := NewManager()
	err := m.RegisterInstance(context.Background(), InstanceConfig{ID: "artifact", PackageID: "fixture", Required: true}, &artifactFixture{&runtimeFixture{manifest: manifest}}, nil)
	var failure *RuntimeError
	if !errors.As(err, &failure) || failure.Code != FailureIncompatible {
		t.Fatal("manifest integrity expectation was ignored", err)
	}
	if _, err := m.ActionPlugin("artifact", "fixture.resource", "resize"); !errors.Is(err, ErrNotFound) {
		t.Fatal("mismatched artifact activated")
	}
}
