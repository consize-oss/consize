package kubernetes

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"github.com/consize-oss/consize/pkg/plugin"
	"io"
	validation "k8s.io/apimachinery/pkg/util/validation"
	"net"
	"net/http"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

// ScanError holds only allowlisted classifications and configured namespace
// coordinates. Raw API bodies, URLs and credential-plugin errors are not retained.
type ScanError struct {
	Code              string `json:"code"`
	Retryable         bool   `json:"retryable"`
	RetryAfterSeconds int    `json:"retry_after_seconds,omitempty"`
	Verb              string `json:"verb,omitempty"`
	APIGroup          string `json:"api_group,omitempty"`
	Resource          string `json:"resource,omitempty"`
	Namespace         string `json:"namespace,omitempty"`
	cause             error
}

func scanCode(code string) string {
	switch code {
	case "configuration", "unauthorized", "forbidden", "tls", "network", "timeout", "rate_limited", "server", "request", "redirect_blocked", "canceled", "expired_continue", "unavailable", "invalid_observation", "invalid_page", "limit_exceeded", "unsupported_client", "scan_busy", "provider_failure":
		return code
	}
	return "provider_failure"
}
func ScanRetryable(code string) bool {
	switch code {
	case "network", "timeout", "rate_limited", "server", "unavailable", "scan_busy":
		return true
	}
	return false
}
func (e *ScanError) Error() string {
	scope := ""
	if e.Verb == "list" && e.APIGroup == "apps" && e.Resource == "deployments" && (e.Namespace == "*" || len(validation.IsDNS1123Label(e.Namespace)) == 0) {
		scope = " [list apps/deployments namespace=" + e.Namespace + "]"
	}
	return "kubernetes discovery failed: " + scanCode(e.Code) + scope + "; " + accessGuidance(scanCode(e.Code))
}
func (e *ScanError) Unwrap() error { return e.cause }
func scanFailure(code string) *ScanError {
	code = scanCode(code)
	return &ScanError{Code: code, Retryable: ScanRetryable(code)}
}
func safeScanError(err error) *ScanError {
	var known *ScanError
	if errors.As(err, &known) {
		out := scanFailure(known.Code)
		out.cause = safeMappingCause(known)
		if errors.Is(known, context.Canceled) {
			out.cause = context.Canceled
		}
		if errors.Is(known, context.DeadlineExceeded) {
			out.cause = context.DeadlineExceeded
		}
		if known.RetryAfterSeconds > 0 {
			out.RetryAfterSeconds = known.RetryAfterSeconds
			if out.RetryAfterSeconds > 300 {
				out.RetryAfterSeconds = 300
			}
		}
		if known.Verb == "list" && known.APIGroup == "apps" && known.Resource == "deployments" && (known.Namespace == "*" || len(validation.IsDNS1123Label(known.Namespace)) == 0) {
			out.Verb = "list"
			out.APIGroup = "apps"
			out.Resource = "deployments"
			out.Namespace = known.Namespace
		}
		return out
	}
	if errors.Is(err, context.Canceled) {
		e := scanFailure("canceled")
		e.cause = context.Canceled
		return e
	}
	if errors.Is(err, context.DeadlineExceeded) {
		e := scanFailure("timeout")
		e.cause = context.DeadlineExceeded
		return e
	}
	var mapping *MappingError
	if errors.As(err, &mapping) {
		out := scanFailure("invalid_observation")
		out.cause = safeMappingCause(err)
		return out
	}
	switch {
	case apierrors.IsUnauthorized(err):
		return scanFailure("unauthorized")
	case apierrors.IsForbidden(err):
		return scanFailure("forbidden")
	case apierrors.IsResourceExpired(err) || apierrors.IsGone(err):
		return scanFailure("expired_continue")
	case apierrors.IsTimeout(err) || apierrors.IsServerTimeout(err):
		return scanFailure("timeout")
	case apierrors.IsTooManyRequests(err):
		e := scanFailure("rate_limited")
		if seconds, ok := apierrors.SuggestsClientDelay(err); ok && seconds > 0 {
			e.RetryAfterSeconds = seconds
			if seconds > 300 {
				e.RetryAfterSeconds = 300
			}
		}
		return e
	case apierrors.IsServiceUnavailable(err) || apierrors.IsInternalError(err):
		return scanFailure("server")
	case apierrors.IsBadRequest(err) || apierrors.IsNotFound(err) || apierrors.IsMethodNotSupported(err):
		return scanFailure("request")
	}
	var verification *tls.CertificateVerificationError
	var unknown x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var invalid x509.CertificateInvalidError
	var record tls.RecordHeaderError
	if errors.As(err, &verification) || errors.As(err, &unknown) || errors.As(err, &hostname) || errors.As(err, &invalid) || errors.As(err, &record) {
		return scanFailure("tls")
	}
	var network net.Error
	if errors.As(err, &network) {
		if network.Timeout() {
			return scanFailure("timeout")
		}
		return scanFailure("network")
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return scanFailure("network")
	}
	return scanFailure("provider_failure")
}
func accessGuidance(code string) string {
	switch code {
	case "configuration":
		return "check the explicit kubeconfig/context or in-cluster service account and verified HTTPS settings"
	case "unauthorized":
		return "renew the configured credential and verify its audience and expiry"
	case "forbidden":
		return "grant list on apps/deployments only in the configured namespaces"
	case "tls":
		return "verify the API server CA, certificate validity and server name; do not disable TLS verification"
	case "network", "unavailable":
		return "check API server connectivity and retry"
	case "timeout":
		return "check connectivity and the bounded request/scan timeout"
	case "rate_limited":
		return "wait for the bounded retry delay and reduce scan frequency"
	case "server":
		return "check API server availability and retry"
	case "request":
		return "check the supported API and configured discovery scope"
	case "redirect_blocked":
		return "configure the final verified API server endpoint directly"
	case "canceled":
		return "the caller canceled the operation"
	default:
		return "inspect the configured discovery scope and safe scan diagnostics"
	}
}
func scopedScanError(err error, namespace string) *ScanError {
	e := safeScanError(err)
	e.Verb = "list"
	e.APIGroup = "apps"
	e.Resource = "deployments"
	if namespace == "" {
		namespace = "*"
	}
	e.Namespace = namespace
	return e
}
func statusScanError(status int, retryAfter string) *ScanError {
	code := "request"
	switch {
	case status == http.StatusUnauthorized:
		code = "unauthorized"
	case status == http.StatusForbidden:
		code = "forbidden"
	case status == http.StatusTooManyRequests:
		code = "rate_limited"
	case status == http.StatusGone:
		code = "expired_continue"
	case status == http.StatusRequestTimeout || status == http.StatusGatewayTimeout:
		code = "timeout"
	case status >= 500:
		code = "server"
	case status >= 300 && status < 400:
		code = "redirect_blocked"
	}
	e := scanFailure(code)
	if seconds, err := time.ParseDuration(retryAfter + "s"); err == nil && seconds > 0 {
		if seconds > 5*time.Minute {
			seconds = 5 * time.Minute
		}
		e.RetryAfterSeconds = int(seconds / time.Second)
	} else if at, err := http.ParseTime(retryAfter); err == nil {
		seconds := int(time.Until(at).Seconds())
		if seconds > 300 {
			seconds = 300
		}
		if seconds > 0 {
			e.RetryAfterSeconds = seconds
		}
	}
	return e
}

func safeMappingCause(err error) error {
	var mapping *MappingError
	if !errors.As(err, &mapping) {
		return nil
	}
	switch mapping.Code {
	case "observation_time_required", "invalid_object_identity", "uid_required_or_invalid", "invalid_cluster_identity", "invalid_replica_or_generation", "invalid_annotation_allowlist", "native_metadata_limit_or_identity", "invalid_environment", "invalid_owner", "invalid_criticality", "invalid_or_duplicate_container", "request_exceeds_limit", "quantity_overflow", "container_required", "mapping_metadata_limit", "mapping_metadata_invalid", "invalid_universal_resource":
		return &MappingError{Code: mapping.Code}
	}
	return &MappingError{Code: "invalid_universal_resource"}
}

func (e *ScanError) ProviderFailure() plugin.FailureDetails {
	return plugin.SafeFailureDetails(plugin.FailureDetails{Code: e.Code, Retryable: ScanRetryable(e.Code), RetryAfterSeconds: e.RetryAfterSeconds})
}
