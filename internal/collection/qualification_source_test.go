package collection_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	quantity "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const evidenceWorkloadImage = "busybox:1.37.0@sha256:a7dbc319b1438115a207e11930dc4bb3e643dcb3ec0bdd694bb36cd8fad289ec"

type liveEvidenceSource struct {
	cleaned                                          bool
	client                                           kubernetes.Interface
	config                                           *rest.Config
	cluster, contextName, kubeconfig, selector, node string
	namespaces                                       []string
	owned                                            map[string]types.UID
	server                                           *httptest.Server
}

func liveSource(t *testing.T, ctx context.Context, suffix string) *liveEvidenceSource {
	t.Helper()
	file, name := os.Getenv("CONSIZE_DISCOVERY_TEST_KUBECONFIG"), os.Getenv("CONSIZE_DISCOVERY_TEST_CONTEXT")
	if file == "" {
		t.Fatal("qualification requires its explicit disposable cluster")
	}
	if os.Getenv("CONSIZE_DISCOVERY_TEST_FIXTURES") != "1" || !filepath.IsAbs(file) || !strings.HasPrefix(name, "kind-consize-discovery-") {
		t.Fatal("refusing a non-fixture evidence target")
	}
	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(&clientcmd.ClientConfigLoadingRules{ExplicitPath: file}, &clientcmd.ConfigOverrides{CurrentContext: name}).ClientConfig()
	if err != nil {
		t.Fatal("load explicit evidence context")
	}
	endpoint, err := url.Parse(cfg.Host)
	if err != nil || endpoint.Scheme != "https" || net.ParseIP(endpoint.Hostname()) == nil || !net.ParseIP(endpoint.Hostname()).IsLoopback() || cfg.Insecure {
		t.Fatal("evidence qualification requires a verified loopback API")
	}
	cfg.Timeout = 8 * time.Second
	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		t.Fatal("create fixture client")
	}
	s := &liveEvidenceSource{client: client, config: cfg, cluster: strings.TrimPrefix(name, "kind-"), contextName: name, kubeconfig: file, selector: "consize.io/evidence-qualification=" + suffix, owned: map[string]types.UID{}, namespaces: []string{"consize-evidence-a-" + suffix, "consize-evidence-b-" + suffix}}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		clean := true
		for ns, uid := range s.owned {
			err := s.client.CoreV1().Namespaces().Delete(cleanup, ns, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}})
			if err != nil && !apierrors.IsNotFound(err) {
				t.Error("delete owned evidence namespace")
				clean = false
				continue
			}
			for {
				_, err = s.client.CoreV1().Namespaces().Get(cleanup, ns, metav1.GetOptions{})
				if apierrors.IsNotFound(err) {
					break
				}
				select {
				case <-cleanup.Done():
					t.Error("evidence namespace cleanup not confirmed")
					return
				case <-time.After(100 * time.Millisecond):
				}
			}
		}
		s.cleaned = clean
	})
	for _, ns := range s.namespaces {
		created, err := client.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns, Labels: map[string]string{"consize.io/evidence-qualification": suffix}}}, metav1.CreateOptions{})
		if err != nil {
			t.Fatal("create evidence fixture namespace")
		}
		s.owned[ns] = created.UID
		replicas := int32(2)
		termination := int64(2)
		runAs := int64(1000)
		no := false
		yes := true
		containers := []corev1.Container{}
		for _, container := range []string{"app", "sidecar"} {
			script := `while :; do dd if=/dev/zero of=/dev/shm/evidence bs=1024 count=8192 2>/dev/null; head -c 1048576 /dev/zero | sha256sum >/dev/null; sleep 1; dd if=/dev/zero of=/dev/shm/evidence bs=1024 count=4096 2>/dev/null; sleep 1; done`
			containers = append(containers, corev1.Container{Name: container, Image: evidenceWorkloadImage, ImagePullPolicy: corev1.PullIfNotPresent, Command: []string{"/bin/sh", "-c", script}, Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: quantity.MustParse("20m"), corev1.ResourceMemory: quantity.MustParse("16Mi")}, Limits: corev1.ResourceList{corev1.ResourceCPU: quantity.MustParse("100m"), corev1.ResourceMemory: quantity.MustParse("48Mi")}}, SecurityContext: &corev1.SecurityContext{RunAsUser: &runAs, RunAsNonRoot: &yes, AllowPrivilegeEscalation: &no, ReadOnlyRootFilesystem: &yes, Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}}, SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}}})
		}
		labels := map[string]string{"consize.io/evidence-qualification": suffix, "consize.io/owner": "qualification", "consize.io/environment": "development"}
		_, err = client.AppsV1().Deployments(ns).Create(ctx, &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "workload", Labels: labels}, Spec: appsv1.DeploymentSpec{Replicas: &replicas, Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"consize.io/evidence-qualification": suffix}}, Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: labels}, Spec: corev1.PodSpec{TerminationGracePeriodSeconds: &termination, AutomountServiceAccountToken: &no, Containers: containers, Tolerations: []corev1.Toleration{{Key: "node-role.kubernetes.io/control-plane", Operator: corev1.TolerationOpExists, Effect: corev1.TaintEffectNoSchedule}}}}}}, metav1.CreateOptions{})
		if err != nil {
			t.Fatal("create evidence workloads")
		}
	}
	waitLive(t, ctx, "running multi-container workloads", func() bool {
		for _, ns := range s.namespaces {
			pods, err := client.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{LabelSelector: s.selector})
			if err != nil || len(pods.Items) != 2 {
				return false
			}
			for _, p := range pods.Items {
				if p.Status.Phase != corev1.PodRunning || len(p.Status.ContainerStatuses) != 2 {
					return false
				}
				for _, c := range p.Status.ContainerStatuses {
					if !c.Ready {
						return false
					}
				}
				if s.node != "" && s.node != p.Spec.NodeName {
					t.Fatal("fixture requires one disposable node")
				}
				s.node = p.Spec.NodeName
			}
		}
		return true
	})
	s.server = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.server.Close)
	return s
}

