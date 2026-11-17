package kubernetes

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	k8sresource "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

type K8sPatcher struct {
	discoveryClient kubernetes.Interface
	client          kubernetes.Interface
	dynamic         dynamic.Interface
}

func NewK8sPatcher(kubeconfig string) (*K8sPatcher, error) {
	return NewK8sPatcherForContext(kubeconfig, "")
}
func loadK8sConfig(kubeconfig, contextName string) (*rest.Config, error) {
	var cfg *rest.Config
	var err error
	if kubeconfig != "" {
		rules := &clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfig}
		overrides := &clientcmd.ConfigOverrides{CurrentContext: contextName}
		cfg, err = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides).ClientConfig()
	} else {
		if contextName != "" {
			return nil, scanFailure("configuration")
		}
		cfg, err = rest.InClusterConfig()
	}
	if err != nil {
		return nil, scanFailure("configuration")
	}
	if err = validateDiscoveryRESTConfig(cfg); err != nil {
		return nil, err
	}
	cfg.Timeout = 15 * time.Second
	return cfg, nil
}
func newDiscoveryClient(cfg *rest.Config) (kubernetes.Interface, error) {
	cfg = rest.CopyConfig(cfg)
	cfg.WarningHandler = rest.NoWarnings{}
	cfg.WarningHandlerWithContext = rest.NoWarnings{}
	previousWrap := cfg.WrapTransport
	cfg.WrapTransport = func(next http.RoundTripper) http.RoundTripper {
		if previousWrap != nil {
			next = previousWrap(next)
		}
		return deploymentListTransport{next: next}
	}
	httpClient, err := rest.HTTPClientFor(cfg)
	if err != nil {
		return nil, scanFailure("configuration")
	}
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error { return scanFailure("redirect_blocked") }
	client, err := kubernetes.NewForConfigAndClient(cfg, httpClient)
	if err != nil {
		return nil, scanFailure("configuration")
	}
	return client, nil
}
func NewK8sPatcherForContext(kubeconfig, contextName string) (*K8sPatcher, error) {
	return newK8sPatcher(kubeconfig, contextName, nil)
}
func newK8sPatcher(kubeconfig, contextName string, discoveryAuth *DiscoveryAuthConfig) (*K8sPatcher, error) {
	cfg, err := loadK8sConfig(kubeconfig, contextName)
	if err != nil {
		return nil, err
	}
	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, scanFailure("configuration")
	}
	dynamicClient, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, scanFailure("configuration")
	}
	discoveryConfig := cfg
	if discoveryAuth != nil {
		discoveryConfig, err = loadK8sConfig(discoveryAuth.Kubeconfig, discoveryAuth.Context)
		if err != nil {
			return nil, err
		}
		if discoveryConfig.Host != cfg.Host {
			return nil, scanFailure("configuration")
		}
	}
	reader, err := newDiscoveryClient(discoveryConfig)
	if err != nil {
		return nil, err
	}
	return &K8sPatcher{client: clientset, discoveryClient: reader, dynamic: dynamicClient}, nil
}

func NewK8sPatcherFromClients(client kubernetes.Interface, dynamicClient dynamic.Interface) *K8sPatcher {
	return &K8sPatcher{client: client, dynamic: dynamicClient}
}

func NewK8sPatcherFromClient(client kubernetes.Interface) *K8sPatcher {
	return &K8sPatcher{client: client}
}

func (k *K8sPatcher) Health(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if client := k.client.Discovery().RESTClient(); client != nil {
		return client.Get().AbsPath("/version").Do(ctx).Error()
	}
	_, err := k.client.Discovery().ServerVersion()
	return err
}

func (k *K8sPatcher) ReadDeploymentResources(ctx context.Context, namespace, name, kind string) (req, lim int64, err error) {
	dep, err := k.client.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return 0, 0, fmt.Errorf("get deployment: %w", err)
	}
	for _, c := range dep.Spec.Template.Spec.Containers {
		r, l := fieldValues(kind, c.Resources.Requests, c.Resources.Limits)
		req += r
		lim += l
	}
	return req, lim, nil
}

