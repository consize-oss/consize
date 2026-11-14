// Package inventory composes read-only coverage assessments from existing
// registry, evidence, policy and capability contracts. It never calls providers.
package inventory

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/consize-oss/consize/internal/policy"
	"github.com/consize-oss/consize/internal/store"
	"github.com/consize-oss/consize/pkg/evidence"
	"github.com/consize-oss/consize/pkg/plugin"
	"github.com/consize-oss/consize/pkg/resource"
)

const Version = "inventory-v1"

type AttentionReason string

const (
	Unowned              AttentionReason = "unowned"
	TelemetryStale       AttentionReason = "telemetry_stale"
	TelemetryUnmonitored AttentionReason = "telemetry_unmonitored"
	TelemetryUnknown     AttentionReason = "telemetry_unknown"
	PolicyGap            AttentionReason = "policy_gap"
	PolicyUnknown        AttentionReason = "policy_unknown"
	ActionUnsupported    AttentionReason = "action_unsupported"
	ActionUnknown        AttentionReason = "action_unknown"
)

type Telemetry struct {
	Status            string     `json:"status"`
	SourcePluginIDs   []string   `json:"source_plugin_ids"`
	LastMeasurementAt *time.Time `json:"last_measurement_at"`
	Reason            string     `json:"reason"`
}
type Actions struct {
	Status    string   `json:"status"`
	PluginIDs []string `json:"plugin_ids"`
	Reason    string   `json:"reason"`
}
type Assessment struct {
	Version                 string            `json:"version"`
	AssessedAt              time.Time         `json:"assessed_at"`
	Owner                   *string           `json:"owner"`
	Telemetry               Telemetry         `json:"telemetry"`
	Policy                  policy.Coverage   `json:"policy"`
	Actions                 Actions           `json:"actions"`
	ActiveRecommendationIDs []int64           `json:"active_recommendation_ids"`
	OptimizationStatus      string            `json:"optimization_status"`
	AttentionReasons        []AttentionReason `json:"attention_reasons"`
}
type Row struct {
	Resource   resource.Resource `json:"resource"`
	Assessment Assessment        `json:"assessment"`
}
type Signal struct{ Name, Unit string }
type PolicyReader interface {
	Coverage(resource.Resource) policy.Coverage
}