func waitLive(t *testing.T, ctx context.Context, label string, ready func() bool) {
	t.Helper()
	for {
		if ready() {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("qualification timed out: %s", label)
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// The proxy forwards real kubelet samples unchanged. The separate inventory
// exporter reads actual Pod/ReplicaSet/Deployment UIDs and resource/status fields.
// It never generates CPU/memory values or fills absent counters with zero.
func (s *liveEvidenceSource) serve(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		w.WriteHeader(405)
		return
	}
	switch r.URL.Path {
	case "/cadvisor":
		raw, err := s.client.CoreV1().RESTClient().Get().Resource("nodes").Name(s.node).SubResource("proxy").Suffix("metrics/cadvisor").DoRaw(r.Context())
		if err != nil || len(raw) > 16<<20 {
			http.Error(w, "fixture kubelet metrics unavailable", 503)
			return
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		_, _ = w.Write(raw)
	case "/inventory":
		var body bytes.Buffer
		if err := s.inventory(r.Context(), &body); err != nil {
			http.Error(w, "fixture ownership inventory unavailable", 503)
			return
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		_, _ = w.Write(body.Bytes())
	default:
		w.WriteHeader(404)
	}
}
func ownerUID(refs []metav1.OwnerReference, kind string) types.UID {
	for _, ref := range refs {
		if ref.Controller != nil && *ref.Controller && ref.Kind == kind {
			return ref.UID
		}
	}
	return ""
}
func metricLabels(labels map[string]string) string {
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+strconv.Quote(labels[key]))
	}
	return strings.Join(parts, ",")
}
func (s *liveEvidenceSource) inventory(ctx context.Context, w io.Writer) error {
	declared := map[string]bool{}
	declare := func(name, kind string) {
		if !declared[name] {
			fmt.Fprintf(w, "# TYPE %s %s\n", name, kind)
			declared[name] = true
		}
	}
	for _, ns := range s.namespaces {
		deps, err := s.client.AppsV1().Deployments(ns).List(ctx, metav1.ListOptions{LabelSelector: s.selector})
		if err != nil {
			return err
		}
		depByUID := map[types.UID]string{}
		for _, d := range deps.Items {
			depByUID[d.UID] = d.Name
		}
		sets, err := s.client.AppsV1().ReplicaSets(ns).List(ctx, metav1.ListOptions{LabelSelector: s.selector})
		if err != nil {
			return err
		}
		setOwners := map[types.UID]types.UID{}
		for _, set := range sets.Items {
			uid := ownerUID(set.OwnerReferences, "Deployment")
			if depByUID[uid] != "" {
				setOwners[set.UID] = uid
			}
		}
		pods, err := s.client.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{LabelSelector: s.selector})
		if err != nil {
			return err
		}
		for _, pod := range pods.Items {
			depUID := setOwners[ownerUID(pod.OwnerReferences, "ReplicaSet")]
			if depUID == "" || pod.UID == "" {
				continue
			}
			base := map[string]string{"cluster": s.cluster, "namespace": ns, "pod": pod.Name, "uid": string(pod.UID), "deployment": depByUID[depUID], "deployment_uid": string(depUID)}
			status := map[string]corev1.ContainerStatus{}
			for _, c := range pod.Status.ContainerStatuses {
				status[c.Name] = c
			}
			for _, container := range pod.Spec.Containers {
				labels := map[string]string{}
				for key, value := range base {
					labels[key] = value
				}
				labels["container"] = container.Name
				emit := func(name string, value float64) {
					kind := "gauge"
					if strings.HasSuffix(name, "_total") {
						kind = "counter"
					}
					declare(name, kind)
					fmt.Fprintf(w, "%s{%s} %.12g\n", name, metricLabels(labels), value)
				}
				emit("kube_pod_container_info", 1)
				if current, ok := status[container.Name]; ok {
					emit("kube_pod_container_status_restarts_total", float64(current.RestartCount))
				}
				for _, kind := range []struct {
					name      string
					resources corev1.ResourceList
				}{{"requests", container.Resources.Requests}, {"limits", container.Resources.Limits}} {
					for _, resource := range []struct {
						name corev1.ResourceName
						unit string
					}{{corev1.ResourceCPU, "core"}, {corev1.ResourceMemory, "byte"}} {
						q, ok := kind.resources[resource.name]
						if !ok {
							continue
						}
						value := float64(q.Value())
						if resource.name == corev1.ResourceCPU {
							value = float64(q.MilliValue()) / 1000
						}
						labels["resource"] = string(resource.name)
						labels["unit"] = resource.unit
						emit("kube_pod_container_resource_"+kind.name, value)
						delete(labels, "resource")
						delete(labels, "unit")
					}
				}
			}
			declare("kube_pod_status_phase", "gauge")
			base["phase"] = string(pod.Status.Phase)
			fmt.Fprintf(w, "kube_pod_status_phase{%s} 1\n", metricLabels(base))
			if pod.Status.Reason != "" {
				declare("kube_pod_status_reason", "gauge")
				delete(base, "phase")
				base["reason"] = pod.Status.Reason
				fmt.Fprintf(w, "kube_pod_status_reason{%s} 1\n", metricLabels(base))
			}
		}
	}
	return nil
}

type qualificationPrometheus struct {
	binary, dir, address, config string
	command                      *exec.Cmd
	done                         chan error
	log                          *os.File
}

func newQualificationPrometheus(t *testing.T, source *liveEvidenceSource) *qualificationPrometheus {
	t.Helper()
	binary := os.Getenv("CONSIZE_PROMETHEUS_BIN")
	if !filepath.IsAbs(binary) {
		t.Fatal("qualification requires the checksum-pinned Prometheus executable")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	_ = listener.Close()
	dir := t.TempDir()
	config := filepath.Join(dir, "prometheus.yml")
	target := strings.TrimPrefix(source.server.URL, "http://")
	body := fmt.Sprintf("global:\n  scrape_interval: 3s\n  scrape_timeout: 2s\nscrape_configs:\n  - job_name: kubelet\n    metrics_path: /cadvisor\n    static_configs:\n      - targets: [%q]\n        labels:\n          cluster: %q\n  - job_name: ownership\n    metrics_path: /inventory\n    static_configs:\n      - targets: [%q]\n", target, source.cluster, target)
	if err := os.WriteFile(config, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	p := &qualificationPrometheus{binary: binary, dir: dir, address: address, config: config}
	t.Cleanup(p.stop)
	return p
}
func (p *qualificationPrometheus) start(ctx context.Context) error {
	if p.command != nil {
		return fmt.Errorf("fixture Prometheus already running")
	}
	log, err := os.OpenFile(filepath.Join(p.dir, "server.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	command := exec.CommandContext(ctx, p.binary, "--config.file="+p.config, "--storage.tsdb.path="+filepath.Join(p.dir, "data"), "--web.listen-address="+p.address, "--storage.tsdb.wal-segment-size=16MB", "--storage.tsdb.retention.size=64MB", "--log.level=error")
	command.Stdout = log
	command.Stderr = log
	command.WaitDelay = 2 * time.Second
	if err := command.Start(); err != nil {
		_ = log.Close()
		return err
	}
	p.command = command
	p.log = log
	p.done = make(chan error, 1)
	go func() { p.done <- command.Wait() }()
	return nil
}
func (p *qualificationPrometheus) stop() {
	if p.command == nil {
		return
	}
	_ = p.command.Process.Signal(os.Interrupt)
	select {
	case <-p.done:
	case <-time.After(3 * time.Second):
		_ = p.command.Process.Kill()
		<-p.done
	}
	_ = p.log.Close()
	p.command = nil
}
