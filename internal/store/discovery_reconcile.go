package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/consize-oss/consize/pkg/plugin"
	"github.com/consize-oss/consize/pkg/resource"
	"k8s.io/apimachinery/pkg/labels"
)

// planDiscovery uses the existing registry preparation and reconciliation rules.
// The caller commits these changes and the run/audit result in one transaction.
func planDiscovery(rows map[string]resource.Resource, run DiscoveryRun, c DiscoveryCompletion, now time.Time) (map[string]resource.Resource, []string, resource.ReconciliationResult, error) {
	empty := resource.ReconciliationResult{Stale: []string{}, Deleted: []string{}}
	if c.Scan == nil {
		return nil, nil, empty, errors.New("authoritative scan required")
	}
	scan := c.Scan
	digest, key := DiscoveryScopeIdentity(run.PluginID, scan.Scope)
	if scan.Version != plugin.DiscoveryScanVersion || scan.PluginID != run.PluginID || !scan.Complete || scan.ErrorCode != "" || scan.ScopeDigest != digest || key != run.ScopeKey || scan.ID == "" || scan.StartedAt.Before(run.History[len(run.History)-1].StartedAt) || scan.FinishedAt.Before(scan.StartedAt) || scan.FinishedAt.After(now.Add(time.Second)) || scan.Count != len(c.Resources) || len(c.Resources) > 100000 {
		return nil, nil, empty, errors.New("invalid authoritative scan")
	}
	selector, err := labels.Parse(run.Scope["label_selector"])
	if err != nil {
		return nil, nil, empty, errors.New("invalid discovery selector")
	}
	namespaces := map[string]bool{}
	for _, ns := range strings.Split(run.Scope["namespaces"], ",") {
		namespaces[ns] = true
	}
	scope := resource.Scope{Provider: resource.ProviderKubernetes, Account: run.Scope["cluster_id"], Region: run.Scope["location"], Type: resource.TypeKubernetesDeployment, SourcePluginID: run.PluginID}
	next := clone(rows)
	identities := map[string]string{}
	for id, r := range next {
		identities[resourceIdentityKey(r)] = id
	}
	observed := map[string]bool{}
	ids := []string{}
	for _, r := range c.Resources {
		if !scopeMatches(r, scope) || r.ObservedAt.Before(scan.StartedAt) || r.ObservedAt.After(scan.FinishedAt) || !selector.Matches(labels.Set(r.Labels)) {
			return nil, nil, empty, errors.New("observation outside scan authority")
		}
		ns, _ := r.Metadata["namespace"].(string)
		if ns == "" || (!namespaces["*"] && !namespaces[ns]) || !strings.HasPrefix(r.ProviderResourceID, ns+"/") {
			return nil, nil, empty, errors.New("observation outside namespace")
		}
		var existing *resource.Resource
		if prior, ok := next[r.ID]; ok {
			existing = &prior
		}
		if existing == nil {
			if id, ok := identities[resourceIdentityKey(r)]; ok {
				prior := next[id]
				existing = &prior
			}
		}
		r = clone(r)
		if r.Metadata == nil {
			r.Metadata = map[string]any{}
		}
		r.Metadata["discovery_authority"] = run.ScopeKey
		delete(r.Metadata, "discovery_absence")
		prepared, e := prepareResource(r, existing, now, false)
		if e != nil {
			return nil, nil, empty, e
		}
		if observed[prepared.ID] {
			return nil, nil, empty, errors.New("duplicate discovery observation")
		}
		observed[prepared.ID] = true
		next[prepared.ID] = prepared
		identities[resourceIdentityKey(prepared)] = prepared.ID
		ids = append(ids, prepared.ID)
	}
	staleAfter, _ := time.ParseDuration(run.StaleAfter)
	tombstoneAfter, _ := time.ParseDuration(run.TombstoneAfter)
	candidates := []resource.Resource{}
	deleted := []string{}
	for id, r := range next {
		authority, _ := r.Metadata["discovery_authority"].(string)
		if observed[id] || authority != run.ScopeKey || !scopeMatches(r, scope) || r.LifecycleState == resource.LifecycleDeleted || r.LastSeenAt.After(scan.StartedAt) || r.LifecycleObservedAt.After(scan.StartedAt) {
			continue
		}
		since := scan.FinishedAt
		confirmations := 1
		if previous, ok := r.Metadata["discovery_absence"].(map[string]any); ok {
			if raw, ok := previous["since"].(string); ok {
				if parsed, e := time.Parse(time.RFC3339Nano, raw); e == nil && !parsed.After(scan.StartedAt) {
					since = parsed
				}
			}
			var count int64
			switch value := previous["confirmations"].(type) {
			case json.Number:
				count, _ = value.Int64()
			case float64:
				if value == float64(int64(value)) {
					count = int64(value)
				}
			case int:
				count = int64(value)
			}
			if count > 0 && count < 1000000 {
				confirmations = int(count) + 1
			}
		}
		r = clone(r)
		r.Metadata["discovery_absence"] = map[string]any{"since": since.UTC().Format(time.RFC3339Nano), "confirmations": confirmations}
		r.UpdatedAt = now
		next[id] = r
		elapsed := scan.FinishedAt.Sub(since)
		if elapsed >= tombstoneAfter && confirmations >= 2 {
			candidates = append(candidates, r)
			deleted = append(deleted, id)
		} else if elapsed >= staleAfter {
			candidates = append(candidates, r)
		}
	}
	changes, result, err := planReconciliation(candidates, resource.Reconciliation{Scope: scope, ObservedAt: scan.FinishedAt, ConfirmedDeletedIDs: deleted}, now)
	if err != nil {
		return nil, nil, empty, fmt.Errorf("discovery lifecycle conflict: %w", err)
	}
	for _, r := range changes {
		next[r.ID] = r
	}
	sort.Strings(ids)
	return next, ids, result, nil
}
