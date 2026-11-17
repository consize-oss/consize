package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/consize-oss/consize/internal/bootstrap"
	"github.com/consize-oss/consize/internal/store"
	"github.com/consize-oss/consize/pkg/plugin"
	"github.com/consize-oss/consize/pkg/plugins/kubernetes"
	"github.com/consize-oss/consize/pkg/resource"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

type scopedFixture struct {
	calls   atomic.Int32
	mode    string
	failure error
}

func (*scopedFixture) ID() string { return "fixture-discovery" }
func (p *scopedFixture) Manifest() plugin.Manifest {
	return plugin.Manifest{ID: p.ID(), Category: plugin.CategoryDataSource, Capabilities: []string{plugin.CapabilityResourceDiscover}, SupportedResourceTypes: []string{resource.TypeKubernetesDeployment}}
}
func (*scopedFixture) Health(context.Context) plugin.Health { return plugin.Health{Status: "healthy"} }
func (*scopedFixture) DiscoveryScope() map[string]string {
	return map[string]string{"cluster_id": "fixture-cluster", "location": "fixture-location", "namespaces": "fixture", "label_selector": ""}
}
func (p *scopedFixture) Discover(ctx context.Context) ([]plugin.ProviderObservation, error) {
	s, e := p.DiscoverSnapshot(ctx)
	return s.Observations, e
}
func (p *scopedFixture) DiscoverSnapshot(ctx context.Context) (plugin.DiscoverySnapshot, error) {
	p.calls.Add(1)
	if p.mode == "block" {
		<-ctx.Done()
		return plugin.DiscoverySnapshot{}, ctx.Err()
	}
	if p.failure != nil {
		return plugin.DiscoverySnapshot{}, p.failure
	}
	start := time.Now().UTC()
	scope := p.DiscoveryScope()
	if p.mode == "scope" {
		scope["namespaces"] = "other"
	}
	digest, _ := store.DiscoveryScopeIdentity(p.ID(), scope)
	finish := time.Now().UTC()
	s := plugin.DiscoverySnapshot{Scan: plugin.DiscoveryScan{Version: plugin.DiscoveryScanVersion, PluginID: p.ID(), ID: "fixture-scan", Scope: scope, ScopeDigest: digest, StartedAt: start, FinishedAt: finish, Complete: true}}
	if p.mode == "partial" {
		s.Scan.Complete = false
	}
	if p.mode != "empty" {
		s.Observations = []plugin.ProviderObservation{{PluginID: p.ID(), Provider: resource.ProviderKubernetes, ObservedAt: finish, Resource: resource.Resource{Provider: resource.ProviderKubernetes, Type: resource.TypeKubernetesDeployment, Account: "fixture-cluster", Region: "fixture-location", ProviderResourceID: "fixture/app", Name: "app", Owner: "fixture-team", Environment: resource.EnvDevelopment, Criticality: resource.CriticalityLow, Metadata: map[string]any{"namespace": "fixture"}}}}
		s.Scan.Count = 1
	}
	return s, nil
}
func schedulerFixture(t *testing.T, p *scopedFixture, c bootstrap.DiscoveryScheduleConfig) (*Scheduler, *store.Memory) {
	t.Helper()
	st := store.NewMemory()
	m := plugin.NewManager()
	if err := m.RegisterDiscovery(p); err != nil {
		t.Fatal(err)
	}
	s, err := NewScheduler(st, m, c)
	if err != nil {
		t.Fatal(err)
	}
	return s, st
}
func TestScheduledAndManualDiscoveryShareOneOperation(t *testing.T) {
	p := &scopedFixture{}
	s, st := schedulerFixture(t, p, bootstrap.DiscoveryScheduleConfig{Enabled: true})
	ctx := context.Background()
	job, err := s.Request(ctx, p.ID(), "fixture-admin")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err = s.Tick(ctx); err != nil {
			t.Fatal(err)
		}
	}
	done, _ := st.GetDiscoveryRun(ctx, job.ID)
	if done.State != "succeeded" || p.calls.Load() != 1 {
		t.Fatalf("state=%s calls=%d", done.State, p.calls.Load())
	}
	rows, _ := st.ListResources(ctx)
	if len(rows) != 1 || rows[0].SourcePluginID != p.ID() {
		t.Fatal("discovery did not register resource")
	}
}
func TestDiscoveryFailureMatrixAndNoRegistryEffects(t *testing.T) {
	for _, tc := range []struct {
		name, mode  string
		err         error
		state, code string
	}{
		{"partial", "partial", nil, "failed", "incomplete_scan"}, {"scope", "scope", nil, "failed", "scope_changed"},
		{"unavailable", "", &kubernetes.ScanError{Code: "unavailable"}, "retry_wait", "unavailable"},
		{"access", "", &kubernetes.ScanError{Code: "forbidden"}, "failed", "authorization"},
		{"query config", "", &kubernetes.ScanError{Code: "configuration"}, "failed", "configuration"},
		{"private error", "", errors.New("fixture-private-never-persist"), "failed", "provider_failure"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &scopedFixture{mode: tc.mode, failure: tc.err}
			s, st := schedulerFixture(t, p, bootstrap.DiscoveryScheduleConfig{})
			ctx := context.Background()
			job, err := s.Request(ctx, p.ID(), "actor")
			if err != nil {
				t.Fatal(err)
			}
			if err = s.Tick(ctx); err != nil {
				t.Fatal(err)
			}
			done, _ := st.GetDiscoveryRun(ctx, job.ID)
			if done.State != tc.state || done.ErrorCode != tc.code {
				t.Fatal(done)
			}
			rows, _ := st.ListResources(ctx)
			if len(rows) != 0 {
				t.Fatal("failure registered observations")
			}
			raw, _ := json.Marshal(done)
			if strings.Contains(string(raw), "fixture-private-never-persist") {
				t.Fatal("raw provider error leaked")
			}
			if done.State == "retry_wait" {
				delay := done.NextRun.Sub(done.UpdatedAt)
				if delay < 4*time.Second || delay > 6*time.Second {
					t.Fatal("unbounded retry", delay)
				}
			}
		})
	}
}
func TestSynchronousDiscoveryUsesGuardedDurablePath(t *testing.T) {
	p := &scopedFixture{}
	s, st := schedulerFixture(t, p, bootstrap.DiscoveryScheduleConfig{})
	result, err := New(st, s.plugins).Run(context.Background())
	if err != nil || len(result) != 1 || result[0].Count != 1 {
		t.Fatal(result, err)
	}
	runs, _ := st.ListDiscoveryRuns(context.Background(), store.DiscoveryRunQuery{})
	if len(runs) != 1 || runs[0].State != "succeeded" {
		t.Fatal("compatibility path bypassed durable run")
	}
}
func TestDiscoveryBoundsAndJitter(t *testing.T) {
	for _, cfg := range []bootstrap.DiscoveryScheduleConfig{{MaxAttempts: 11}, {MaxRunsPerTick: 101}, {Interval: "0s"}, {Timeout: "5m"}, {StaleAfter: "2h", TombstoneAfter: "1h"}} {
		if _, err := NewScheduler(store.NewMemory(), plugin.NewManager(), cfg); err == nil {
			t.Fatal("invalid schedule accepted", cfg)
		}
	}
	if _, err := NewScheduler(store.NewMemory(), plugin.NewManager(), bootstrap.DiscoveryScheduleConfig{Enabled: true}); err == nil {
		t.Fatal("enabled schedule without scoped provider accepted")
	}
	for i := 0; i < 100; i++ {
		key := time.Unix(int64(i), 0).String()
		j := discoveryJitter(key, time.Second)
		if j < 0 || j >= time.Second || j != discoveryJitter(key, time.Second) {
			t.Fatal("invalid jitter", j)
		}
	}
}