func (k *K8sPatcher) PatchDeployment(ctx context.Context, namespace, name string, diff PatchDiff) error {
	for attempt := 0; attempt < 3; attempt++ {
		dep, err := k.client.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("get deployment: %w", err)
		}
		if !mutateResources(dep.Spec.Template.Spec.Containers, diff) {
			return fmt.Errorf("no container resources to patch in %s/%s", namespace, name)
		}
		_, err = k.client.AppsV1().Deployments(namespace).Update(ctx, dep, metav1.UpdateOptions{})
		if err == nil {
			return nil
		}
		if apierrors.IsConflict(err) {
			continue
		}
		return fmt.Errorf("update deployment: %w", err)
	}
	return fmt.Errorf("patch %s/%s: resourceVersion conflicted 3 times", namespace, name)
}

type containerShare struct {
	idx    int
	hasReq bool
	hasLim bool
	curReq int64
	share  float64
}

func mutateResources(containers []corev1.Container, diff PatchDiff) bool {
	var targets []containerShare
	var totalReq int64
	for i, c := range containers {
		req, lim := fieldValues(diff.Resource, c.Resources.Requests, c.Resources.Limits)
		if req == 0 && lim == 0 {
			continue
		}
		targets = append(targets, containerShare{idx: i, hasReq: req != 0, hasLim: lim != 0, curReq: req})
		totalReq += req
	}
	if len(targets) == 0 {
		return false
	}
	for i := range targets {
		if totalReq > 0 {
			targets[i].share = float64(targets[i].curReq) / float64(totalReq)
		} else {
			targets[i].share = 1.0 / float64(len(targets))
		}
	}
	stepReq := diff.ProposedReq - diff.CurrentReq
	stepLim := diff.ProposedLimit - diff.CurrentLimit
	reqAllocs := distribute(stepReq, targets)
	limAllocs := distribute(stepLim, targets)
	for i, t := range targets {
		c := &containers[t.idx]
		if t.hasReq {
			setField(c, diff.Resource, true, t.curReq+reqAllocs[i])
		}
		if t.hasLim && diff.ProposedLimit != diff.CurrentLimit {
			setField(c, diff.Resource, false, limValue(diff.Resource, c.Resources.Limits)+limAllocs[i])
		}
	}
	return true
}

func distribute(step int64, targets []containerShare) []int64 {
	out := make([]int64, len(targets))
	var sum int64
	for i, t := range targets {
		if i == len(targets)-1 {
			out[i] = step - sum
			break
		}
		v := int64(math.Round(float64(step) * t.share))
		out[i] = v
		sum += v
	}
	return out
}

func fieldValues(kind string, r, l corev1.ResourceList) (req, lim int64) {
	if kind == "cpu" {
		return r.Cpu().MilliValue(), l.Cpu().MilliValue()
	}
	return r.Memory().Value(), l.Memory().Value()
}

func limValue(kind string, l corev1.ResourceList) int64 {
	if kind == "cpu" {
		return l.Cpu().MilliValue()
	}
	return l.Memory().Value()
}

func setField(c *corev1.Container, kind string, request bool, value int64) {
	var q k8sresource.Quantity
	if kind == "cpu" {
		q = *k8sresource.NewMilliQuantity(value, k8sresource.DecimalSI)
	} else {
		q = *k8sresource.NewQuantity(value, k8sresource.BinarySI)
	}
	name := corev1.ResourceName(kind)
	if request {
		if c.Resources.Requests == nil {
			c.Resources.Requests = corev1.ResourceList{}
		}
		c.Resources.Requests[name] = q
		return
	}
	if c.Resources.Limits == nil {
		c.Resources.Limits = corev1.ResourceList{}
	}
	c.Resources.Limits[name] = q
}