// Build evaluates every row against the same time and input snapshot. Manifests
// must be from the configured manager, not resource-supplied metadata. A nil
// manifest slice means capability assessment is unavailable; empty means none.
func Build(ctx context.Context, input store.InventorySnapshot, manifests []plugin.Manifest, policies PolicyReader, signals []Signal, at time.Time) ([]Row, error) {
	at = at.UTC()
	recs := map[string][]store.Recommendation{}
	records := map[string][]evidence.Record{}
	for _, rec := range input.Recommendations {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		recs[rec.ResourceID] = append(recs[rec.ResourceID], rec)
		for _, record := range rec.EvidenceSnapshot {
			if record.ResourceID == rec.ResourceID {
				records[rec.ResourceID] = append(records[rec.ResourceID], record)
			}
		}
	}
	for _, job := range input.Jobs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		snapshots := []plugin.MetricsSnapshot{job.Baseline}
		if job.Result != nil {
			snapshots = append(snapshots, job.Result.Before, job.Result.After)
		}
		for _, snapshot := range snapshots {
			if snapshot.ResourceID != job.Resource.ID || snapshot.ValidateRecords() != nil {
				continue
			}
			records[job.Resource.ID] = append(records[job.Resource.ID], snapshot.Records...)
		}
	}
	out := make([]Row, 0, len(input.Resources))
	for _, res := range input.Resources {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		a := Assessment{Version: Version, AssessedAt: at, ActiveRecommendationIDs: []int64{}, AttentionReasons: []AttentionReason{}}
		owner := strings.TrimSpace(res.Owner)
		if owner != "" && !strings.EqualFold(owner, "unassigned") {
			a.Owner = &owner
		} else {
			a.AttentionReasons = append(a.AttentionReasons, Unowned)
		}
		telemetry, err := assessTelemetry(ctx, res, records[res.ID], manifests, signals, at)
		if err != nil {
			return nil, err
		}
		a.Telemetry = telemetry
		switch a.Telemetry.Status {
		case "stale":
			a.AttentionReasons = append(a.AttentionReasons, TelemetryStale)
		case "unmonitored":
			a.AttentionReasons = append(a.AttentionReasons, TelemetryUnmonitored)
		case "unknown":
			a.AttentionReasons = append(a.AttentionReasons, TelemetryUnknown)
		}
		a.Policy = policy.Coverage{Status: "unknown", PolicyIDs: []string{}, Reason: "policy_assessment_unavailable"}
		if policies != nil {
			a.Policy = policies.Coverage(res)
		}
		// Fail closed on incomplete/malformed assessor output; absence of rules is
		// a gap only when the policy resolver explicitly confirms it.
		if a.Policy.Status == "covered" && len(a.Policy.PolicyIDs) == 0 || a.Policy.Status != "covered" && a.Policy.Status != "uncovered" {
			a.Policy = policy.Coverage{Status: "unknown", PolicyIDs: []string{}, Reason: "policy_assessment_unavailable"}
		}
		if a.Policy.PolicyIDs == nil {
			a.Policy.PolicyIDs = []string{}
		}
		a.Policy.PolicyIDs = append([]string{}, a.Policy.PolicyIDs...)
		sort.Strings(a.Policy.PolicyIDs)
		if a.Policy.Status == "unknown" {
			a.AttentionReasons = append(a.AttentionReasons, PolicyUnknown)
		}
		if a.Policy.Status == "uncovered" {
			a.AttentionReasons = append(a.AttentionReasons, PolicyGap)
		}
		a.Actions = assessActions(res.Type, manifests)
		if a.Actions.Status == "unsupported" {
			a.AttentionReasons = append(a.AttentionReasons, ActionUnsupported)
		}
		if a.Actions.Status == "unknown" {
			a.AttentionReasons = append(a.AttentionReasons, ActionUnknown)
		}
		a.OptimizationStatus = "awaiting_evidence"
		if a.Telemetry.Status == "usable" {
			a.OptimizationStatus = "no_active_recommendations"
		}
		executing := false
		for _, rec := range recs[res.ID] {
			if rec.SupersededBy != 0 || !rec.ExpiresAt.IsZero() && !at.Before(rec.ExpiresAt) || rec.CreatedAt.After(at) {
				continue
			}
			switch rec.Status {
			case store.RecommendationPending, store.RecommendationPlanned, store.RecommendationApproved, store.RecommendationExecuting:
				a.ActiveRecommendationIDs = append(a.ActiveRecommendationIDs, rec.ID)
				if rec.Status == store.RecommendationExecuting {
					executing = true
				}
			}
		}
		sort.Slice(a.ActiveRecommendationIDs, func(i, j int) bool { return a.ActiveRecommendationIDs[i] < a.ActiveRecommendationIDs[j] })
		if len(a.ActiveRecommendationIDs) > 0 {
			a.OptimizationStatus = "recommendations_available"
		}
		if executing {
			a.OptimizationStatus = "executing"
		}
		out = append(out, Row{Resource: res, Assessment: a})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Resource.ID < out[j].Resource.ID })
	return out, nil
}

func assessActions(typ string, manifests []plugin.Manifest) Actions {
	out := Actions{Status: "unsupported", PluginIDs: []string{}, Reason: "no_registered_action_path"}
	if manifests == nil {
		out.Status, out.Reason = "unknown", "capability_assessment_unavailable"
		return out
	}
	for _, m := range manifests {
		if m.Category == plugin.CategoryAction && plugin.Supports(m.SupportedResourceTypes, typ) && len(m.SupportedActionTypes) > 0 && plugin.Supports(m.Capabilities, plugin.CapabilityActionPlan) && plugin.Supports(m.Capabilities, plugin.CapabilityActionExecute) {
			out.PluginIDs = append(out.PluginIDs, m.ID)
		}
	}
	if len(out.PluginIDs) > 0 {
		sort.Strings(out.PluginIDs)
		out.Status, out.Reason = "supported", "registered_action_path"
	}
	return out
}

