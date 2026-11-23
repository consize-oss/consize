// Package collection owns durable read-only evidence operations. It never
// dispatches infrastructure actions or evaluates recommendation policies.
package collection

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/consize-oss/consize/internal/bootstrap"
	"github.com/consize-oss/consize/internal/store"
	"github.com/consize-oss/consize/pkg/evidence"
	"github.com/consize-oss/consize/pkg/evidence/quality"
	"github.com/consize-oss/consize/pkg/plugin"
	"github.com/consize-oss/consize/pkg/resource"
)

type Config = bootstrap.CollectionConfig

// Settings are resolved by the API composition root to avoid a bootstrap cycle.
type Settings struct {
	Config         Config
	ProviderConfig any
	Quality        quality.Config
}
type Registry interface {
	GetResource(context.Context, string) (resource.Resource, error)
	ListResources(context.Context) ([]resource.Resource, error)
}
type Collector struct {
	st                                         store.CollectionStore
	registry                                   Registry
	plugins                                    *plugin.Manager
	cfg                                        Config
	interval, window, poll, timeout, retention time.Duration
	digest, owner                              string
	thresholds                                 quality.Thresholds
	now                                        func() time.Time
}

var ErrInvalidRequest = errors.New("invalid collection request")
var ErrNotConfigured = errors.New("collection provider is not configured")

var required = []string{"cpu_cores_p95", "memory_working_set_bytes_p95", "cpu_request_cores_current", "cpu_limit_cores_current", "memory_request_bytes_current", "memory_limit_bytes_current", "restarts_30m_p95", "cpu_throttling_ratio_p95", "oom_events_30m_p95", "evicted_pods_p95"}

