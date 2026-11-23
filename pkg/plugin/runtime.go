package plugin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/consize-oss/consize/pkg/provenance"
	"sync"
	"sync/atomic"
	"time"
)

type FailureCode string

const (
	FailureUnavailable     FailureCode = "unavailable"
	FailureIncompatible    FailureCode = "incompatible"
	FailureInvalidResponse FailureCode = "invalid_response"
	FailureTimeout         FailureCode = "timeout"
	FailureCanceled        FailureCode = "canceled"
	FailureInternal        FailureCode = "internal"
	FailureUnsupported     FailureCode = "capability_unsupported"
	FailureInvalidRequest  FailureCode = "invalid_request"
	FailureBusy            FailureCode = "busy"
)

// RuntimeError deliberately excludes plugin error strings and panic payloads.
type RuntimeError struct {
	Code     FailureCode     `json:"code"`
	Provider *FailureDetails `json:"-"`
}

func (e *RuntimeError) Error() string { return "plugin invocation failed: " + string(e.Code) }
func (e *RuntimeError) Unwrap() error {
	switch e.Code {
	case FailureCanceled:
		return context.Canceled
	case FailureTimeout:
		return context.DeadlineExceeded
	case FailureUnsupported:
		return ErrUnsupported
	}
	return nil
}

// InvocationIdentity is copied through context, including nested capabilities.
// Providers must not use it as authorization; the core authorizes before dispatch.
type InvocationIdentity struct {
	CorrelationID string `json:"correlation_id,omitempty"`
	OperationID   string `json:"operation_id,omitempty"`
	Actor         string `json:"actor,omitempty"`
	WorkflowID    string `json:"workflow_id,omitempty"`
}
type invocationKey struct{}

func WithInvocation(ctx context.Context, identity InvocationIdentity) context.Context {
	return context.WithValue(ctx, invocationKey{}, identity)
}
func InvocationFrom(ctx context.Context) InvocationIdentity {
	identity, _ := ctx.Value(invocationKey{}).(InvocationIdentity)
	return identity
}

type InstanceState string

const (
	StateDiscovered   InstanceState = "discovered"
	StateInstalled    InstanceState = "installed"
	StateDisabled     InstanceState = "disabled"
	StateConfiguring  InstanceState = "configuring"
	StateReady        InstanceState = "ready"
	StateDegraded     InstanceState = "degraded"
	StateUnavailable  InstanceState = "unavailable"
	StateIncompatible InstanceState = "incompatible"
	StateFailed       InstanceState = "failed"
	StateDraining     InstanceState = "draining"
	StateStopped      InstanceState = "stopped"
)

type InstanceConfig struct {
	CallTimeout   string          `json:"timeout,omitempty"`
	ID            string          `json:"id"`
	PackageID     string          `json:"package_id"`
	Required      bool            `json:"required"`
	Disabled      bool            `json:"disabled"`
	Provider      string          `json:"provider,omitempty"`
	Account       string          `json:"account,omitempty"`
	Location      string          `json:"location,omitempty"`
	Configuration json.RawMessage `json:"configuration,omitempty"`
	// SecretRefs contains reference names only. Values are resolved by the host and
	// passed separately to initialization; they never enter inventory or journals.
	SecretRefs map[string]string `json:"secret_refs,omitempty"`
	Timeout    time.Duration     `json:"-"`
}

type Provenance = provenance.Identity

type InstanceStatus struct {
	ProviderFailure *FailureDetails `json:"provider_failure,omitempty"`
	Manifest        Manifest        `json:"manifest"`
	Identity        Provenance      `json:"identity"`
	State           InstanceState   `json:"state"`
	Required        bool            `json:"required"`
	LastHealth      *time.Time      `json:"last_health,omitempty"`
	LastSuccess     *time.Time      `json:"last_success,omitempty"`
	LastFailure     *time.Time      `json:"last_failure,omitempty"`
	Failure         FailureCode     `json:"failure,omitempty"`
	Active          int             `json:"active"`
}