func assessTelemetry(ctx context.Context, res resource.Resource, records []evidence.Record, manifests []plugin.Manifest, signals []Signal, at time.Time) (Telemetry, error) {
	out := Telemetry{Status: "unmonitored", SourcePluginIDs: []string{}, Reason: "metrics_not_configured"}
	if manifests == nil {
		out.Status, out.Reason = "unknown", "capability_assessment_unavailable"
		return out, nil
	}
	providers := []string{}
	for _, m := range manifests {
		if plugin.Supports(m.Capabilities, plugin.CapabilityMetricsRead) && !m.CanMutateInfrastructure && plugin.Supports(m.SupportedResourceTypes, res.Type) {
			providers = append(providers, m.ID)
		}
	}
	sort.Strings(providers)
	if len(providers) == 0 {
		return out, nil
	}
	// A complete set must come from one source. Combining sparse sources would
	// create a fictitious complete observation.
	out.Status, out.Reason = "unknown", "no_trusted_evidence"
	best := -1
	for _, id := range providers {
		latest := map[string]evidence.Record{}
		conflicts := map[string]bool{}
		var lastSuccessful *time.Time
		for _, r := range records {
			if err := ctx.Err(); err != nil {
				return Telemetry{}, err
			}
			if r.ResourceID != res.ID || r.PluginID != id || r.Validate() != nil || r.CollectedAt.After(at) {
				continue
			}
			if (r.Quality == evidence.Valid || r.Quality == evidence.Stale) && (lastSuccessful == nil || r.WindowEnd.After(*lastSuccessful)) {
				t := r.WindowEnd
				lastSuccessful = &t
			}
			prior, ok := latest[r.Signal]
			if !ok || r.CollectedAt.After(prior.CollectedAt) {
				latest[r.Signal] = r
				conflicts[r.Signal] = false
			} else if r.CollectedAt.Equal(prior.CollectedAt) && r.ID != prior.ID {
				conflicts[r.Signal] = true
			}
		}
		candidate := Telemetry{Status: "unknown", SourcePluginIDs: []string{id}, Reason: "no_trusted_evidence"}
		candidate.LastMeasurementAt = lastSuccessful
		if len(latest) > 0 && len(signals) > 0 {
			stale, incomplete, missing := false, false, 0
			for _, spec := range signals {
				r, ok := latest[spec.Name]
				if !ok || conflicts[spec.Name] || spec.Unit != "" && r.Unit != spec.Unit {
					incomplete = true
					continue
				}
				switch {
				case r.Quality == evidence.Missing:
					missing++
				case (r.Quality == evidence.Valid || r.Quality == evidence.Stale) && r.SampleCount < 2:
					incomplete = true
				case r.Quality == evidence.Stale || r.Quality == evidence.Valid && !r.Usable(at):
					stale = true
				case !r.Usable(at):
					incomplete = true
				}
			}
			switch {
			case missing == len(signals):
				candidate.Status, candidate.Reason = "unmonitored", "measurements_missing"
			case incomplete || missing > 0:
				candidate.Reason = "incomplete_or_invalid_evidence"
			case stale:
				candidate.Status, candidate.Reason = "stale", "measurements_stale"
			default:
				candidate.Status, candidate.Reason = "usable", "required_measurements_usable"
			}
		}
		rank := map[string]int{"unmonitored": 0, "unknown": 1, "stale": 2, "usable": 3}[candidate.Status]
		if rank > best || rank == best && candidate.LastMeasurementAt != nil && (out.LastMeasurementAt == nil || candidate.LastMeasurementAt.After(*out.LastMeasurementAt)) {
			out, best = candidate, rank
		}
	}
	return out, nil
}
