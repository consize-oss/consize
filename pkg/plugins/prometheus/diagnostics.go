package prometheus

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"time"
)

// Error contains only safe classifications, never an upstream URL, body, query,
// header value or raw error. Unwrap preserves cancellation semantics only.
type Error struct {
	Code              string `json:"code"`
	Status            int    `json:"http_status,omitempty"`
	Retryable         bool   `json:"retryable"`
	RetryAfterSeconds int    `json:"retry_after_seconds,omitempty"`
	cause             error
}

func (e *Error) Error() string   { return "prometheus request failed: " + e.Code }
func (e *Error) Unwrap() error   { return e.cause }
func failure(code string) *Error { return &Error{Code: code} }
func safeError(err error) *Error {
	var e *Error
	if errors.As(err, &e) {
		return &Error{Code: e.Code, Status: e.Status, Retryable: e.Retryable, RetryAfterSeconds: e.RetryAfterSeconds, cause: e.cause}
	}
	if errors.Is(err, context.Canceled) {
		return &Error{Code: "canceled", cause: context.Canceled}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &Error{Code: "timeout", cause: context.DeadlineExceeded}
	}
	var certificate *tls.CertificateVerificationError
	var unknown x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var record tls.RecordHeaderError
	if strings.Contains(err.Error(), "tls:") || errors.As(err, &certificate) || errors.As(err, &unknown) || errors.As(err, &hostname) || errors.As(err, &record) {
		return failure("tls")
	}
	if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
		return &Error{Code: "network", Retryable: true}
	}
	var network net.Error
	if errors.As(err, &network) {
		if network.Timeout() {
			return &Error{Code: "timeout", Retryable: true}
		}
		return &Error{Code: "network", Retryable: true}
	}
	return failure("provider_failure")
}
func statusError(status int) *Error {
	e := &Error{Status: status}
	switch {
	case status == 401:
		e.Code = "authentication"
	case status == 403:
		e.Code = "authorization"
	case status == 429:
		e.Code = "rate_limited"
		e.Retryable = true
	case status == 400 || status == 422:
		e.Code = "query"
	case status >= 500:
		e.Code = "server"
		e.Retryable = true
	case status >= 300 && status < 400:
		e.Code = "redirect_blocked"
	default:
		e.Code = "http_status"
	}
	return e
}

type Annotation struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
type Diagnostics struct {
	State         string     `json:"state"`
	LastRequestAt *time.Time `json:"last_request_at"`
	LastSuccessAt *time.Time `json:"last_success_at"`
	LatestError   *Error     `json:"latest_error"`
	WarningCount  int        `json:"warning_count"`
	InfoCount     int        `json:"info_count"`
	Attempts      int        `json:"attempts"`
}
type diagnosticState struct {
	mu       sync.Mutex
	value    Diagnostics
	sequence uint64
}

func (s *diagnosticState) begin() (uint64, time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sequence++
	at := time.Now().UTC()
	s.value.LastRequestAt = &at
	return s.sequence, at
}
func (s *diagnosticState) finish(sequence uint64, result Result, err error, attempts int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// A slower old request must not overwrite a newer request's diagnostics.
	if sequence != s.sequence {
		return
	}
	s.value.Attempts = attempts
	s.value.WarningCount = len(result.Warnings)
	s.value.InfoCount = len(result.Infos)
	if err != nil {
		s.value.State = "unavailable"
		s.value.LatestError = safeError(err)
		if s.value.LatestError.Code == "partial_response" {
			s.value.State = "degraded"
		}
		return
	}
	now := time.Now().UTC()
	s.value.LastSuccessAt = &now
	s.value.LatestError = nil
	s.value.State = "reachable"
	if len(result.Warnings) > 0 {
		s.value.State = "degraded"
	}
}
func (s *diagnosticState) snapshot() Diagnostics {
	s.mu.Lock()
	defer s.mu.Unlock()
	d := s.value
	if d.State == "" {
		d.State = "configured"
	}
	if d.LastRequestAt != nil {
		v := *d.LastRequestAt
		d.LastRequestAt = &v
	}
	if d.LastSuccessAt != nil {
		v := *d.LastSuccessAt
		d.LastSuccessAt = &v
	}
	if d.LatestError != nil {
		e := *d.LatestError
		d.LatestError = &e
	}
	return d
}