// RuntimeJournal implementations must atomically persist only sanitized status.
// Configuration and secrets are deliberately absent from this interface.
type RuntimeJournal interface {
	SavePluginStatus(context.Context, InstanceStatus) error
	LoadPluginStatuses(context.Context) ([]InstanceStatus, error)
}
type ScopeDeclarer interface{ InstanceScope() ResourceScope }

type ConfigValidator interface {
	ValidateConfiguration(context.Context, json.RawMessage) error
}
type Initializer interface {
	Initialize(context.Context, json.RawMessage, map[string]string) error
}
type Drainer interface{ Drain(context.Context) error }
type Shutdowner interface{ Shutdown(context.Context) error }

type invokeMode uint8

const (
	modeInvoke invokeMode = iota
	modeLifecycle
	modeProbe
)

type runtimeEntry struct {
	scope       map[string]string
	timeout     time.Duration
	persistGate chan struct{}
	drainGate   chan struct{}
	mu          sync.Mutex
	strict      bool
	raw         Plugin
	manifest    Manifest
	config      InstanceConfig
	status      InstanceStatus
	active      int
	idle        chan struct{}
	journal     RuntimeJournal
}

func newEntry(p Plugin, manifest Manifest, cfg InstanceConfig, journal RuntimeJournal) *runtimeEntry {
	cfg.Configuration = append(json.RawMessage(nil), cfg.Configuration...)
	refs := map[string]string{}
	for k, v := range cfg.SecretRefs {
		refs[k] = v
	}
	cfg.SecretRefs = refs
	configRaw, _ := json.Marshal(cfg)
	configHash := sha256.Sum256(configRaw)
	idle := make(chan struct{})
	close(idle)
	integrity := ""
	if manifest.Runtime != nil {
		integrity = manifest.Runtime.Integrity
	}

	timeout := cfg.Timeout
	if cfg.CallTimeout != "" {
		timeout, _ = time.ParseDuration(cfg.CallTimeout)
	}
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	return &runtimeEntry{persistGate: make(chan struct{}, 1), timeout: timeout, drainGate: make(chan struct{}, 1), raw: p, manifest: manifest, config: cfg, status: InstanceStatus{Manifest: copyManifest(manifest), Identity: Provenance{PackageID: manifest.ID, InstanceID: cfg.ID, Version: manifest.Version, Integrity: integrity, ConfigurationID: hex.EncodeToString(configHash[:])}, State: StateInstalled, Required: cfg.Required}, idle: idle, journal: journal}
}
func (e *runtimeEntry) snapshot() InstanceStatus {
	e.mu.Lock()
	defer e.mu.Unlock()
	s := e.status
	s.Manifest = copyManifest(s.Manifest)
	if s.ProviderFailure != nil {
		v := *s.ProviderFailure
		s.ProviderFailure = &v
	}
	s.Active = e.active
	if s.LastHealth != nil {
		v := *s.LastHealth
		s.LastHealth = &v
	}
	if s.LastSuccess != nil {
		v := *s.LastSuccess
		s.LastSuccess = &v
	}
	if s.LastFailure != nil {
		v := *s.LastFailure
		s.LastFailure = &v
	}
	return s
}
func (e *runtimeEntry) persist(ctx context.Context) error { return e.commitStatus(ctx, false) }

// Drain must not wait behind an invocation's unfinished status transaction.
// The writer coalesces a concurrent lifecycle change before releasing its gate.
func (e *runtimeEntry) tryPersist(ctx context.Context) error { return e.commitStatus(ctx, true) }
func (e *runtimeEntry) commitStatus(ctx context.Context, nonblocking bool) error {
	if e.journal == nil {
		return nil
	}
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if nonblocking {
		select {
		case e.persistGate <- struct{}{}:
		default:
			return nil
		}
	} else {
		select {
		case e.persistGate <- struct{}{}:
		case <-persistCtx.Done():
			return persistCtx.Err()
		}
	}
	defer func() { <-e.persistGate }()
	status := e.snapshot()
	if err := e.journal.SavePluginStatus(persistCtx, status); err != nil {
		return err
	}
	latest := e.snapshot()
	if latest.State != status.State || latest.Failure != status.Failure {
		return e.journal.SavePluginStatus(persistCtx, latest)
	}
	return nil
}

