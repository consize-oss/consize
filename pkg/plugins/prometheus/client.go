package prometheus

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Series struct {
	Metric map[string]string `json:"metric"`
	Points []Point           `json:"points"`
}
type Point struct {
	Timestamp time.Time `json:"timestamp"`
	Value     float64   `json:"value"`
}
type StringPoint struct {
	Timestamp time.Time `json:"timestamp"`
	Value     string    `json:"value"`
}
type Result struct {
	ResultType string       `json:"result_type"`
	Series     []Series     `json:"series,omitempty"`
	Scalar     *Point       `json:"scalar,omitempty"`
	Text       *StringPoint `json:"text,omitempty"`
	Warnings   []Annotation `json:"warnings"`
	Infos      []Annotation `json:"infos"`
}
type Client interface {
	QueryRange(context.Context, string, time.Time, time.Time, time.Duration) ([]Series, error)
}
type HTTPClient struct {
	baseURL     *url.URL
	client      *http.Client
	headers     http.Header
	limits      limits
	diagnostics diagnosticState
}

func NewConfiguredHTTPClient(baseURL string, c TransportConfig) (*HTTPClient, error) {
	u, err := validateBase(baseURL)
	if err != nil {
		return nil, err
	}
	client, headers, l, err := transportFor(u, c)
	if err != nil {
		return nil, err
	}
	return &HTTPClient{baseURL: u, client: client, headers: headers, limits: l}, nil
}