func TestScheduledDiscoveryWithKubernetesClientCreateUpdateDelete(t *testing.T) {
	ctx := context.Background()
	replicas := int32(1)
	deployment := func(ns, uid string) *appsv1.Deployment {
		return &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: "app", UID: types.UID(uid), Generation: 1}, Spec: appsv1.DeploymentSpec{Replicas: &replicas, Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "app"}}}}}}
	}
	client := fake.NewSimpleClientset(deployment("fixture", "fixture-uid"), deployment("other", "other-uid"))
	client.PrependReactor("list", "deployments", func(action ktesting.Action) (bool, runtime.Object, error) {
		obj, err := client.Tracker().List(schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}, schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}, action.GetNamespace())
		if err != nil {
			return true, nil, err
		}
		list := obj.(*appsv1.DeploymentList)
		list.ResourceVersion = "fixture-version"
		return true, list, nil
	})
	provider, err := kubernetes.NewWithPatcherAndConfig(kubernetes.NewK8sPatcherFromClient(client), kubernetes.Config{ClusterID: "fixture-cluster", Location: "fixture-location", Discovery: kubernetes.DiscoveryConfig{Namespaces: []string{"fixture"}}})
	if err != nil {
		t.Fatal(err)
	}
	manager := plugin.NewManager()
	if err = manager.RegisterDiscovery(provider); err != nil {
		t.Fatal(err)
	}
	st := store.NewMemory()
	service := NewWithConfig(st, manager, bootstrap.DiscoveryScheduleConfig{StaleAfter: "0s", TombstoneAfter: "1ms"})
	first, err := service.Run(ctx)
	if err != nil || len(first) != 1 || first[0].Count != 1 {
		t.Fatal(first, err)
	}
	id := first[0].Resources[0].ID
	item, err := client.AppsV1().Deployments("fixture").Get(ctx, "app", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	item.Labels = map[string]string{"team": "fixture-updated"}
	client.AppsV1().Deployments("fixture").Update(ctx, item, metav1.UpdateOptions{})
	// New manual requests are unique by request time, while scope exclusion still applies.
	time.Sleep(2 * time.Millisecond)
	if _, err = service.Run(ctx); err != nil {
		t.Fatal(err)
	}
	row, _ := st.GetResource(ctx, id)
	if row.Labels["team"] != "fixture-updated" {
		t.Fatal("metadata did not update")
	}
	client.AppsV1().Deployments("fixture").Delete(ctx, "app", metav1.DeleteOptions{})
	time.Sleep(2 * time.Millisecond)
	if _, err = service.Run(ctx); err != nil {
		t.Fatal(err)
	}
	row, _ = st.GetResource(ctx, id)
	if row.LifecycleState != resource.LifecycleStale {
		t.Fatal("complete absence did not mark stale")
	}
	time.Sleep(2 * time.Millisecond)
	if _, err = service.Run(ctx); err != nil {
		t.Fatal(err)
	}
	row, _ = st.GetResource(ctx, id)
	if row.LifecycleState != resource.LifecycleDeleted {
		t.Fatal("confirmed absence did not tombstone")
	}
	rows, _ := st.ListResources(ctx)
	if len(rows) != 1 {
		t.Fatal("namespace scope leaked", rows)
	}
}

