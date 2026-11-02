package recommender

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/consize-oss/consize/internal/bootstrap"
	"github.com/consize-oss/consize/internal/store"
	"github.com/consize-oss/consize/pkg/plugin"
	"github.com/consize-oss/consize/pkg/resource"
)

const BuiltInHeadroomAlgorithmID = "prometheus-headroom-v1"

type Input struct {
	Resource resource.Resource
	Evidence []plugin.MetricsSnapshot
}

type Engine interface {
	ID() string
	Recommend(ctx context.Context, input Input) ([]store.Recommendation, error)
}

type HeadroomRecommender struct {
	cfg bootstrap.RecommenderConfig
}

func New(cfg bootstrap.RecommenderConfig) *HeadroomRecommender {
	return &HeadroomRecommender{cfg: withDefaults(cfg)}
}

func (r *HeadroomRecommender) ID() string { return BuiltInHeadroomAlgorithmID }

func (r *HeadroomRecommender) Recommend(ctx context.Context, input Input) ([]store.Recommendation, error) {
	rec, err := r.RecommendOne(ctx, input.Resource, firstEvidence(input.Evidence))
	if err != nil {
		return nil, err
	}
	return []store.Recommendation{rec}, nil
}

func (r *HeadroomRecommender) RecommendOne(_ context.Context, res resource.Resource, snapshot plugin.MetricsSnapshot) (store.Recommendation, error) {
	if res.Type != resource.TypeKubernetesDeployment {
		return store.Recommendation{}, fmt.Errorf("unsupported resource type %q", res.Type)
	}
	if snapshot.ResourceID != res.ID || snapshot.PluginID != r.cfg.MetricsPluginID {
		return store.Recommendation{}, fmt.Errorf("metrics identity mismatch")
	}
	if _, err := snapshot.Measurement(r.cfg.MetricKey, "bytes", time.Now().UTC()); err != nil {
		return store.Recommendation{}, err
	}
	currentReq, err := numberFromState(res.CurrentState, r.cfg.CurrentRequestKey)
	if err != nil {
		return store.Recommendation{}, err
	}
	currentLimit, err := optionalNumberFromState(res.CurrentState, r.cfg.CurrentLimitKey)
	if err != nil {
		return store.Recommendation{}, err
	}
	observed, err := numberFromSignals(snapshot.Signals, r.cfg.MetricKey)
	if err != nil {
		return store.Recommendation{}, err
	}
	proposedReq := int64(math.Ceil(observed * r.cfg.HeadroomRatio))
	if r.cfg.HeadroomRatio < 1 || r.cfg.MaxReductionRatio <= 0 || r.cfg.MaxReductionRatio > 0.25 || math.IsNaN(observed) || math.IsInf(observed, 0) || observed <= 0 {
		return store.Recommendation{}, fmt.Errorf("invalid evidence or safety configuration: headroom must be >= 1 and max reduction within (0, 0.25]")
	}
	proposedReq = max(proposedReq, int64(math.Ceil(float64(currentReq)*(1-r.cfg.MaxReductionRatio))))
	if proposedReq <= 0 || proposedReq >= currentReq {
		return store.Recommendation{}, fmt.Errorf("no safe reduction: observed=%f current_request=%d proposed_request=%d", observed, currentReq, proposedReq)
	}
	reductionRatio := float64(currentReq-proposedReq) / float64(currentReq)
	if reductionRatio < r.cfg.MinReductionRatio {
		return store.Recommendation{}, fmt.Errorf("reduction %.2f is below minimum %.2f", reductionRatio, r.cfg.MinReductionRatio)
	}
	proposedLimit := currentLimit
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return store.Recommendation{}, err
	}
	var frozen plugin.MetricsSnapshot
	if err = json.Unmarshal(encoded, &frozen); err != nil {
		return store.Recommendation{}, err
	}
	snapshot = frozen
	confidence := AssessConfidence(r.cfg, res, snapshot, time.Now().UTC())
	return store.Recommendation{
		ResourceID:       res.ID,
		PluginID:         r.cfg.ActionPluginID,
		AlgorithmID:      r.ID(),
		AlgorithmVersion: "1",
		EvidenceSnapshot: snapshot.Records,
		ActionType:       r.cfg.ActionType,
		Title:            fmt.Sprintf("Reduce %s request for %s", r.cfg.ResourceKind, res.Name),
		Summary:          fmt.Sprintf("Reduce the %s reservation by %.0f%% using p95 usage with %.2fx headroom. The limit is unchanged. This is reserved capacity, not verified billing savings.", r.cfg.ResourceKind, reductionRatio*100, r.cfg.HeadroomRatio),
		Current: map[string]any{
			"resource": r.cfg.ResourceKind,
			"request":  currentReq,
			"limit":    currentLimit,
		},
		Proposed: map[string]any{
			"resource": r.cfg.ResourceKind,
			"request":  proposedReq,
			"limit":    proposedLimit,
		},
		Parameters: map[string]any{
			"confidence_assessment": confidence,
			"patch": map[string]any{
				"resource":         r.cfg.ResourceKind,
				"current_request":  currentReq,
				"proposed_request": proposedReq,
				"current_limit":    currentLimit,
				"proposed_limit":   proposedLimit,
			},
			"proposed": map[string]any{
				"resource": r.cfg.ResourceKind,
				"request":  proposedReq,
				"limit":    proposedLimit,
			},
			"verification_plan": map[string]any{
				"metrics_plugin_id": r.cfg.MetricsPluginID,
				"checks": []string{
					"memory p95 stays within configured increase ratio",
					"cpu p95 stays within configured increase ratio",
					"container restarts do not increase beyond threshold",
				},
			},
			"algorithm": map[string]any{
				"id":                  r.ID(),
				"metric_key":          r.cfg.MetricKey,
				"headroom_ratio":      r.cfg.HeadroomRatio,
				"min_reduction_ratio": r.cfg.MinReductionRatio,
				"observed":            observed,
				"reduction_ratio":     reductionRatio,
			},
		},
		Confidence:   confidence.Label,
		Risk:         riskFor(res, reductionRatio),
		Evidence:     append(evidenceLines(r, snapshot, observed, currentReq, proposedReq, reductionRatio), confidence.Reasons...),
		EvidenceRefs: evidenceIDs(snapshot),
		Status:       store.RecommendationPending,
	}, nil
}

