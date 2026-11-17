package kubernetes

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/consize-oss/consize/pkg/plugin"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

type deploymentPageLister interface {
	ListDeploymentPage(context.Context, string, metav1.ListOptions) (*appsv1.DeploymentList, error)
}
type DiscoveryDiagnostics struct {
	Access             *AccessCheck          `json:"access,omitempty"`
	InProgress         bool                  `json:"in_progress"`
	LastAttempt        *plugin.DiscoveryScan `json:"last_attempt"`
	LastSuccessfulScan *plugin.DiscoveryScan `json:"last_successful_scan"`
}
type scanState struct {
	mu          sync.Mutex
	diagnostics DiscoveryDiagnostics
	running     bool
}

func cloneScan(scan *plugin.DiscoveryScan) *plugin.DiscoveryScan {
	if scan == nil {
		return nil
	}
	copy := *scan
	copy.Scope = cloneLabels(scan.Scope)
	copy.SnapshotVersions = cloneLabels(scan.SnapshotVersions)
	return &copy
}
func (p *Plugin) DiscoveryDiagnostics() DiscoveryDiagnostics {
	p.scans.mu.Lock()
	defer p.scans.mu.Unlock()
	var access *AccessCheck
	if p.scans.diagnostics.Access != nil {
		copy := *p.scans.diagnostics.Access
		if copy.APIReachable != nil {
			v := *copy.APIReachable
			copy.APIReachable = &v
		}
		access = &copy
	}
	return DiscoveryDiagnostics{Access: access, InProgress: p.scans.running, LastAttempt: cloneScan(p.scans.diagnostics.LastAttempt), LastSuccessfulScan: cloneScan(p.scans.diagnostics.LastSuccessfulScan)}
}
func (p *Plugin) DiscoverSnapshot(ctx context.Context) (batch plugin.DiscoverySnapshot, err error) {
	p.scans.mu.Lock()
	if p.scans.running {
		p.scans.mu.Unlock()
		return batch, scanFailure("scan_busy")
	}
	p.scans.running = true
	p.scans.mu.Unlock()
	started := time.Now()
	scan := plugin.DiscoveryScan{Version: plugin.DiscoveryScanVersion, PluginID: ID, StartedAt: started.UTC(), SnapshotVersions: map[string]string{}, Scope: p.DiscoveryScope()}
	encoded, _ := json.Marshal(scan.Scope)
	digest := sha256.Sum256(encoded)
	scan.ScopeDigest = hex.EncodeToString(digest[:])
	random := make([]byte, 16)
	if _, readErr := rand.Read(random); readErr == nil {
		scan.ID = "scan:" + hex.EncodeToString(random)
	} else {
		err = scanFailure("unavailable")
	}
	p.scans.mu.Lock()
	p.scans.diagnostics.LastAttempt = cloneScan(&scan)
	p.scans.mu.Unlock()
	defer func() {
		scan.FinishedAt = time.Now().UTC()
		scan.DurationMillis = time.Since(started).Milliseconds()
		if err != nil {
			classified := safeScanError(err)
			err = classified
			scan.ErrorCode = classified.Code
			scan.Complete = false
			batch.Observations = nil
		}
		batch.Scan = scan
		p.scans.mu.Lock()
		defer p.scans.mu.Unlock()
		p.scans.running = false
		p.scans.diagnostics.LastAttempt = cloneScan(&scan)
		if scan.Complete {
			p.scans.diagnostics.LastSuccessfulScan = cloneScan(&scan)
			check := accessCheck(nil, scan.Scope["namespaces"], scan.FinishedAt)
			p.scans.diagnostics.Access = &check
		} else if err != nil {
			check := accessCheck(err, "", scan.FinishedAt)
			p.scans.diagnostics.Access = &check
		}
	}()
	if err != nil {
		return batch, err
	}
	lister, ok := p.patcher.(deploymentPageLister)
	if !ok {
		return batch, scanFailure("unsupported_client")
	}
	ctx, cancel := context.WithTimeout(ctx, p.discovery.timeout)
	defer cancel()
	items, err := collectDeployments(ctx, lister, p.discovery, &scan)
	if err != nil {
		return batch, err
	}
	batch.Observations = make([]plugin.ProviderObservation, 0, len(items))
	observedAt := time.Now().UTC()
	for _, deployment := range items {
		if err := ctx.Err(); err != nil {
			return batch, err
		}
		res, err := MapDeployment(deployment, MappingConfig{ClusterID: p.clusterID, Location: p.location, AnnotationAllowlist: p.discovery.annotations}, observedAt)
		if err != nil {
			return batch, err
		}
		res.Metadata["discovery_scan"] = map[string]any{"id": scan.ID, "scope_digest": scan.ScopeDigest}
		batch.Observations = append(batch.Observations, plugin.ProviderObservation{PluginID: ID, Provider: res.Provider, ObservedAt: observedAt, Resource: res})
	}
	if err := ctx.Err(); err != nil {
		return batch, err
	}
	scan.Count = len(batch.Observations)
	scan.Complete = true
	return batch, nil
}
func collectDeployments(ctx context.Context, lister deploymentPageLister, o discoveryOptions, scan *plugin.DiscoveryScan) ([]appsv1.Deployment, error) {
	namespaces := o.namespaces
	if len(namespaces) == 0 {
		namespaces = []string{metav1.NamespaceAll}
	}
	out := []appsv1.Deployment{}
	var observedBytes int64
	identities := map[string]bool{}
	uids := map[string]bool{}
	for _, namespace := range namespaces {
		continuation, version := "", ""
		seenContinuations := map[string]bool{}
		for {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if scan.Pages >= 1000 {
				return nil, scanFailure("limit_exceeded")
			}
			page, err := lister.ListDeploymentPage(ctx, namespace, metav1.ListOptions{Limit: o.pageSize, Continue: continuation, LabelSelector: o.selector.String()})
			if err != nil {
				return nil, scopedScanError(err, namespace)
			}
			scan.Pages++
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if page == nil || page.ResourceVersion == "" || len(page.ResourceVersion) > 1024 || len(page.Items) > int(o.pageSize) || len(page.Continue) > 4096 {
				return nil, scanFailure("invalid_page")
			}
			if continuation == "" {
				version = page.ResourceVersion
			} else if page.ResourceVersion != version {
				return nil, scanFailure("invalid_page")
			}
			key := namespace
			if key == "" {
				key = "*"
			}
			scan.SnapshotVersions[key] = version
			for _, item := range page.Items {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				encoded, err := json.Marshal(item)
				if err != nil {
					return nil, scanFailure("invalid_observation")
				}
				observedBytes += int64(len(encoded))
				if observedBytes > o.maxBytes {
					return nil, scanFailure("limit_exceeded")
				}
				identity := item.Namespace + "/" + item.Name
				if item.Namespace == "" || item.Name == "" || item.UID == "" || namespace != "" && item.Namespace != namespace || !o.selector.Matches(labels.Set(item.Labels)) || identities[identity] || uids[string(item.UID)] {
					return nil, scanFailure("invalid_observation")
				}
				identities[identity] = true
				uids[string(item.UID)] = true
				if len(out) >= o.maxObjects {
					return nil, scanFailure("limit_exceeded")
				}
				out = append(out, *item.DeepCopy())
				scan.Count = len(out)
			}
			if page.Continue == "" {
				break
			}
			if seenContinuations[page.Continue] {
				return nil, scanFailure("invalid_page")
			}
			seenContinuations[page.Continue] = true
			continuation = page.Continue
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Namespace != b.Namespace {
			return a.Namespace < b.Namespace
		}
		return a.Name < b.Name
	})
	return out, nil
}

// DiscoveryScope returns an independent descriptor without contacting Kubernetes.
func (p *Plugin) DiscoveryScope() map[string]string {
	namespaces := strings.Join(p.discovery.namespaces, ",")
	if namespaces == "" {
		namespaces = "*"
	}
	return map[string]string{"cluster_id": p.clusterID, "location": p.location, "namespaces": namespaces, "label_selector": p.discovery.selector.String()}
}
