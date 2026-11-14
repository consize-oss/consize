package marketplace

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"reflect"
	"time"

	"github.com/consize-oss/consize/pkg/plugin"
	"github.com/consize-oss/consize/pkg/resource"
)

type Request struct {
	Protocol int                `json:"protocol"`
	Method   string             `json:"method"`
	Config   json.RawMessage    `json:"config"`
	Input    plugin.ActionInput `json:"input"`
	Plan     plugin.ActionPlan  `json:"plan"`
	Resource resource.Resource  `json:"resource"`
	Start    time.Time          `json:"start"`
	End      time.Time          `json:"end"`
}
type Response struct {
	Preflight []plugin.PreflightCheck `json:"preflight,omitempty"`
	Protocol  int                     `json:"protocol"`
	Result    json.RawMessage         `json:"result,omitempty"`
	Error     string                  `json:"error,omitempty"`
}
type ExternalConfig struct {
	Directory     string          `json:"directory"`
	PublicKeyPath string          `json:"public_key_path"`
	Config        json.RawMessage `json:"config"`
}

type remote struct {
	executionEnvelope bool
	digest            string
	directory         string
	key               ed25519.PublicKey
	manifest          plugin.Manifest
	config            json.RawMessage
}

func Load(cfg ExternalConfig) (*remote, error) {
	key, err := PublicKey(cfg.PublicKeyPath)
	if err != nil {
		return nil, err
	}
	r, _, err := Installed(cfg.Directory, key)
	if err != nil {
		return nil, err
	}
	p := &remote{directory: cfg.Directory, key: key, manifest: r.Manifest, config: cfg.Config, digest: r.SHA256}
	var handshake struct {
		ExecutionEnvelope bool            `json:"execution_envelope"`
		Preflight         bool            `json:"preflight"`
		Manifest          plugin.Manifest `json:"manifest"`
		Recovery          bool            `json:"recovery"`
		Windowed          bool            `json:"windowed"`
	}
	if err = p.call(context.Background(), Request{Method: "handshake"}, &handshake); err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(handshake.Manifest, p.manifest) || (p.manifest.Category == plugin.CategoryAction && (!handshake.Recovery || !handshake.Preflight)) || (p.manifest.Category == plugin.CategoryMetrics && !handshake.Windowed) {
		return nil, errors.New("plugin handshake does not match signed contract")
	}
	p.executionEnvelope = handshake.ExecutionEnvelope
	return p, nil
}
func Register(manager *plugin.Manager, cfg ExternalConfig) error {
	p, err := Load(cfg)
	if err != nil {
		return err
	}
	if p.manifest.Category == plugin.CategoryAction {
		return manager.RegisterAction(&Action{p})
	}
	return manager.RegisterMetrics(&Metrics{p})
}

type limitedBuffer struct {
	bytes.Buffer
	limit int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.Len() {
		return 0, errors.New("plugin output exceeds limit")
	}
	return b.Buffer.Write(p)
}

// Protocol v1 originally had no execution envelope. Negotiate this additive field
// before sending it to an older executable whose decoder rejects unknown fields.
func (p *remote) prepareRequest(req Request) Request {
	req.Protocol, req.Config = Protocol, p.config
	if !p.executionEnvelope {
		req.Plan.Execution = nil
	}
	return req
}

func (p *remote) call(ctx context.Context, req Request, out any) error {
	r, path, err := Installed(p.directory, p.key)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(r.Manifest, p.manifest) || r.SHA256 != p.digest {
		return errors.New("installed release changed; restart with explicit configuration")
	}
	req = p.prepareRequest(req)
	b, err := json.Marshal(req)
	if err != nil {
		return err
	}
	if len(b) > 8<<20 {
		return errors.New("plugin request too large")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path)
	cmd.WaitDelay = 2 * time.Second
	cmd.Stdin = bytes.NewReader(b)
	// Credentials are passed explicitly through config; do not inherit API process secrets.
	cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + os.Getenv("HOME"), "TMPDIR=" + os.TempDir()}
	stdout := &limitedBuffer{limit: 16 << 20}
	stderr := &limitedBuffer{limit: 64 << 10}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err = cmd.Run(); err != nil {
		return fmt.Errorf("plugin %s %s failed: %w", p.manifest.ID, req.Method, err)
	}
	var response Response
	dec := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
	if err = dec.Decode(&response); err != nil {
		return fmt.Errorf("invalid plugin response: %w", err)
	}
	var extra any
	if err = dec.Decode(&extra); err != io.EOF {
		return errors.New("plugin wrote multiple responses")
	}
	if response.Protocol != Protocol {
		return errors.New("plugin response protocol mismatch")
	}
	if response.Error != "" {
		if len(response.Preflight) > 0 {
			return &plugin.PreflightError{Checks: response.Preflight}
		}
		return fmt.Errorf("plugin %s: %s", p.manifest.ID, response.Error)
	}
	if out == nil {
		return nil
	}
	dec = json.NewDecoder(bytes.NewReader(response.Result))
	dec.UseNumber()
	return dec.Decode(out)
}
func (p *remote) ID() string                { return p.manifest.ID }
func (p *remote) ArtifactDigest() string    { return p.digest }
func (p *remote) Manifest() plugin.Manifest { return p.manifest }
func (p *remote) Health(ctx context.Context) plugin.Health {
	var h plugin.Health
	if err := p.call(ctx, Request{Method: "health"}, &h); err != nil {
		return plugin.Health{Status: "unhealthy", Message: err.Error(), CheckedAt: time.Now().UTC()}
	}
	return h
}

