package discovery

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/consize-oss/consize/internal/bootstrap"
	"github.com/consize-oss/consize/internal/store"
	"github.com/consize-oss/consize/pkg/plugin"
	"github.com/consize-oss/consize/pkg/plugins/kubernetes"
	"github.com/consize-oss/consize/pkg/resource"
)

type Scheduler struct {
	st                      store.DiscoveryRunStore
	plugins                 *plugin.Manager
	cfg                     bootstrap.DiscoveryScheduleConfig
	interval, poll, timeout time.Duration
	owner                   string
	now                     func() time.Time
}

var ErrProviderScope = errors.New("configured scoped discovery provider unavailable")

func NewScheduler(st store.DiscoveryRunStore, plugins *plugin.Manager, c bootstrap.DiscoveryScheduleConfig) (*Scheduler, error) {
	if c.MaxAttempts == 0 {
		c.MaxAttempts = 4
	}
	if c.MaxRunsPerTick == 0 {
		c.MaxRunsPerTick = 10
	}
	if c.MaxAttempts < 1 || c.MaxAttempts > 10 || c.MaxRunsPerTick < 1 || c.MaxRunsPerTick > 100 {
		return nil, errors.New("invalid discovery worker bounds")
	}
	parse := func(raw, def string, min, max time.Duration) (time.Duration, error) {
		if raw == "" {
			raw = def
		}
		d, err := time.ParseDuration(raw)
		if err != nil || d < min || d > max || d%time.Millisecond != 0 {
			return 0, errors.New("invalid discovery schedule duration")
		}
		return d, nil
	}
	interval, err := parse(c.Interval, "5m", time.Second, 24*time.Hour)
	if err != nil {
		return nil, err
	}
	poll, err := parse(c.PollInterval, "5s", time.Second, time.Minute)
	if err != nil {
		return nil, err
	}
	timeout, err := parse(c.Timeout, "60s", time.Second, 4*time.Minute)
	if err != nil {
		return nil, err
	}
	if c.StaleAfter == "" {
		c.StaleAfter = "30m"
	}
	if c.TombstoneAfter == "" {
		c.TombstoneAfter = "24h"
	}
	stale, err := parse(c.StaleAfter, "30m", 0, 365*24*time.Hour)
	if err != nil {
		return nil, err
	}
	tomb, err := parse(c.TombstoneAfter, "24h", time.Millisecond, 365*24*time.Hour)
	if err != nil || tomb <= stale {
		return nil, errors.New("tombstone_after must exceed stale_after")
	}
	b := make([]byte, 16)
	if _, err = rand.Read(b); err != nil {
		return nil, err
	}
	out := &Scheduler{st: st, plugins: plugins, cfg: c, interval: interval, poll: poll, timeout: timeout, owner: hex.EncodeToString(b), now: time.Now}
	for _, p := range out.scoped() {
		if err := store.ValidateDiscoverySelection(p.DiscoveryScope()); err != nil {
			return nil, err
		}
	}
	if c.Enabled && len(out.scoped()) == 0 {
		return nil, ErrProviderScope
	}
	return out, nil
}
func (s *Scheduler) scoped() []plugin.ScopedDiscoveryPlugin {
	out := []plugin.ScopedDiscoveryPlugin{}
	for _, p := range s.plugins.DiscoveryPlugins() {
		if scoped, ok := p.(plugin.ScopedDiscoveryPlugin); ok {
			out = append(out, scoped)
		}
	}
	return out
}
func (s *Scheduler) provider(id string) (plugin.ScopedDiscoveryPlugin, error) {
	for _, p := range s.scoped() {
		if p.ID() == id {
			return p, nil
		}
	}
	return nil, ErrProviderScope
}
func discoveryJitter(key string, bound time.Duration) time.Duration {
	if bound <= 0 {
		return 0
	}
	sum := sha256.Sum256([]byte(key))
	return time.Duration(binary.BigEndian.Uint64(sum[:8]) % uint64(bound))
}
func (s *Scheduler) request(ctx context.Context, p plugin.ScopedDiscoveryPlugin, actor, trigger string, at, due time.Time) (store.DiscoveryRun, error) {
	return s.st.EnqueueDiscovery(ctx, store.DiscoveryRun{PluginID: p.ID(), Scope: p.DiscoveryScope(), RequestedAt: at, Actor: actor, Trigger: trigger, MaxAttempts: s.cfg.MaxAttempts, StaleAfter: s.cfg.StaleAfter, TombstoneAfter: s.cfg.TombstoneAfter, NextRun: due})
}
func (s *Scheduler) Request(ctx context.Context, id, actor string) (store.DiscoveryRun, error) {
	p, err := s.provider(id)
	if err != nil {
		return store.DiscoveryRun{}, err
	}
	return s.request(ctx, p, actor, "manual", s.now().UTC().Truncate(s.interval), time.Time{})
}
func (s *Scheduler) Tick(ctx context.Context) error {
	if s.cfg.Enabled {
		end := s.now().UTC().Truncate(s.interval)
		jitter := s.interval / 5
		if jitter > 30*time.Second {
			jitter = 30 * time.Second
		}
		for _, p := range s.scoped() {
			_, key := store.DiscoveryScopeIdentity(p.ID(), p.DiscoveryScope())
			_, err := s.request(ctx, p, "discovery-scheduler", "scheduled", end, end.Add(discoveryJitter(key+end.String(), jitter)))
			if err != nil && !errors.Is(err, store.ErrDiscoveryBusy) {
				return err
			}
		}
	}
	jobs, err := s.st.ListDiscoveryRuns(ctx, store.DiscoveryRunQuery{Due: true, Limit: s.cfg.MaxRunsPerTick})
	if err != nil {
		return err
	}
	for _, job := range jobs {
		if err = ctx.Err(); err != nil {
			return err
		}
		if _, err = s.execute(ctx, job.ID); err != nil && !errors.Is(err, store.ErrJobBusy) && !errors.Is(err, store.ErrJobChanged) {
			return err
		}
	}
	return nil
}
func (s *Scheduler) Run(ctx context.Context) error {
	ticker := time.NewTicker(s.poll)
	defer ticker.Stop()
	for {
		if err := s.Tick(ctx); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}
func (s *Scheduler) execute(ctx context.Context, id string) (store.DiscoveryRun, error) {
	job, err := s.st.ClaimDiscovery(ctx, id, s.owner, s.timeout+5*time.Second)
	if err != nil {
		return store.DiscoveryRun{}, err
	}
	if store.DiscoveryTerminal(job.State) {
		return job, nil
	}
	completion := s.read(ctx, job)
	if ctx.Err() != nil {
		return store.DiscoveryRun{}, ctx.Err()
	}
	out, err := s.st.CompleteDiscovery(ctx, job, completion)
	if errors.Is(err, store.ErrDiscoveryInvalid) {
		return s.st.CompleteDiscovery(ctx, job, store.DiscoveryCompletion{ErrorCode: "registry_conflict"})
	}
	if errors.Is(err, store.ErrLeaseLost) {
		return store.DiscoveryRun{}, nil
	}
	return out, err
}
func (s *Scheduler) read(ctx context.Context, job store.DiscoveryRun) store.DiscoveryCompletion {
	fail := func(code string) store.DiscoveryCompletion { return store.DiscoveryCompletion{ErrorCode: code} }
	p, err := s.provider(job.PluginID)
	if err != nil {
		return fail("scope_changed")
	}
	_, key := store.DiscoveryScopeIdentity(p.ID(), p.DiscoveryScope())
	if key != job.ScopeKey {
		return fail("scope_changed")
	}
	callCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	snapshot, err := p.DiscoverSnapshot(callCtx)
	if err != nil {
		code, retry := classifyDiscoveryError(err)
		result := fail(code)
		if retry {
			delay := 5 * time.Second * time.Duration(1<<uint(job.Attempts-1))
			if delay > 5*time.Minute {
				delay = 5 * time.Minute
			}
			var access *kubernetes.ScanError
			if errors.As(err, &access) && access.RetryAfterSeconds > 0 {
				hint := time.Duration(access.RetryAfterSeconds) * time.Second
				if access.RetryAfterSeconds > 300 {
					hint = 5 * time.Minute
				}
				if hint > delay {
					delay = hint
				}
			}
			result.RetryAt = s.now().UTC().Add(delay + discoveryJitter(fmt.Sprintf("%s/%d", job.ID, job.Attempts), delay/5))
		}
		return result
	}
	if validateScan(snapshot, p.ID(), s.now().UTC()) != nil {
		return fail("incomplete_scan")
	}
	_, actual := store.DiscoveryScopeIdentity(p.ID(), snapshot.Scan.Scope)
	if actual != job.ScopeKey {
		return fail("scope_changed")
	}
	normalized := make([]resource.Resource, 0, len(snapshot.Observations))
	for _, observation := range snapshot.Observations {
		r, err := normalize(p, observation, s.now().UTC())
		if err != nil {
			return fail("invalid_observation")
		}
		normalized = append(normalized, r)
	}
	return store.DiscoveryCompletion{Scan: &snapshot.Scan, Resources: normalized}
}
func classifyDiscoveryError(err error) (string, bool) {
	var scan *kubernetes.ScanError
	if errors.As(err, &scan) {
		switch scan.Code {
		case "unauthorized":
			return "authentication", false
		case "forbidden":
			return "authorization", false
		case "configuration", "unsupported_client":
			return "configuration", false
		case "tls", "network", "timeout", "rate_limited", "server":
			return scan.Code, kubernetes.ScanRetryable(scan.Code)
		case "request", "redirect_blocked":
			return "request", false
		case "canceled":
			return "canceled", false
		case "unavailable", "scan_busy":
			return "unavailable", true
		default:
			return "incomplete_scan", false
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout", true
	}
	if errors.Is(err, context.Canceled) {
		return "canceled", false
	}
	return "provider_failure", false
}
