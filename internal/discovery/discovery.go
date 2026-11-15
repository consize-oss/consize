package discovery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/consize-oss/consize/internal/store"
	"github.com/consize-oss/consize/pkg/plugin"
	"github.com/consize-oss/consize/pkg/resource"
)

type Result struct {
	Scan      *plugin.DiscoveryScan `json:"scan,omitempty"`
	PluginID  string                `json:"plugin_id"`
	Count     int                   `json:"count"`
	Resources []resource.Resource   `json:"resources"`
}

type Service struct {
	store   store.Store
	plugins *plugin.Manager
	now     func() time.Time
}

func New(st store.Store, plugins *plugin.Manager) *Service {
	return &Service{store: st, plugins: plugins, now: time.Now}
}

func (s *Service) Run(ctx context.Context) ([]Result, error) {
	if s.store == nil || s.plugins == nil {
		return nil, errors.New("discovery requires registry and plugin manager")
	}
	plugins := s.plugins.DiscoveryPlugins()
	if len(plugins) == 0 {
		return nil, errors.New("no discovery plugins are configured")
	}
	results := make([]Result, 0, len(plugins))
	for _, provider := range plugins {
		var observations []plugin.ProviderObservation
		var scan *plugin.DiscoveryScan
		if snapshotProvider, ok := provider.(plugin.SnapshotDiscoveryPlugin); ok {
			snapshot, err := snapshotProvider.DiscoverSnapshot(ctx)
			if err != nil {
				return nil, fmt.Errorf("discover with %s: %w", provider.ID(), err)
			}
			if err := validateScan(snapshot, provider.ID(), s.now().UTC()); err != nil {
				return nil, err
			}
			observations = snapshot.Observations
			scan = &snapshot.Scan
		} else {
			var err error
			observations, err = provider.Discover(ctx)
			if err != nil {
				return nil, fmt.Errorf("discover with %s: %w", provider.ID(), err)
			}
		}
		result := Result{Scan: scan, PluginID: provider.ID(), Resources: make([]resource.Resource, 0, len(observations))}
		normalized := make([]resource.Resource, 0, len(observations))
		for _, observation := range observations {
			res, err := normalize(provider, observation, s.now().UTC())
			if err != nil {
				return nil, err
			}
			normalized = append(normalized, res)
		}
		for _, res := range normalized {
			persisted, err := s.store.UpsertResource(ctx, res)
			if err != nil {
				return nil, fmt.Errorf("register discovered resource %s: %w", res.ID, err)
			}
			result.Resources = append(result.Resources, persisted)
		}
		result.Count = len(result.Resources)
		results = append(results, result)
	}
	return results, nil
}

func normalize(provider plugin.DiscoveryPlugin, observation plugin.ProviderObservation, now time.Time) (resource.Resource, error) {
	manifest := provider.Manifest()
	res := observation.Resource
	if observation.PluginID != provider.ID() || observation.Provider == "" || res.Provider != observation.Provider {
		return resource.Resource{}, fmt.Errorf("discovery plugin %s returned mismatched provider identity", provider.ID())
	}
	if res.Type == "" || res.ProviderResourceID == "" {
		return resource.Resource{}, fmt.Errorf("discovery plugin %s returned incomplete resource identity", provider.ID())
	}
	if !plugin.Supports(manifest.SupportedResourceTypes, res.Type) {
		return resource.Resource{}, fmt.Errorf("discovery plugin %s returned unsupported resource type %s", provider.ID(), res.Type)
	}
	if observation.ObservedAt.IsZero() || observation.ObservedAt.After(now.Add(time.Minute)) {
		return resource.Resource{}, fmt.Errorf("discovery plugin %s returned invalid observation time", provider.ID())
	}
	res.SourcePluginID = provider.ID()
	res.ObservedAt = observation.ObservedAt.UTC()
	res, err := res.Normalize(now)
	if err != nil {
		return resource.Resource{}, fmt.Errorf("discovery plugin %s returned invalid resource: %w", provider.ID(), err)
	}
	return res, nil
}

// Completeness is explicit and bound to the provider's declared scope. This does
// not reconcile absence or make registry writes atomic; those remain core-owned.
func validateScan(snapshot plugin.DiscoverySnapshot, providerID string, now time.Time) error {
	scan := snapshot.Scan
	scope, err := json.Marshal(scan.Scope)
	digest := sha256.Sum256(scope)
	if err != nil || scan.Version != plugin.DiscoveryScanVersion || scan.PluginID != providerID || scan.ID == "" || len(scan.ID) > 128 || !scan.Complete || scan.ErrorCode != "" || scan.Count != len(snapshot.Observations) || scan.Count < 0 || scan.Count > 100000 || scan.Pages < 0 || len(scan.Scope) == 0 || len(scan.Scope) > 64 || scan.ScopeDigest != hex.EncodeToString(digest[:]) || scan.StartedAt.IsZero() || scan.FinishedAt.Before(scan.StartedAt) || scan.FinishedAt.After(now.Add(time.Minute)) || scan.DurationMillis < 0 {
		return errors.New("discovery snapshot is incomplete or invalid")
	}
	for key, value := range scan.Scope {
		if len(key) > 256 || len(value) > 16384 {
			return errors.New("discovery snapshot scope exceeds limits")
		}
	}
	return nil
}
