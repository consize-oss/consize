package kubernetes

import (
	"context"
	"errors"
	"net"
	"sort"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	validation "k8s.io/apimachinery/pkg/util/validation"
)

type DiscoveryConfig struct {
	Namespaces          []string `json:"namespaces,omitempty"`
	LabelSelector       string   `json:"label_selector,omitempty"`
	PageSize            int64    `json:"page_size,omitempty"`
	MaxBytes            int64    `json:"max_bytes,omitempty"`
	MaxObjects          int      `json:"max_objects,omitempty"`
	Timeout             string   `json:"timeout,omitempty"`
	AnnotationAllowlist []string `json:"annotation_allowlist,omitempty"`
}
type discoveryOptions struct {
	namespaces  []string
	selector    labels.Selector
	pageSize    int64
	maxBytes    int64
	maxObjects  int
	timeout     time.Duration
	annotations []string
}

func discoveryDefaults(c DiscoveryConfig) (discoveryOptions, error) {
	o := discoveryOptions{pageSize: 200, maxObjects: 10000, maxBytes: 32 << 20, timeout: 30 * time.Second}
	if c.MaxBytes != 0 {
		o.maxBytes = c.MaxBytes
	}
	if c.PageSize != 0 {
		o.pageSize = c.PageSize
	}
	if c.MaxObjects != 0 {
		o.maxObjects = c.MaxObjects
	}
	if c.Timeout != "" {
		d, err := time.ParseDuration(c.Timeout)
		if err != nil {
			return o, scanFailure("configuration")
		}
		o.timeout = d
	}
	if o.maxBytes < 64<<10 || o.maxBytes > 256<<20 || o.pageSize < 1 || o.pageSize > 1000 || o.maxObjects < 1 || o.maxObjects > 100000 || o.timeout < time.Millisecond || o.timeout > 5*time.Minute || len(c.Namespaces) > 128 || len(c.LabelSelector) > 4096 || len(c.AnnotationAllowlist) > 16 {
		return o, scanFailure("configuration")
	}
	var err error
	o.selector, err = labels.Parse(c.LabelSelector)
	if err != nil {
		return o, scanFailure("configuration")
	}
	seen := map[string]bool{}
	for _, ns := range c.Namespaces {
		if len(validation.IsDNS1123Label(ns)) > 0 || seen[ns] {
			return o, scanFailure("configuration")
		}
		seen[ns] = true
		o.namespaces = append(o.namespaces, ns)
	}
	sort.Strings(o.namespaces)
	seen = map[string]bool{}
	for _, key := range c.AnnotationAllowlist {
		if len(validation.IsQualifiedName(key)) > 0 || seen[key] || sensitiveAnnotation(key) {
			return o, scanFailure("configuration")
		}
		seen[key] = true
		o.annotations = append(o.annotations, key)
	}
	sort.Strings(o.annotations)
	return o, nil
}
func sensitiveAnnotation(key string) bool {
	lower := strings.ToLower(key)
	for _, part := range []string{"token", "password", "passwd", "secret", "credential", "authorization", "private-key", "private_key", "last-applied-configuration"} {
		if strings.Contains(lower, part) {
			return true
		}
	}
	return false
}

// ScanError never retains API bodies, request URLs, auth-plugin output or config
// contents. Only cancellation/deadline sentinels remain available through Unwrap.
type ScanError struct {
	Code  string `json:"code"`
	cause error
}

func (e *ScanError) Error() string       { return "kubernetes discovery failed: " + e.Code }
func (e *ScanError) Unwrap() error       { return e.cause }
func scanFailure(code string) *ScanError { return &ScanError{Code: code} }
func safeScanError(err error) *ScanError {
	var known *ScanError
	if errors.As(err, &known) {
		return known
	}
	if errors.Is(err, context.Canceled) {
		return &ScanError{Code: "canceled", cause: context.Canceled}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &ScanError{Code: "timeout", cause: context.DeadlineExceeded}
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
		return scanFailure("rate_limited")
	case apierrors.IsServiceUnavailable(err):
		return scanFailure("unavailable")
	}
	var network net.Error
	if errors.As(err, &network) && network.Timeout() {
		return scanFailure("timeout")
	}
	return scanFailure("unavailable")
}