func New(st store.CollectionStore, registry Registry, plugins *plugin.Manager, s Settings) (*Collector, error) {
	c := s.Config
	if c.MaxAttempts == 0 {
		c.MaxAttempts = 4
	}
	if c.MaxJobsPerTick == 0 {
		c.MaxJobsPerTick = 10
	}
	if c.MaxAttempts < 1 || c.MaxAttempts > 10 || c.MaxJobsPerTick < 1 || c.MaxJobsPerTick > 100 {
		return nil, errors.New("invalid collection worker bounds")
	}
	parse := func(raw, def string, min, max time.Duration) (time.Duration, error) {
		if raw == "" {
			raw = def
		}
		d, e := time.ParseDuration(raw)
		if e != nil || d < min || d > max || d%time.Millisecond != 0 {
			return 0, errors.New("invalid collection duration")
		}
		return d, nil
	}
	var err error
	out := &Collector{st: st, registry: registry, plugins: plugins, cfg: c, now: time.Now}
	out.interval, err = parse(c.Interval, "15m", time.Second, 24*time.Hour)
	if err != nil {
		return nil, err
	}
	out.window, err = parse(c.Window, "24h", time.Millisecond, 365*24*time.Hour)
	if err != nil {
		return nil, err
	}
	out.poll, err = parse(c.PollInterval, "5s", time.Second, time.Minute)
	if err != nil {
		return nil, err
	}
	out.timeout, err = parse(c.Timeout, "30s", time.Second, 4*time.Minute)
	if err != nil {
		return nil, err
	}
	out.retention, err = parse(c.Retention, "0s", 0, 365*24*time.Hour)
	if err != nil {
		return nil, err
	}
	if out.retention > 0 && out.retention < 24*time.Hour {
		return nil, errors.New("collection retention must be zero or at least 24h")
	}
	out.thresholds, err = quality.Resolve(s.Quality, quality.Reporting, false, false)
	if err != nil {
		return nil, err
	}
	b := make([]byte, 16)
	if _, err = rand.Read(b); err != nil {
		return nil, err
	}
	out.owner = hex.EncodeToString(b)
	if c.PluginID == "" {
		if c.Enabled {
			return nil, errors.New("collection.plugin_id is required when scheduling is enabled")
		}
		return out, nil
	}
	provider, err := plugins.MetricsPlugin(c.PluginID, resource.TypeKubernetesDeployment)
	if err != nil {
		if !c.Enabled {
			out.cfg.PluginID = ""
			return out, nil
		}
		return nil, errors.New("configured collection provider unavailable")
	}
	if _, ok := provider.(plugin.WindowedMetricsPlugin); !ok {
		if !c.Enabled {
			out.cfg.PluginID = ""
			return out, nil
		}
		return nil, errors.New("collection provider must support fixed windows")
	}
	identity, err := plugins.InstanceIdentity(c.PluginID)
	if err != nil {
		return nil, errors.New("collection instance identity unavailable")
	}
	raw, err := json.Marshal([]any{"collection-instance-v2", identity, provider.Manifest().DataVersion, c.PluginID, provider.Manifest().Version, s.ProviderConfig})
	if err != nil {
		return nil, errors.New("invalid collection provider configuration")
	}
	sum := sha256.Sum256(raw)
	out.digest = hex.EncodeToString(sum[:])
	return out, nil
}
func eligible(r resource.Resource) bool {
	return r.Type == resource.TypeKubernetesDeployment && r.LifecycleState == resource.LifecycleActive && r.SupportStatus == resource.SupportFull && r.Validate() == nil
}
func (c *Collector) Request(ctx context.Context, id, actor string, end time.Time) (store.Collection, error) {
	if c.cfg.PluginID == "" {
		return store.Collection{}, ErrNotConfigured
	}
	res, err := c.registry.GetResource(ctx, id)
	if err != nil {
		return store.Collection{}, err
	}
	if !eligible(res) {
		return store.Collection{}, ErrInvalidRequest
	}
	if end.IsZero() {
		end = c.now().UTC().Truncate(c.interval)
	}
	if end.After(c.now().UTC()) || end.Nanosecond()%1000000 != 0 || end.Add(-c.window).Year() < 1 {
		return store.Collection{}, ErrInvalidRequest
	}
	return c.enqueue(ctx, res, actor, "manual", end, time.Time{})
}
func (c *Collector) enqueue(ctx context.Context, r resource.Resource, actor, trigger string, end, due time.Time) (store.Collection, error) {
	keys := append([]string(nil), required...)
	sort.Strings(keys)
	return c.st.EnqueueCollection(ctx, store.Collection{Resource: r, PluginID: c.cfg.PluginID, InstanceDigest: c.digest, Signals: keys, WindowStart: end.Add(-c.window), WindowEnd: end, Actor: actor, Trigger: trigger, MaxAttempts: c.cfg.MaxAttempts, NextRun: due})
}
func stableJitter(key string, bound time.Duration) time.Duration {
	if bound <= 0 {
		return 0
	}
	sum := sha256.Sum256([]byte(key))
	return time.Duration(binary.BigEndian.Uint64(sum[:8]) % uint64(bound))
}
func (c *Collector) schedule(ctx context.Context, now time.Time) error {
	if !c.cfg.Enabled {
		return nil
	}
	resources, err := c.registry.ListResources(ctx)
	if err != nil {
		return err
	}
	end := now.UTC().Truncate(c.interval)
	jitter := c.interval / 5
	if jitter > 30*time.Second {
		jitter = 30 * time.Second
	}
	for _, r := range resources {
		if !eligible(r) {
			continue
		}
		due := end.Add(stableJitter(r.ID+c.digest+end.Format(time.RFC3339Nano), jitter))
		_, err = c.enqueue(ctx, r, "evidence-scheduler", "scheduled", end, due)
		if err != nil && !errors.Is(err, store.ErrCollectionOverlap) {
			return err
		}
	}
	return nil
}
func (c *Collector) Tick(ctx context.Context) error {
	now := c.now().UTC()
	if err := c.schedule(ctx, now); err != nil {
		return err
	}
	jobs, err := c.st.ListCollections(ctx, store.CollectionQuery{Due: true, Limit: c.cfg.MaxJobsPerTick})
	if err != nil {
		return err
	}
	for _, job := range jobs {
		if err = ctx.Err(); err != nil {
			return err
		}
		if err = c.execute(ctx, job); err != nil {
			return err
		}
	}
	if c.retention > 0 {
		_, err = c.st.PruneCollections(ctx, now.Add(-c.retention))
	}
	return err
}
func (c *Collector) Run(ctx context.Context) error {
	ticker := time.NewTicker(c.poll)
	defer ticker.Stop()
	for {
		if err := c.Tick(ctx); err != nil {
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
func (c *Collector) execute(ctx context.Context, job store.Collection) error {
	claimed, err := c.st.ClaimCollection(ctx, job.ID, c.owner, c.timeout+5*time.Second)
	if errors.Is(err, store.ErrJobBusy) || errors.Is(err, store.ErrJobChanged) {
		return nil
	}
	if err != nil {
		return err
	}
	if store.CollectionTerminal(claimed.State) {
		return nil
	}
	result := c.read(ctx, claimed)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	_, err = c.st.FinishCollection(ctx, claimed, result)
	if errors.Is(err, store.ErrLeaseLost) {
		return nil
	}
	return err
}
func classify(err error) (string, bool) {
	var runtimeFailure *plugin.RuntimeError
	if errors.As(err, &runtimeFailure) && runtimeFailure.Code == plugin.FailureInvalidResponse {
		return "invalid_evidence", false
	}
	var upstream plugin.ProviderFailure
	if errors.As(err, &upstream) {
		p := plugin.SafeFailureDetails(upstream.ProviderFailure())
		switch p.Code {
		case "authentication", "authorization":
			return "provider_access", false
		case "query":
			return "provider_query", false
		case "configuration", "unit_configuration", "tls":
			return "provider_configuration", false
		case "timeout":
			return "provider_timeout", true
		default:
			if p.Retryable {
				return "provider_unavailable", true
			}
			return "provider_failure", false
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "provider_timeout", true
	}
	return "provider_failure", false
}

func (c *Collector) read(ctx context.Context, job store.Collection) store.CollectionResult {
	identity := plugin.InvocationFrom(ctx)
	identity.OperationID = job.ID
	identity.WorkflowID = job.ID
	identity.Actor = job.Actor
	ctx = plugin.WithInvocation(ctx, identity)
	fail := func(code string) store.CollectionResult { return store.CollectionResult{ErrorCode: code} }
	if job.InstanceDigest != c.digest || job.PluginID != c.cfg.PluginID {
		return fail("instance_changed")
	}
	res, err := c.registry.GetResource(ctx, job.Resource.ID)
	if err != nil || !eligible(res) || !resource.SameIdentity(res, job.Resource) || store.ResourceCollectionScope(res) != store.ResourceCollectionScope(job.Resource) {
		return fail("resource_unavailable")
	}
	provider, err := c.plugins.MetricsPlugin(job.PluginID, job.Resource.Type)
	windowed, ok := provider.(plugin.WindowedMetricsPlugin)
	if err != nil || !ok {
		return fail("provider_configuration")
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	snapshot, err := windowed.ReadMetricsBetween(callCtx, job.Resource, job.WindowStart, job.WindowEnd)
	if err != nil {
		code, retry := classify(err)
		result := fail(code)
		if retry {
			result.RetryAt = c.now().UTC().Add(retryDelay(job, err))
		}
		return result
	}
	if !validSnapshot(snapshot, job) {
		return fail("invalid_evidence")
	}
	assessment := snapshot.AssessQuality([]quality.Signal{{Name: "cpu_cores_p95", Unit: "cores"}, {Name: "memory_working_set_bytes_p95", Unit: "bytes"}, {Name: "restarts_30m_p95", Unit: "count"}, {Name: "cpu_throttling_ratio_p95", Unit: "ratio"}, {Name: "oom_events_30m_p95", Unit: "count"}, {Name: "evicted_pods_p95", Unit: "count"}}, quality.Reporting, c.thresholds, c.now().UTC())
	records := map[string]evidence.Record{}
	for _, r := range snapshot.Records {
		records[r.Signal] = r
	}
	for _, key := range job.Signals {
		r, exists := records[key]
		if !exists || r.Value == nil || r.Quality != evidence.Valid {
			assessment.Eligible = false
			if assessment.State == "valid" {
				assessment.State = "partial"
			}
			assessment.Dimensions = append(assessment.Dimensions, quality.Dimension{Name: "collection_completeness", Signal: key, Status: "fail", Mandatory: true, Code: "incomplete_collection", Message: "A required collection signal is missing or unusable."})
		}
	}
	return store.CollectionResult{Snapshot: &snapshot, Assessment: &assessment}
}
func retryDelay(job store.Collection, err error) time.Duration {
	delay := 5 * time.Second * time.Duration(1<<uint(job.Attempts-1))
	if delay > 5*time.Minute {
		delay = 5 * time.Minute
	}
	var providerFailure plugin.ProviderFailure
	if errors.As(err, &providerFailure) && providerFailure.ProviderFailure().RetryAfterSeconds > 0 {
		upstream := plugin.SafeFailureDetails(providerFailure.ProviderFailure())
		hint := time.Duration(upstream.RetryAfterSeconds) * time.Second
		if upstream.RetryAfterSeconds > 300 {
			hint = 5 * time.Minute
		}
		if hint > delay {
			delay = hint
		}
	}
	return delay + stableJitter(fmt.Sprintf("%s/%d", job.ID, job.Attempts), delay/5)
}
func validSnapshot(s plugin.MetricsSnapshot, job store.Collection) bool {
	if s.ValidateRecords() != nil || s.ResourceID != job.Resource.ID || s.PluginID != job.PluginID {
		return false
	}
	raw, err := json.Marshal(s)
	if err != nil || len(raw) > 4<<20 {
		return false
	}
	units := map[string]string{"cpu_cores_p95": "cores", "memory_working_set_bytes_p95": "bytes", "cpu_request_cores_current": "cores", "cpu_limit_cores_current": "cores", "memory_request_bytes_current": "bytes", "memory_limit_bytes_current": "bytes", "restarts_30m_p95": "count", "cpu_throttling_ratio_p95": "ratio", "oom_events_30m_p95": "count", "evicted_pods_p95": "count"}
	for _, r := range s.Records {
		if !r.WindowStart.Equal(job.WindowStart) || !r.WindowEnd.Equal(job.WindowEnd) {
			return false
		}
		if expected := units[r.Signal]; expected != "" {
			aggregation := "p95"
			if strings.HasSuffix(r.Signal, "_current") {
				aggregation = "last"
			}
			if r.Value != nil && (*r.Value < 0 || r.Signal == "cpu_throttling_ratio_p95" && *r.Value > 1) {
				return false
			}
			if strings.HasSuffix(r.Signal, "_current") && r.Quality == evidence.Valid && r.SampleCount != 1 {
				return false
			}
			if r.Unit != expected || r.Aggregation != aggregation {
				return false
			}
		}
		projected, exists := s.Signals[r.Signal]
		if r.Value != nil {
			if !exists || projected == nil {
				return false
			}
			encoded, e := json.Marshal(projected)
			var v float64
			if e != nil || json.Unmarshal(encoded, &v) != nil || v != *r.Value {
				return false
			}
		} else if exists && projected != nil {
			return false
		}
	}
	return true
}