// NewHTTPClient preserves the injection hook for trusted Go callers and tests.
// Production configuration uses NewConfiguredHTTPClient's validated transport.
func NewHTTPClient(baseURL string, client *http.Client) (*HTTPClient, error) {
	p, err := NewConfiguredHTTPClient(baseURL, TransportConfig{})
	if err != nil {
		return nil, err
	}
	if client != nil {
		copy := *client
		copy.CheckRedirect = func(*http.Request, []*http.Request) error { return failure("redirect_blocked") }
		if copy.Timeout <= 0 || copy.Timeout > p.limits.request {
			copy.Timeout = p.limits.request
		}
		p.client = &copy
	}
	return p, nil
}
func (p *HTTPClient) Diagnostics() Diagnostics { return p.diagnostics.snapshot() }
func (p *HTTPClient) QueryRange(ctx context.Context, query string, start, end time.Time, step time.Duration) ([]Series, error) {
	result, err := p.QueryRangeResult(ctx, query, start, end, step)
	if err != nil {
		return nil, err
	}
	if len(result.Warnings) > 0 {
		return nil, failure("partial_response")
	}
	return result.Series, nil
}
func (p *HTTPClient) QueryRangeResult(ctx context.Context, query string, start, end time.Time, step time.Duration) (Result, error) {
	if !validQuery(query) || start.IsZero() || end.IsZero() || start.Year() < 1 || end.Year() > 9999 || end.Before(start) || end.Sub(start) > 365*24*time.Hour || step < time.Millisecond || step > 24*time.Hour || int64(end.Sub(start)/step)+1 > int64(p.limits.samples) {
		return Result{}, failure("configuration")
	}
	// Reuse PR #79's millisecond bounds contained inside the caller's interval.
	queryStart := start.Truncate(time.Millisecond)
	if queryStart.Before(start) {
		queryStart = queryStart.Add(time.Millisecond)
	}
	queryEnd := end.Truncate(time.Millisecond)
	if queryEnd.Before(queryStart) {
		return Result{}, failure("configuration")
	}
	values := url.Values{"query": {query}, "start": {queryStart.UTC().Format(time.RFC3339Nano)}, "end": {queryEnd.UTC().Format(time.RFC3339Nano)}, "step": {strconv.FormatFloat(step.Seconds(), 'f', -1, 64)}, "timeout": {strconv.FormatFloat(p.limits.query.Seconds(), 'f', -1, 64)}}
	return p.query(ctx, "/api/v1/query_range", values, "matrix", queryStart, queryEnd)
}
func (p *HTTPClient) QueryInstant(ctx context.Context, query string, at time.Time) (Result, error) {
	if !validQuery(query) || at.IsZero() || at.Year() < 1 || at.Year() > 9999 {
		return Result{}, failure("configuration")
	}
	values := url.Values{"query": {query}, "time": {at.UTC().Format(time.RFC3339Nano)}, "timeout": {strconv.FormatFloat(p.limits.query.Seconds(), 'f', -1, 64)}}
	return p.query(ctx, "/api/v1/query", values, "", time.Time{}, time.Time{})
}
func validQuery(query string) bool {
	return strings.TrimSpace(query) != "" && len(query) <= 8192 && !strings.ContainsRune(query, '\x00')
}
func (p *HTTPClient) Ready(ctx context.Context) error {
	_, err := p.query(ctx, "/-/ready", nil, "ready", time.Time{}, time.Time{})
	return err
}
func (p *HTTPClient) query(ctx context.Context, path string, values url.Values, expected string, start, end time.Time) (result Result, err error) {
	sequence, _ := p.diagnostics.begin()
	attempts := 0
	defer func() { p.diagnostics.finish(sequence, result, err, attempts) }()
	ctx, cancel := context.WithTimeout(ctx, p.limits.request)
	defer cancel()
	for attempts < p.limits.attempts {
		attempts++
		result, err = p.once(ctx, path, values, expected, start, end)
		if err == nil {
			return result, nil
		}
		classified := safeError(err)
		err = classified
		if !classified.Retryable || attempts >= p.limits.attempts || ctx.Err() != nil {
			return Result{}, err
		}
		delay := p.limits.backoff * time.Duration(1<<(attempts-1))
		if suggested := time.Duration(classified.RetryAfterSeconds) * time.Second; suggested > delay {
			delay = suggested
		}
		if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) <= delay {
			return Result{}, err
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return Result{}, safeError(ctx.Err())
		case <-timer.C:
		}
	}
	return Result{}, err
}
func (p *HTTPClient) once(ctx context.Context, path string, values url.Values, expected string, start, end time.Time) (Result, error) {
	u := *p.baseURL
	u.Path += path
	u.RawQuery = values.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return Result{}, failure("configuration")
	}
	request.Header = p.headers.Clone()
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Accept-Encoding", "identity")
	response, err := p.client.Do(request)
	if err != nil {
		return Result{}, safeError(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		classified := statusError(response.StatusCode)
		if response.StatusCode == 429 || response.StatusCode == 503 {
			classified.RetryAfterSeconds = retryAfter(response.Header.Get("Retry-After"))
		}
		if expected == "ready" && response.StatusCode == 503 {
			classified.Code = "not_ready"
		}
		if expected != "ready" && response.StatusCode == 503 {
			// Error bodies are neither logged nor returned. Inspect only the documented
			// error discriminator under a small bound; preserve safe HTTP classification
			// when a proxy returns HTML, truncated or oversized error content.
			body, readErr := io.ReadAll(io.LimitReader(response.Body, 4097))
			if ctx.Err() != nil {
				return Result{}, safeError(ctx.Err())
			}
			var e struct {
				Status string `json:"status"`
				Type   string `json:"errorType"`
			}
			if readErr == nil && len(body) <= 4096 && json.Unmarshal(body, &e) == nil && e.Status == "error" {
				switch e.Type {
				case "timeout":
					classified.Code = "timeout"
				case "canceled":
					classified.Code = "query_canceled"
					classified.Retryable = false
				}
			}
		}
		return Result{}, classified
	}
	if encoding := response.Header.Get("Content-Encoding"); encoding != "" && encoding != "identity" {
		return Result{}, failure("unsupported_encoding")
	}
	if response.ContentLength > p.limits.bytes {
		return Result{}, failure("response_too_large")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, p.limits.bytes+1))
	if err != nil {
		return Result{}, safeError(err)
	}
	if int64(len(body)) > p.limits.bytes {
		return Result{}, failure("response_too_large")
	}
	if expected == "ready" {
		return Result{}, nil
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return Result{}, failure("invalid_response")
	}
	return parseResult(body, expected, p.limits, start, end)
}

func retryAfter(value string) int {
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil && seconds > 0 {
		if seconds > 86400 {
			return 86400
		}
		return int(seconds)
	}
	if at, err := http.ParseTime(value); err == nil {
		seconds := int(time.Until(at).Seconds()) + 1
		if seconds > 86400 {
			return 86400
		}
		if seconds > 0 {
			return seconds
		}
	}
	return 0
}