type Action struct{ *remote }

func (p *Action) Preflight(ctx context.Context, plan plugin.ActionPlan) ([]plugin.PreflightCheck, error) {
	var out []plugin.PreflightCheck
	err := p.call(ctx, Request{Method: "preflight", Plan: plan}, &out)
	return out, err
}

func (p *Action) Plan(ctx context.Context, input plugin.ActionInput) (plugin.ActionPlan, error) {
	var out plugin.ActionPlan
	err := p.call(ctx, Request{Method: "plan", Input: input}, &out)
	return out, err
}
func (p *Action) Execute(ctx context.Context, plan plugin.ActionPlan) (plugin.ActionResult, error) {
	var out plugin.ActionResult
	err := p.call(ctx, Request{Method: "execute", Plan: plan}, &out)
	return out, err
}
func (p *Action) Inspect(ctx context.Context, plan plugin.ActionPlan) (string, error) {
	var out string
	err := p.call(ctx, Request{Method: "inspect", Plan: plan}, &out)
	return out, err
}
func (p *Action) Rollback(ctx context.Context, plan plugin.ActionPlan) (plugin.ActionResult, error) {
	var out plugin.ActionResult
	err := p.call(ctx, Request{Method: "rollback", Plan: plan}, &out)
	return out, err
}
func (p *Action) Ready(ctx context.Context, plan plugin.ActionPlan) error {
	return p.call(ctx, Request{Method: "ready", Plan: plan}, nil)
}

type Metrics struct{ *remote }

func (p *Metrics) ReadMetrics(ctx context.Context, res resource.Resource) (plugin.MetricsSnapshot, error) {
	var out plugin.MetricsSnapshot
	err := p.call(ctx, Request{Method: "metrics", Resource: res}, &out)
	return out, err
}
func (p *Metrics) ReadMetricsBetween(ctx context.Context, res resource.Resource, start, end time.Time) (plugin.MetricsSnapshot, error) {
	var out plugin.MetricsSnapshot
	err := p.call(ctx, Request{Method: "metrics_between", Resource: res, Start: start, End: end}, &out)
	return out, err
}

// ServeOnce is the SDK entry point for packaged plugins. Logs belong on stderr.
func ServeOnce(ctx context.Context, in io.Reader, out io.Writer, factory func(json.RawMessage) (plugin.Plugin, error)) error {
	var req Request
	b, err := io.ReadAll(io.LimitReader(in, (8<<20)+1))
	if err != nil {
		return err
	}
	if len(b) > 8<<20 {
		return errors.New("plugin request too large")
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	dec.DisallowUnknownFields()
	if err = dec.Decode(&req); err != nil {
		return err
	}
	var extra any
	if err = dec.Decode(&extra); err != io.EOF {
		return errors.New("expected one request")
	}
	var result any
	if req.Protocol != Protocol {
		err = errors.New("unsupported protocol")
	} else {
		var p plugin.Plugin
		p, err = factory(req.Config)
		if err == nil {
			action, actionOK := p.(plugin.RecoverableActionPlugin)
			preflight, preflightOK := p.(plugin.PreflightActionPlugin)
			metrics, metricsOK := p.(plugin.WindowedMetricsPlugin)
			switch req.Method {
			case "handshake":
				result = struct {
					ExecutionEnvelope bool            `json:"execution_envelope"`
					Preflight         bool            `json:"preflight"`
					Manifest          plugin.Manifest `json:"manifest"`
					Recovery          bool            `json:"recovery"`
					Windowed          bool            `json:"windowed"`
				}{true, preflightOK, p.Manifest(), actionOK, metricsOK}
			case "preflight":
				if preflightOK {
					result, err = preflight.Preflight(ctx, req.Plan)
				} else {
					err = errors.New("live preflight contract required")
				}
			case "health":
				result = p.Health(ctx)
			case "plan":
				if actionOK {
					result, err = action.Plan(ctx, req.Input)
				} else {
					err = errors.New("recovery action contract required")
				}
			case "execute":
				if actionOK {
					result, err = action.Execute(ctx, req.Plan)
				} else {
					err = errors.New("unsupported execute")
				}
			case "inspect":
				if actionOK {
					result, err = action.Inspect(ctx, req.Plan)
				} else {
					err = errors.New("unsupported inspect")
				}
			case "rollback":
				if actionOK {
					result, err = action.Rollback(ctx, req.Plan)
				} else {
					err = errors.New("unsupported rollback")
				}
			case "ready":
				if actionOK {
					err = action.Ready(ctx, req.Plan)
				} else {
					err = errors.New("unsupported readiness")
				}
			case "metrics":
				if metricsOK {
					result, err = metrics.ReadMetrics(ctx, req.Resource)
				} else {
					err = errors.New("windowed metrics contract required")
				}
			case "metrics_between":
				if metricsOK {
					result, err = metrics.ReadMetricsBetween(ctx, req.Resource, req.Start, req.End)
				} else {
					err = errors.New("unsupported windowed metrics")
				}
			default:
				err = errors.New("unknown plugin method")
			}
		}
	}
	response := Response{Protocol: Protocol}
	if err != nil {
		response.Error = err.Error()
		var blocked *plugin.PreflightError
		if errors.As(err, &blocked) {
			response.Preflight = blocked.Checks
		}
	} else {
		response.Result, err = json.Marshal(result)
		if err != nil {
			return err
		}
	}
	return json.NewEncoder(out).Encode(response)
}