func firstEvidence(snapshots []plugin.MetricsSnapshot) plugin.MetricsSnapshot {
	if len(snapshots) == 0 {
		return plugin.MetricsSnapshot{}
	}
	return snapshots[0]
}

func riskFor(res resource.Resource, reductionRatio float64) string {
	if res.Environment == resource.EnvProduction || res.Criticality == resource.CriticalityHigh || reductionRatio > 0.40 {
		return "medium"
	}
	return "low"
}

func signalPointCount(snapshot plugin.MetricsSnapshot) int {
	if v, ok := snapshot.Signals["memory_working_set_bytes_points"]; ok {
		n, ok := asFloat(v)
		if ok {
			return int(n)
		}
	}
	return 0
}

func evidenceLines(r *HeadroomRecommender, snapshot plugin.MetricsSnapshot, observed float64, currentReq, proposedReq int64, reductionRatio float64) []string {
	return []string{
		fmt.Sprintf("algorithm: %s", r.ID()),
		fmt.Sprintf("metrics source: %s", snapshot.Source),
		fmt.Sprintf("metrics window: %s", snapshot.Window),
		fmt.Sprintf("memory samples available: %d (confidence uses actual history, coverage, freshness and peak evidence)", signalPointCount(snapshot)),
		fmt.Sprintf("%s: %.2f", r.cfg.MetricKey, observed),
		fmt.Sprintf("current request: %d", currentReq),
		fmt.Sprintf("proposed request: %d", proposedReq),
		fmt.Sprintf("request reduction: %.2f%%", reductionRatio*100),
		fmt.Sprintf("retained headroom ratio: %.2f", r.cfg.HeadroomRatio),
		fmt.Sprintf("maximum reduction per change: %.0f%%; memory limit unchanged", r.cfg.MaxReductionRatio*100),
		"verification required after action using metrics plugin",
	}
}

func withDefaults(cfg bootstrap.RecommenderConfig) bootstrap.RecommenderConfig {
	if cfg.ConfidenceProfile == "" {
		cfg.ConfidenceProfile = "standard"
	}
	if cfg.MetricsPluginID == "" {
		cfg.MetricsPluginID = "prometheus-metrics"
	}
	if cfg.ActionPluginID == "" {
		cfg.ActionPluginID = "kubernetes-action"
	}
	if cfg.ActionType == "" {
		cfg.ActionType = "k8s.patch_resources"
	}
	if cfg.ResourceKind == "" {
		cfg.ResourceKind = "memory"
	}
	if cfg.MetricKey == "" {
		cfg.MetricKey = "memory_working_set_bytes_p95"
	}
	if cfg.CurrentRequestKey == "" {
		cfg.CurrentRequestKey = "memory_request_bytes"
	}
	if cfg.CurrentLimitKey == "" {
		cfg.CurrentLimitKey = "memory_limit_bytes"
	}
	if cfg.HeadroomRatio == 0 {
		cfg.HeadroomRatio = 1.5
	}
	if cfg.MinReductionRatio == 0 {
		cfg.MinReductionRatio = 0.10
	}
	if cfg.MaxReductionRatio == 0 {
		cfg.MaxReductionRatio = 0.25
	}
	return cfg
}

func numberFromState(values map[string]any, key string) (int64, error) {
	v, ok := values[key]
	if !ok {
		return 0, fmt.Errorf("resource current_state.%s is required", key)
	}
	n, ok := asFloat(v)
	if !ok {
		return 0, fmt.Errorf("resource current_state.%s must be numeric", key)
	}
	return int64(n), nil
}

func optionalNumberFromState(values map[string]any, key string) (int64, error) {
	if key == "" {
		return 0, nil
	}
	v, ok := values[key]
	if !ok {
		return 0, nil
	}
	n, ok := asFloat(v)
	if !ok {
		return 0, fmt.Errorf("resource current_state.%s must be numeric", key)
	}
	return int64(n), nil
}

func numberFromSignals(values map[string]any, key string) (float64, error) {
	v, ok := values[key]
	if !ok {
		return 0, fmt.Errorf("metrics signal %s is required", key)
	}
	n, ok := asFloat(v)
	if !ok {
		return 0, fmt.Errorf("metrics signal %s must be numeric", key)
	}
	return n, nil
}

func asFloat(v any) (float64, bool) {
	switch t := v.(type) {
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	case float64:
		return t, true
	case jsonNumber:
		n, err := t.Float64()
		return n, err == nil
	default:
		return 0, false
	}
}

type jsonNumber interface {
	Float64() (float64, error)
}

func evidenceIDs(snapshot plugin.MetricsSnapshot) []string {
	ids := make([]string, 0, len(snapshot.Records))
	for _, r := range snapshot.Records {
		ids = append(ids, r.ID)
	}
	return ids
}
