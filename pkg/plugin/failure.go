package plugin

import (
	"context"
	"errors"
	"github.com/consize-oss/consize/pkg/evidence/quality"
)

// ProviderFailure exposes only structured, non-sensitive retry information.
// Core workers use this SDK interface rather than importing a provider package.
type ProviderFailure interface{ ProviderFailure() FailureDetails }
type FailureDetails struct {
	Code              string `json:"code"`
	Retryable         bool   `json:"retryable"`
	RetryAfterSeconds int    `json:"retry_after_seconds"`
}

// SafeFailureDetails rejects arbitrary provider text and bounds retry hints.
func SafeFailureDetails(v FailureDetails) FailureDetails {
	switch v.Code {
	case "configuration", "unit_configuration", "authentication", "authorization", "unauthorized", "forbidden", "tls", "network", "timeout", "rate_limited", "server", "request", "query", "redirect_blocked", "canceled", "expired_continue", "unavailable", "invalid_observation", "invalid_page", "limit_exceeded", "unsupported_client", "scan_busy", "provider_failure", "partial_response":
	default:
		v.Code = "provider_failure"
		v.Retryable = false
	}
	if v.RetryAfterSeconds < 0 {
		v.RetryAfterSeconds = 0
	}
	if v.RetryAfterSeconds > 300 {
		v.RetryAfterSeconds = 300
	}
	return v
}

// invocationFailure retains SDK gate types used by orchestration while removing
// free-form provider error text. Provider bodies and panic values never escape.
func invocationFailure(err error) error {
	var runtimeFailure *RuntimeError
	if errors.As(err, &runtimeFailure) {
		out := &RuntimeError{Code: safeFailureCode(runtimeFailure.Code)}
		if runtimeFailure.Provider != nil {
			details := SafeFailureDetails(*runtimeFailure.Provider)
			out.Provider = &details
		}
		return out
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return contextFailure(err)
	}
	var preflight *PreflightError
	if errors.As(err, &preflight) {
		if len(preflight.Checks) == 0 || len(preflight.Checks) > 64 {
			return &RuntimeError{Code: FailureInvalidResponse}
		}
		checks := make([]PreflightCheck, len(preflight.Checks))
		for i, c := range preflight.Checks {
			status := c.Status
			if status != "passed" && status != "failed" && status != "unknown" && status != "blocked" {
				status = "unknown"
			}
			id := c.ID
			if !runtimeID.MatchString(id) {
				id = "plugin_preflight"
			}
			checks[i] = PreflightCheck{ID: id, Status: status, Message: "Plugin preflight check: " + status}
		}
		return &PreflightError{Checks: checks}
	}
	var blocked *quality.BlockedError
	if errors.As(err, &blocked) {
		assessment, copyErr := freeze(blocked.Assessment)
		if copyErr != nil || assessment.Version != quality.Version || len(assessment.Dimensions) > 256 {
			return &RuntimeError{Code: FailureInvalidResponse}
		}
		assessment.Eligible = false
		for i := range assessment.Dimensions {
			assessment.Dimensions[i].Message = "Evidence quality gate did not pass"
		}
		return &quality.BlockedError{Assessment: assessment}
	}
	var provider ProviderFailure
	if errors.As(err, &provider) {
		details := SafeFailureDetails(provider.ProviderFailure())
		return &RuntimeError{Code: FailureUnavailable, Provider: &details}
	}
	return &RuntimeError{Code: FailureInternal}
}
