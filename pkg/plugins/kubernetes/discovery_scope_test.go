package kubernetes

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/consize-oss/consize/pkg/plugin"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

type discoveryTestClient struct {
	mu    sync.Mutex
	calls []string
	list  func(context.Context, string, metav1.ListOptions) (*appsv1.DeploymentList, error)
}

func (*discoveryTestClient) PatchDeployment(context.Context, string, string, PatchDiff) error {
	return errors.New("discovery must not mutate")
}
func (*discoveryTestClient) ReadDeploymentResources(context.Context, string, string, string) (int64, int64, error) {
	return 0, 0, nil
}
func (*discoveryTestClient) Health(context.Context) error { return nil }
func (c *discoveryTestClient) ListDeploymentPage(ctx context.Context, namespace string, options metav1.ListOptions) (*appsv1.DeploymentList, error) {
	c.mu.Lock()
	c.calls = append(c.calls, namespace+":"+options.Continue)
	c.mu.Unlock()
	return c.list(ctx, namespace, options)
}
func deploymentFixture(namespace, name, uid string) appsv1.Deployment {
	return appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name, UID: types.UID(uid), ResourceVersion: "12", Labels: map[string]string{"app": "chosen"}}, Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "app"}}}}}}
}
func discoveryPlugin(t *testing.T, client *discoveryTestClient, cfg DiscoveryConfig) *Plugin {
	t.Helper()
	p, err := NewWithPatcherAndConfig(client, Config{ClusterID: "cluster-one", Location: "local", Discovery: cfg})
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func page(items ...appsv1.Deployment) *appsv1.DeploymentList {
	return &appsv1.DeploymentList{ListMeta: metav1.ListMeta{ResourceVersion: "100"}, Items: items}
}
func requireScanCode(t *testing.T, err error, code string) {
	t.Helper()
	var failure *ScanError
	if !errors.As(err, &failure) || failure.Code != code {
		t.Fatalf("expected %s, got %v", code, err)
	}
}
func TestScopedPaginatedDiscoveryIsDeterministic(t *testing.T) {
	client := &discoveryTestClient{list: func(_ context.Context, namespace string, options metav1.ListOptions) (*appsv1.DeploymentList, error) {
		if options.LabelSelector != "app=chosen" || options.Limit != 1 {
			t.Fatalf("scope not sent: %+v", options)
		}
		switch {
		case namespace == "alpha" && options.Continue == "":
			p := page(deploymentFixture("alpha", "b", "uid-b"))
			p.Continue = "next"
			return p, nil
		case namespace == "alpha" && options.Continue == "next":
			return page(deploymentFixture("alpha", "a", "uid-a")), nil
		case namespace == "beta":
			return page(deploymentFixture("beta", "a", "uid-c")), nil
		}
		t.Fatal("out of scope request")
		return nil, nil
	}}
	p := discoveryPlugin(t, client, DiscoveryConfig{Namespaces: []string{"beta", "alpha"}, LabelSelector: "app=chosen", PageSize: 1})
	snapshot, err := p.DiscoverSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Scan.Version != plugin.DiscoveryScanVersion || !snapshot.Scan.Complete || snapshot.Scan.Count != 3 || snapshot.Scan.Pages != 3 || snapshot.Scan.Scope["namespaces"] != "alpha,beta" || len(snapshot.Scan.SnapshotVersions) != 2 {
		t.Fatalf("incomplete scan: %+v", snapshot.Scan)
	}
	got := []string{}
	for _, observation := range snapshot.Observations {
		got = append(got, observation.Resource.ProviderResourceID)
		if observation.Resource.Account != "cluster-one" || observation.Resource.Metadata["uid"] == "" {
			t.Fatal("identity missing")
		}
	}
	if strings.Join(got, ",") != "alpha/a,alpha/b,beta/a" {
		t.Fatal(got)
	}
	first := snapshot.Scan
	snapshot, err = p.DiscoverSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == snapshot.Scan.ID || first.ScopeDigest != snapshot.Scan.ScopeDigest {
		t.Fatal("scan identity or immutable scope binding invalid")
	}
	diagnostics := p.DiscoveryDiagnostics()
	if diagnostics.LastSuccessfulScan == nil || diagnostics.LastSuccessfulScan.ID != snapshot.Scan.ID {
		t.Fatal("missing last success")
	}
	diagnostics.LastSuccessfulScan.Scope["namespaces"] = "altered"
	if p.DiscoveryDiagnostics().LastSuccessfulScan.Scope["namespaces"] == "altered" {
		t.Fatal("diagnostics alias internal state")
	}
}
func TestDiscoveryRejectsIncompleteAndAmbiguousPages(t *testing.T) {
	for _, tc := range []struct {
		name, code string
		list       func(context.Context, string, metav1.ListOptions) (*appsv1.DeploymentList, error)
	}{
		{"wrong namespace", "invalid_observation", func(context.Context, string, metav1.ListOptions) (*appsv1.DeploymentList, error) {
			return page(deploymentFixture("other", "a", "uid")), nil
		}},
		{"missing uid", "invalid_observation", func(context.Context, string, metav1.ListOptions) (*appsv1.DeploymentList, error) {
			return page(deploymentFixture("alpha", "a", "")), nil
		}},
		{"selector mismatch", "invalid_observation", func(context.Context, string, metav1.ListOptions) (*appsv1.DeploymentList, error) {
			d := deploymentFixture("alpha", "a", "uid")
			d.Labels["app"] = "other"
			return page(d), nil
		}},
		{"missing snapshot version", "invalid_page", func(context.Context, string, metav1.ListOptions) (*appsv1.DeploymentList, error) {
			p := page()
			p.ResourceVersion = ""
			return p, nil
		}},
		{"snapshot changes", "invalid_page", func(_ context.Context, _ string, o metav1.ListOptions) (*appsv1.DeploymentList, error) {
			p := page()
			if o.Continue == "" {
				p.Continue = "next"
			} else {
				p.ResourceVersion = "new"
			}
			return p, nil
		}},
		{"continuation cycle", "invalid_page", func(context.Context, string, metav1.ListOptions) (*appsv1.DeploymentList, error) {
			p := page()
			p.Continue = "same"
			return p, nil
		}},
		{"duplicate uid", "invalid_observation", func(context.Context, string, metav1.ListOptions) (*appsv1.DeploymentList, error) {
			return page(deploymentFixture("alpha", "a", "uid"), deploymentFixture("alpha", "b", "uid")), nil
		}},
		{"duplicate identity", "invalid_observation", func(context.Context, string, metav1.ListOptions) (*appsv1.DeploymentList, error) {
			return page(deploymentFixture("alpha", "a", "uid"), deploymentFixture("alpha", "a", "other")), nil
		}},
		{"forbidden", "forbidden", func(context.Context, string, metav1.ListOptions) (*appsv1.DeploymentList, error) {
			return nil, apierrors.NewForbidden(schema.GroupResource{Resource: "deployments"}, "fixture", errors.New("fixture-private-details"))
		}},
		{"unauthorized", "unauthorized", func(context.Context, string, metav1.ListOptions) (*appsv1.DeploymentList, error) {
			return nil, apierrors.NewUnauthorized("fixture-private-details")
		}},
		{"expired continuation", "expired_continue", func(context.Context, string, metav1.ListOptions) (*appsv1.DeploymentList, error) {
			return nil, apierrors.NewResourceExpired("fixture-private-details")
		}},
		{"unavailable", "unavailable", func(context.Context, string, metav1.ListOptions) (*appsv1.DeploymentList, error) {
			return nil, apierrors.NewServiceUnavailable("fixture-private-details")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := discoveryPlugin(t, &discoveryTestClient{list: tc.list}, DiscoveryConfig{Namespaces: []string{"alpha"}, LabelSelector: "app=chosen"})
			snapshot, err := p.DiscoverSnapshot(context.Background())
			requireScanCode(t, err, tc.code)
			if snapshot.Scan.Complete || snapshot.Observations != nil || p.DiscoveryDiagnostics().LastSuccessfulScan != nil {
				t.Fatal("failed scan presented as complete")
			}
			encoded, _ := json.Marshal(p.DiscoveryDiagnostics())
			if strings.Contains(string(encoded), "fixture-private-details") || strings.Contains(err.Error(), "fixture-private-details") {
				t.Fatal("provider details leaked")
			}
		})
	}
}
func TestDiscoveryRetainsSuccessAcrossFailureAndDistinguishesEmpty(t *testing.T) {
	fail := false
	client := &discoveryTestClient{list: func(context.Context, string, metav1.ListOptions) (*appsv1.DeploymentList, error) {
		if fail {
			return nil, apierrors.NewForbidden(schema.GroupResource{Resource: "deployments"}, "fixture", errors.New("fixture"))
		}
		return page(), nil
	}}
	p := discoveryPlugin(t, client, DiscoveryConfig{})
	first, err := p.DiscoverSnapshot(context.Background())
	if err != nil || !first.Scan.Complete || first.Scan.Count != 0 || first.Observations == nil {
		t.Fatalf("empty scan: %+v %v", first, err)
	}
	fail = true
	second, err := p.DiscoverSnapshot(context.Background())
	requireScanCode(t, err, "forbidden")
	d := p.DiscoveryDiagnostics()
	if d.LastSuccessfulScan.ID != first.Scan.ID || d.LastAttempt.ID != second.Scan.ID || d.LastAttempt.Complete {
		t.Fatal("last attempt overwrote successful scan")
	}
	if p.Health(context.Background()).Status != "degraded" {
		t.Fatal("failed discovery hidden behind API reachability")
	}
}
func TestCancellationTimeoutBusyAndFailureDoNotEmitPartialResults(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	client := &discoveryTestClient{list: func(ctx context.Context, _ string, _ metav1.ListOptions) (*appsv1.DeploymentList, error) {
		close(entered)
		select {
		case <-release:
			return page(), nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}}
	p := discoveryPlugin(t, client, DiscoveryConfig{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if _, err := p.DiscoverSnapshot(context.Background()); err != nil {
			t.Error(err)
		}
	}()
	<-entered
	if !p.DiscoveryDiagnostics().InProgress {
		t.Fatal("active scan hidden")
	}
	_, err := p.DiscoverSnapshot(context.Background())
	requireScanCode(t, err, "scan_busy")
	close(release)
	wg.Wait()
	timeout := discoveryPlugin(t, &discoveryTestClient{list: func(ctx context.Context, _ string, _ metav1.ListOptions) (*appsv1.DeploymentList, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}}, DiscoveryConfig{Timeout: "10ms"})
	_, err = timeout.DiscoverSnapshot(context.Background())
	requireScanCode(t, err, "timeout")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("deadline sentinel lost")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = timeout.DiscoverSnapshot(ctx)
	requireScanCode(t, err, "canceled")
	// Even an empty success returned just after cancellation cannot authorize absence.
	ctx, cancel = context.WithCancel(context.Background())
	canceled := discoveryPlugin(t, &discoveryTestClient{list: func(context.Context, string, metav1.ListOptions) (*appsv1.DeploymentList, error) {
		cancel()
		return page(), nil
	}}, DiscoveryConfig{})
	snapshot, err := canceled.DiscoverSnapshot(ctx)
	requireScanCode(t, err, "canceled")
	if snapshot.Scan.Complete {
		t.Fatal("canceled empty scan completed")
	}
}
func TestDiscoveryConfigurationLimitsAndObservationMinimization(t *testing.T) {
	for _, cfg := range []DiscoveryConfig{{Namespaces: []string{""}}, {Namespaces: []string{"*"}}, {Namespaces: []string{"alpha", "alpha"}}, {Namespaces: []string{"UPPER"}}, {LabelSelector: "not valid???"}, {PageSize: 1001}, {MaxObjects: -1}, {MaxBytes: 1}, {Timeout: "bad"}, {Timeout: "0s"}, {AnnotationAllowlist: []string{"kubectl.kubernetes.io/last-applied-configuration"}}, {AnnotationAllowlist: []string{"example.io/secret"}}} {
		if _, err := discoveryDefaults(cfg); err == nil {
			t.Fatalf("accepted %+v", cfg)
		}
	}
	d := deploymentFixture("alpha", "a", "uid")
	d.Annotations = map[string]string{"example.io/team": "platform", "kubectl.kubernetes.io/last-applied-configuration": "fixture-private-manifest"}
	d.Spec.Template.Spec.Containers[0].Env = []corev1.EnvVar{{Name: "FIXTURE_PRIVATE_VALUE", Value: "fixture-private-environment"}}
	native, err := captureDeployment(d, "cluster-one", []string{"example.io/team"})
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(native)
	if strings.Contains(string(encoded), "fixture-private-manifest") || strings.Contains(string(encoded), "fixture-private-environment") {
		t.Fatal("private manifest values leaked")
	}
	if !strings.Contains(string(encoded), "platform") || native["uid"] != "uid" || native["version"] != "kubernetes-deployment-discovery-v1" {
		t.Fatal("capture missing")
	}

	for _, limits := range []DiscoveryConfig{{MaxObjects: 1}, {MaxBytes: 64 << 10}} {
		d2 := deploymentFixture("alpha", "b", "uid-b")
		if limits.MaxBytes > 0 {
			d2.Annotations = map[string]string{"example.io/large": strings.Repeat("x", 70<<10)}
		}
		p := discoveryPlugin(t, &discoveryTestClient{list: func(context.Context, string, metav1.ListOptions) (*appsv1.DeploymentList, error) {
			return page(d, d2), nil
		}}, limits)
		result, err := p.DiscoverSnapshot(context.Background())
		requireScanCode(t, err, "limit_exceeded")
		if result.Scan.Complete || result.Observations != nil {
			t.Fatal("bounded scan truncated into success")
		}
	}
}