func TestDiscoverySchedulerResumesAfterFileStoreRestart(t *testing.T) {
	p := &scopedFixture{}
	template, _ := schedulerFixture(t, p, bootstrap.DiscoveryScheduleConfig{})
	path := filepath.Join(t.TempDir(), "state.json")
	st, err := store.OpenDurable(path)
	if err != nil {
		t.Fatal(err)
	}
	first, err := NewScheduler(st, template.plugins, template.cfg)
	if err != nil {
		t.Fatal(err)
	}
	job, err := first.Request(context.Background(), p.ID(), "fixture-admin")
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := st.ClaimDiscovery(context.Background(), job.ID, "fixture-crashed", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	st.Close()
	st, err = store.OpenDurable(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	restarted, err := NewScheduler(st, template.plugins, template.cfg)
	if err != nil {
		t.Fatal(err)
	}
	timer := time.NewTimer(time.Until(claimed.Lease.Until) + 20*time.Millisecond)
	defer timer.Stop()
	<-timer.C
	if err = restarted.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	done, err := st.GetDiscoveryRun(context.Background(), job.ID)
	if err != nil || done.State != "succeeded" || done.Attempts != 2 || done.History[0].Outcome != "interrupted" || p.calls.Load() != 1 {
		t.Fatal("restart failed", done, err)
	}
	if err = restarted.Tick(context.Background()); err != nil || p.calls.Load() != 1 {
		t.Fatal("completed scan repeated", err)
	}
}