func (e *runtimeEntry) setState(state InstanceState) {
	e.mu.Lock()
	e.status.State = state
	e.mu.Unlock()
}

// run bounds waiting and concurrency, including providers that ignore context.
// A timed-out provider retains its slot until it actually exits. Go cannot kill
// an in-process goroutine; shutdown therefore fails closed while calls remain.
func run[T any](ctx context.Context, e *runtimeEntry, mode invokeMode, call func(context.Context) (T, error)) (T, error) {
	var zero T
	if err := ctx.Err(); err != nil {
		return zero, contextFailure(err)
	}
	e.mu.Lock()
	if (mode == modeInvoke && e.status.State != StateReady && e.status.State != StateDegraded) || (mode == modeProbe && e.status.State != StateReady && e.status.State != StateDegraded && e.status.State != StateUnavailable) {
		e.mu.Unlock()
		return zero, &RuntimeError{Code: FailureUnavailable}
	}
	if e.active >= 16 {
		e.mu.Unlock()
		return zero, &RuntimeError{Code: FailureBusy}
	}
	if e.active == 0 {
		e.idle = make(chan struct{})
	}
	e.active++
	timeout := e.timeout
	e.mu.Unlock()
	var completed atomic.Int32
	finish := func() {
		if completed.Add(1) == 2 {
			e.mu.Lock()
			e.active--
			if e.active == 0 {
				close(e.idle)
			}
			e.mu.Unlock()
		}
	}
	defer finish()
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	type result struct {
		value T
		err   error
	}
	done := make(chan result, 1)
	go func() {
		var r result
		defer func() {
			if recover() != nil {
				r = result{err: &RuntimeError{Code: FailureInternal}}
			}
			finish()
			done <- r
		}()
		r.value, r.err = call(callCtx)
		if r.err != nil {
			r.err = invocationFailure(r.err)
		}
	}()
	var r result
	select {
	case r = <-done:
		if callCtx.Err() != nil {
			r = result{err: contextFailure(callCtx.Err())}
		}
	case <-callCtx.Done():
		r.err = contextFailure(callCtx.Err())
	}
	// Provider errors have already been converted inside the bounded callback;
	// custom error methods must never run on the core caller's goroutine.

	now := time.Now().UTC()
	e.mu.Lock()
	if r.err != nil {
		e.status.LastFailure = &now
		var safe *RuntimeError
		if errors.As(r.err, &safe) {
			e.status.Failure = safe.Code
			if safe.Provider != nil {
				v := SafeFailureDetails(*safe.Provider)
				e.status.ProviderFailure = &v
			} else {
				e.status.ProviderFailure = nil
			}
		} else {
			e.status.Failure = FailureInternal
		}
		if e.status.State == StateReady {
			e.status.State = StateDegraded
		}
	} else {
		e.status.LastSuccess = &now
		if mode != modeProbe && e.status.State == StateDegraded {
			e.status.State = StateReady
		}
	}
	e.mu.Unlock()
	if err := e.persist(ctx); err != nil {
		return zero, &RuntimeError{Code: FailureInternal}
	}
	return r.value, r.err
}
func contextFailure(err error) error {
	if errors.Is(err, context.Canceled) {
		return &RuntimeError{Code: FailureCanceled}
	}
	return &RuntimeError{Code: FailureTimeout}
}

