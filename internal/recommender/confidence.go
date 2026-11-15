package recommender

import (
	"fmt"
	"strings"
	"time"

	"github.com/consize-oss/consize/internal/bootstrap"
	"github.com/consize-oss/consize/pkg/evidence/quality"
	"github.com/consize-oss/consize/pkg/plugin"
	"github.com/consize-oss/consize/pkg/resource"
)

// ConfidenceReport describes confidence after shared quality gates; it is not a probability of safety.
type ConfidenceReport struct {
	Model           string    `json:"model"`
	Label           string    `json:"label"`
	Profile         string    `json:"profile"`
	Signal          string    `json:"signal"`
	RequiredHistory string    `json:"required_history"`
	ObservedHistory string    `json:"observed_history"`
	CoverageRatio   float64   `json:"coverage_ratio"`
	PeakToP95Ratio  float64   `json:"peak_to_p95_ratio"`
	CollectedAt     time.Time `json:"collected_at"`
	Reasons         []string  `json:"reasons"`
}

func AssessConfidence(cfg bootstrap.RecommenderConfig, res resource.Resource, snapshot plugin.MetricsSnapshot, now time.Time) ConfidenceReport {
	cfg = withDefaults(cfg)
	signal := strings.TrimSuffix(cfg.MetricKey, "_p95")
	report := ConfidenceReport{Model: "history-coverage-v1", Label: "low", Profile: cfg.ConfidenceProfile, Signal: signal, CollectedAt: snapshot.CollectedAt, Reasons: []string{}}
	highHistory := 7 * 24 * time.Hour
	if res.Environment == resource.EnvProduction || res.Criticality == resource.CriticalityHigh {
		highHistory = 14 * 24 * time.Hour
	}
	if cfg.ConfidenceProfile == "lab" {
		if res.Environment != resource.EnvDevelopment || res.Criticality == resource.CriticalityHigh {
			report.Reasons = append(report.Reasons, "lab profile is restricted to non-critical development resources")
		}
	}
	if cfg.ConfidenceProfile != "standard" && cfg.ConfidenceProfile != "lab" {
		report.Reasons = append(report.Reasons, "unknown confidence profile")
	}
	if snapshot.ResourceID != res.ID || snapshot.PluginID != cfg.MetricsPluginID {
		report.Reasons = append(report.Reasons, "metrics identity does not match resource or configured plugin")
	}
	assessment := AssessQuality(cfg, res, snapshot, now)
	for _, dimension := range assessment.Dimensions {
		if dimension.Mandatory && dimension.Status != "pass" {
			report.Reasons = append(report.Reasons, dimension.Code+": "+dimension.Message)
		}
	}
	span := time.Duration(0)
	historySeen, coverageSeen := false, false
	report.RequiredHistory = assessment.Thresholds.MinHistory
	for _, dimension := range assessment.Dimensions {
		if dimension.Observed == nil {
			continue
		}
		switch dimension.Name {
		case "history":
			observed := time.Duration(*dimension.Observed * float64(time.Second))
			if !historySeen || observed < span {
				span = observed
			}
			historySeen = true
		case "coverage", "source_coverage":
			if !coverageSeen || *dimension.Observed < report.CoverageRatio {
				report.CoverageRatio = *dimension.Observed
			}
			coverageSeen = true
		case "peak":
			report.PeakToP95Ratio = *dimension.Observed
		}
	}
	report.ObservedHistory = span.String()
	window, _ := time.ParseDuration(snapshot.Window)

	if len(report.Reasons) > 0 {
		return report
	}
	report.Label = "medium"
	report.Reasons = append(report.Reasons, "history, freshness, coverage and peak checks passed")
	if cfg.ConfidenceProfile == "lab" {
		report.Reasons = append(report.Reasons, "lab assessment only; not representative production history")
		return report
	}
	if window >= highHistory && span >= highHistory && report.CoverageRatio >= 0.98 {
		report.Label = "high"
	} else {
		report.Reasons = append(report.Reasons, fmt.Sprintf("high confidence requires %s and at least 98%% coverage", highHistory))
	}
	report.Reasons = append(report.Reasons, "known demand cycles, deployment history and incidents are not yet evaluated")
	return report
}

// AssessQuality is shared with generation and the evidence diagnostics API.
func AssessQuality(cfg bootstrap.RecommenderConfig, res resource.Resource, snapshot plugin.MetricsSnapshot, now time.Time) quality.Assessment {
	cfg = withDefaults(cfg)
	production := res.Environment == resource.EnvProduction || res.Criticality == resource.CriticalityHigh
	thresholds, err := quality.Resolve(cfg.Quality, quality.Recommendation, production, cfg.ConfidenceProfile == "lab")
	if err != nil {
		return quality.Assess(quality.Input{Invalid: true}, quality.Recommendation, thresholds, now)
	}
	if snapshot.ResourceID != res.ID || snapshot.PluginID != cfg.MetricsPluginID {
		snapshot.Records = nil
	}
	return snapshot.AssessQuality([]quality.Signal{{Name: cfg.MetricKey, Unit: "bytes", PeakName: quality.BaseSignal(cfg.MetricKey) + "_max"}}, quality.Recommendation, thresholds, now)
}
