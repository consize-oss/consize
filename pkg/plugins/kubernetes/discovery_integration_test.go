package kubernetes

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// Mutations are fixture setup/cleanup only, under an explicitly isolated kind
// control plane. Discovery itself uses list-only credentials and never mutates.
func TestIsolatedKubernetesDiscovery(t *testing.T) {
	kubeconfig := os.Getenv("CONSIZE_DISCOVERY_TEST_KUBECONFIG")
	if kubeconfig == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("CI requires the isolated discovery test cluster")
		}
		t.Skip("set the explicit isolated kind test kubeconfig/context and fixture opt-in")
	}
	contextName := os.Getenv("CONSIZE_DISCOVERY_TEST_CONTEXT")
	if os.Getenv("CONSIZE_DISCOVERY_TEST_FIXTURES") != "1" || !strings.HasPrefix(contextName, "kind-consize-discovery-") || !filepath.IsAbs(kubeconfig) {
		t.Fatal("refusing unapproved discovery fixture target")
	}
	rules := &clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfig}
	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, &clientcmd.ConfigOverrides{CurrentContext: contextName}).ClientConfig()
	if err != nil {
		t.Fatal("load isolated discovery test configuration")
	}
	endpoint, err := url.Parse(cfg.Host)
	if err != nil || net.ParseIP(endpoint.Hostname()) == nil || !net.ParseIP(endpoint.Hostname()).IsLoopback() {
		t.Fatal("refusing non-loopback test API server")
	}
	cfg.Timeout = 10 * time.Second
	admin, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	suffix := make([]byte, 6)
	if _, err = rand.Read(suffix); err != nil {
		t.Fatal(err)
	}
	alpha, beta := "consize-discovery-a-"+hex.EncodeToString(suffix), "consize-discovery-b-"+hex.EncodeToString(suffix)
	for _, namespace := range []string{alpha, beta} {
		created, err := admin.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace, Labels: map[string]string{"consize.io/discovery-integration": "fixture"}}}, metav1.CreateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		uid := created.UID
		t.Cleanup(func() {
			cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
			defer stop()
			if err := admin.CoreV1().Namespaces().Delete(cleanup, namespace, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}}); err != nil {
				t.Error("cleanup isolated namespace:", err)
			}
		})
	}
	for _, entry := range []struct{ namespace, name, selected string }{{alpha, "api", "yes"}, {alpha, "other", "yes"}, {alpha, "excluded", "no"}, {beta, "api", "yes"}} {
		replicas := int32(0)
		labels := map[string]string{"discovery-test": entry.selected}
		fixture := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Namespace: entry.namespace, Name: entry.name, Labels: labels}, Spec: appsv1.DeploymentSpec{Replicas: &replicas, Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": entry.name}}, Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": entry.name}}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "example.invalid/consize-discovery-fixture@sha256:" + strings.Repeat("0", 64)}}}}}}
		if _, err := admin.AppsV1().Deployments(entry.namespace).Create(ctx, fixture, metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	p, err := New(Config{Kubeconfig: kubeconfig, Context: contextName, ClusterID: "isolated-discovery-test", Location: "local", Discovery: DiscoveryConfig{Namespaces: []string{beta, alpha}, LabelSelector: "discovery-test=yes", PageSize: 1}})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := p.DiscoverSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.Scan.Complete || snapshot.Scan.Count != 3 || snapshot.Scan.Pages < 3 || len(snapshot.Scan.SnapshotVersions) != 2 {
		t.Fatalf("real scoped/paged discovery: %+v", snapshot.Scan)
	}
	identities := map[string]bool{}
	for _, observation := range snapshot.Observations {
		uid, ok := observation.Resource.Metadata["uid"].(string)
		if !ok || uid == "" || identities[uid] {
			t.Fatal("missing/colliding native identity")
		}
		identities[uid] = true
	}
	// Bind only Deployment list permission in alpha to dedicated temporary identity.
	if _, err = admin.CoreV1().ServiceAccounts(alpha).Create(ctx, &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "discovery-reader"}}, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err = admin.RbacV1().Roles(alpha).Create(ctx, &rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: "discovery-reader"}, Rules: []rbacv1.PolicyRule{{APIGroups: []string{"apps"}, Resources: []string{"deployments"}, Verbs: []string{"list"}}}}, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	fixtureBinding, err := admin.RbacV1().RoleBindings(alpha).Create(ctx, &rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "discovery-reader"}, RoleRef: rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: "discovery-reader"}, Subjects: []rbacv1.Subject{{Kind: "ServiceAccount", Name: "discovery-reader", Namespace: alpha}}}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	expiration := int64(600)
	fixtureToken, err := admin.CoreV1().ServiceAccounts(alpha).CreateToken(ctx, "discovery-reader", &authenticationv1.TokenRequest{Spec: authenticationv1.TokenRequestSpec{ExpirationSeconds: &expiration}}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal("create isolated fixture identity:", err)
	}
	readerConfig := rest.AnonymousClientConfig(cfg)
	readerConfig.BearerToken = fixtureToken.Status.Token
	reader, err := kubernetes.NewForConfig(readerConfig)
	if err != nil {
		t.Fatal(err)
	}
	allowed, err := NewWithPatcherAndConfig(NewK8sPatcherFromClient(reader), Config{ClusterID: "isolated-discovery-test", Location: "local", Discovery: DiscoveryConfig{Namespaces: []string{alpha}, LabelSelector: "discovery-test=yes", PageSize: 1}})
	if err != nil {
		t.Fatal(err)
	}
	// RBAC informer propagation is asynchronous; retry only the setup permission
	// check, not failed production scans or expired continuation tokens.
	var success bool
	for i := 0; i < 40; i++ {
		result, e := allowed.DiscoverSnapshot(ctx)
		if e == nil && result.Scan.Count == 2 {
			success = true
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
	if !success {
		t.Fatal("list-only identity could not discover its namespace")
	}
	disallowed, err := NewWithPatcherAndConfig(NewK8sPatcherFromClient(reader), Config{ClusterID: "isolated-discovery-test", Location: "local", Discovery: DiscoveryConfig{Namespaces: []string{alpha, beta}, LabelSelector: "discovery-test=yes", PageSize: 1}})
	if err != nil {
		t.Fatal(err)
	}
	partial, err := disallowed.DiscoverSnapshot(ctx)
	requireScanCode(t, err, "forbidden")
	if partial.Scan.Complete || partial.Observations != nil || partial.Scan.Count != 2 {
		t.Fatal("partially authorized scan was presented as complete")
	}
	for _, target := range []struct{ namespace, resource, verb string }{{alpha, "deployments", "patch"}, {alpha, "secrets", "list"}, {beta, "deployments", "list"}} {
		group := "apps"
		if target.resource == "secrets" {
			group = ""
		}
		review, err := reader.AuthorizationV1().SelfSubjectAccessReviews().Create(ctx, &authorizationv1.SelfSubjectAccessReview{Spec: authorizationv1.SelfSubjectAccessReviewSpec{ResourceAttributes: &authorizationv1.ResourceAttributes{Namespace: target.namespace, Group: group, Resource: target.resource, Verb: target.verb}}}, metav1.CreateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if review.Status.Allowed {
			t.Fatalf("read-only discovery identity has excess permission: %s/%s", target.resource, target.verb)
		}
	}
	empty, err := NewWithPatcherAndConfig(NewK8sPatcherFromClient(reader), Config{ClusterID: "isolated-discovery-test", Location: "local", Discovery: DiscoveryConfig{Namespaces: []string{alpha}, LabelSelector: "discovery-test=absent"}})
	if err != nil {
		t.Fatal(err)
	}
	none, err := empty.DiscoverSnapshot(ctx)
	if err != nil || !none.Scan.Complete || none.Scan.Count != 0 {
		t.Fatal("empty success not distinguished from denied access", err)
	}
	// Only this test-created binding in its newly created random namespace is
	// changed, under the loopback/Kind/explicit-opt-in guards at the top of this test.
	if allowed.Health(ctx).Status != "healthy" {
		t.Fatal("list-only identity failed scoped health")
	}
	bindingUID := fixtureBinding.UID
	if err = admin.RbacV1().RoleBindings(alpha).Delete(ctx, fixtureBinding.Name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &bindingUID}}); err != nil {
		t.Fatal(err)
	}
	revoked := false
	for i := 0; i < 50; i++ {
		allowed.Health(ctx)
		d := allowed.DiscoveryDiagnostics()
		if d.Access != nil && d.Access.Code == "forbidden" {
			revoked = true
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
	if !revoked {
		t.Fatal("isolated fixture access loss not detected")
	}
	if _, err = admin.RbacV1().RoleBindings(alpha).Create(ctx, &rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: fixtureBinding.Name}, RoleRef: fixtureBinding.RoleRef, Subjects: fixtureBinding.Subjects}, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	restored := false
	for i := 0; i < 50; i++ {
		allowed.Health(ctx)
		d := allowed.DiscoveryDiagnostics()
		if d.Access != nil && d.Access.Status == "authorized" {
			restored = true
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
	if !restored {
		t.Fatal("isolated fixture access restoration not detected")
	}
	if allowed.Health(ctx).Status != "degraded" {
		t.Fatal("restored permissions bypassed successful recovery scan")
	}
	if _, err = allowed.DiscoverSnapshot(ctx); err != nil {
		t.Fatal(err)
	}
	if allowed.Health(ctx).Status != "healthy" {
		t.Fatal("recovered scan did not restore health")
	}
	t.Log("isolated Kubernetes: namespace and label scoping, pagination, UID capture, empty success, forbidden partial scan and list-only RBAC passed")
}