func (m *Manager) InstanceStatuses() []InstanceStatus {
	m.mu.RLock()
	entries := make([]*runtimeEntry, 0, len(m.instances))
	for _, e := range m.instances {
		entries = append(entries, e)
	}
	m.mu.RUnlock()
	out := make([]InstanceStatus, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.snapshot())
	}
	sortStatuses(out)
	return out
}

// Drain never removes an instance or swaps its code while work is in flight.
func (m *Manager) Drain(ctx context.Context, id string) error {
	m.mu.RLock()
	e := m.instances[id]
	m.mu.RUnlock()
	if e == nil {
		return ErrNotFound
	}
	ctx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()
	if ctx.Err() != nil {
		return contextFailure(ctx.Err())
	}
	select {
	case e.drainGate <- struct{}{}:
		defer func() { <-e.drainGate }()
	case <-ctx.Done():
		return contextFailure(ctx.Err())
	}
	e.mu.Lock()
	if e.status.State == StateStopped {
		e.mu.Unlock()
		return nil
	}
	e.status.State = StateDraining
	idle := e.idle
	e.mu.Unlock()
	if err := e.tryPersist(ctx); err != nil {
		return &RuntimeError{Code: FailureInternal}
	}
	select {
	case <-idle:
	case <-ctx.Done():
		now := time.Now().UTC()
		e.mu.Lock()
		e.status.LastFailure = &now
		e.status.Failure = FailureTimeout
		e.mu.Unlock()
		_ = e.tryPersist(ctx)
		return contextFailure(ctx.Err())
	}
	if hook, ok := e.raw.(Drainer); ok {
		if _, err := run(ctx, e, modeLifecycle, func(ctx context.Context) (struct{}, error) { return struct{}{}, hook.Drain(ctx) }); err != nil {
			return err
		}
	}
	if hook, ok := e.raw.(Shutdowner); ok {
		if _, err := run(ctx, e, modeLifecycle, func(ctx context.Context) (struct{}, error) { return struct{}{}, hook.Shutdown(ctx) }); err != nil {
			return err
		}
	}
	e.setState(StateStopped)
	return e.persist(ctx)
}

func safeFailureCode(code FailureCode) FailureCode {
	switch code {
	case FailureUnavailable, FailureIncompatible, FailureInvalidResponse, FailureTimeout, FailureCanceled, FailureInternal, FailureUnsupported, FailureInvalidRequest, FailureBusy:
		return code
	}
	return FailureInternal
}
func (e *RuntimeError) ProviderFailure() FailureDetails {
	if e.Provider != nil {
		return SafeFailureDetails(*e.Provider)
	}
	code := "provider_failure"
	switch e.Code {
	case FailureTimeout:
		code = "timeout"
	case FailureCanceled:
		code = "canceled"
	case FailureUnavailable, FailureBusy:
		code = "unavailable"
	}
	return FailureDetails{Code: code, Retryable: code == "unavailable" || code == "timeout"}
}

func ValidateInstanceStatus(s InstanceStatus) error {
	if s.Manifest.ID != s.Identity.PackageID || s.Manifest.Version != s.Identity.Version {
		return errors.New("plugin status manifest does not match its identity")
	}
	if !runtimeID.MatchString(s.Identity.InstanceID) || !runtimeID.MatchString(s.Identity.PackageID) || s.Active < 0 || s.Active > 16 {
		return errors.New("invalid plugin runtime identity")
	}
	switch s.State {
	case StateDiscovered, StateInstalled, StateDisabled, StateConfiguring, StateReady, StateDegraded, StateUnavailable, StateIncompatible, StateFailed, StateDraining, StateStopped:
	default:
		return errors.New("invalid plugin runtime state")
	}
	if s.ProviderFailure != nil && SafeFailureDetails(*s.ProviderFailure) != *s.ProviderFailure {
		return errors.New("invalid provider runtime diagnostic")
	}
	if s.Failure != "" && safeFailureCode(s.Failure) != s.Failure {
		return errors.New("invalid plugin runtime diagnostic")
	}
	return nil
}
