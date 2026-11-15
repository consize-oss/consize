package prometheus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func serveJSON(t *testing.T, handler func(http.ResponseWriter, *http.Request)) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		handler(w, r)
	}))
	t.Cleanup(s.Close)
	return s
}
func matrixResponse(w http.ResponseWriter) {
	fmt.Fprint(w, `{"status":"success","data":{"resultType":"matrix","result":[{"metric":{"job":"test"},"values":[[1700000000.123,"0"],[1700000001.123,"2"]]}]}}`)
}
func rangeRead(p *HTTPClient) (Result, error) {
	start := time.UnixMilli(1700000000123)
	return p.QueryRangeResult(context.Background(), "test_query", start, start.Add(time.Second), time.Second)
}
func requireCode(t *testing.T, err error, code string) {
	t.Helper()
	var provider *Error
	if !errors.As(err, &provider) || provider.Code != code {
		t.Fatalf("got %v want code %s", err, code)
	}
}
func TestConfiguredTransportValidation(t *testing.T) {
	for _, raw := range []string{"", "://bad", "file:///etc/passwd", "http://user:pass@example.test", "https://example.test?token=private", "https://example.test#secret", "https://example.test/../secret", "https://example.test/%2e%2e/secret", "http://0.0.0.0", "http://169.254.169.254", "http://[fd00:ec2::254]", "http://100.100.100.200", "https://example.test:0", "https://example.test:65536"} {
		if _, err := NewConfiguredHTTPClient(raw, TransportConfig{}); err == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
	for _, cfg := range []TransportConfig{{RequestTimeout: "bad"}, {RequestTimeout: "0s"}, {RequestTimeout: "3m"}, {QueryTimeout: "20s"}, {MaxResponseBytes: 1}, {MaxSeries: -1}, {MaxSamples: 1000001}, {RetryAttempts: 4}, {RetryBackoff: "0s"}, {Auth: AuthConfig{BearerTokenEnv: "MISSING_TRANSPORT_TOKEN"}}, {HeaderEnv: map[string]string{"Authorization": "ANY"}}, {TLS: TLSConfig{KeyFile: "missing"}}} {
		if _, err := NewConfiguredHTTPClient("https://example.test", cfg); err == nil {
			t.Fatalf("accepted invalid configuration %+v", cfg)
		}
	}
	t.Setenv("TRANSPORT_TOKEN", "a-private-test-token")
	if _, err := NewConfiguredHTTPClient("http://monitoring.example.test", TransportConfig{Auth: AuthConfig{BearerTokenEnv: "TRANSPORT_TOKEN"}}); err == nil {
		t.Fatal("remote plaintext credentials accepted")
	}
	if _, err := NewConfiguredHTTPClient("https://example.test", TransportConfig{Auth: AuthConfig{BearerTokenEnv: "TRANSPORT_TOKEN", BearerTokenFile: "file"}}); err == nil {
		t.Fatal("ambiguous credentials accepted")
	}
	t.Setenv("TRANSPORT_HEADER", "tenant-test")
	if _, err := NewConfiguredHTTPClient("https://example.test", TransportConfig{HeaderEnv: map[string]string{"X-Tenant": "TRANSPORT_HEADER", "x-tenant": "TRANSPORT_HEADER"}}); err == nil {
		t.Fatal("duplicate canonical headers accepted")
	}
}
func TestAuthenticationHeadersAndSafeErrors(t *testing.T) {
	const credential = "secret-auth-value-for-test"
	t.Setenv("TRANSPORT_TOKEN", credential)
	t.Setenv("TRANSPORT_TENANT", "tenant-a")
	s := serveJSON(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+credential || r.Header.Get("X-Scope-OrgID") != "tenant-a" {
			t.Error("missing authentication or tenant header")
		}
		http.Error(w, credential+" private-provider-body "+r.URL.String(), 401)
	})
	p, err := NewConfiguredHTTPClient(s.URL, TransportConfig{Auth: AuthConfig{BearerTokenEnv: "TRANSPORT_TOKEN"}, HeaderEnv: map[string]string{"X-Scope-OrgID": "TRANSPORT_TENANT"}, RetryAttempts: 3})
	if err != nil {
		t.Fatal(err)
	}
	_, err = rangeRead(p)
	requireCode(t, err, "authentication")
	encoded, _ := json.Marshal(p.Diagnostics())
	for _, output := range []string{err.Error(), fmt.Sprintf("%+v", err), string(encoded)} {
		for _, secret := range []string{credential, "private-provider-body", s.URL, "test_query", "tenant-a"} {
			if strings.Contains(output, secret) {
				t.Fatal("provider detail leaked")
			}
		}
	}
	if p.Diagnostics().Attempts != 1 || p.Diagnostics().State != "unavailable" {
		t.Fatal("authentication was retried or marked clean")
	}
	file := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(file, []byte(credential+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	p, err = NewConfiguredHTTPClient(s.URL, TransportConfig{Auth: AuthConfig{BearerTokenFile: file}, HeaderEnv: map[string]string{"X-Scope-OrgID": "TRANSPORT_TENANT"}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = rangeRead(p)
	requireCode(t, err, "authentication")
}
func TestResultTypesLimitsAndWarnings(t *testing.T) {
	cases := []struct{ name, body, code string }{
		{"valid", `{"status":"success","data":{"resultType":"matrix","result":[]}}`, ""},
		{"wrong type", `{"status":"success","data":{"resultType":"vector","result":[]}}`, "invalid_result_type"},
		{"malformed", `{"status":`, "invalid_response"},
		{"trailing JSON", `{"status":"success","data":{"resultType":"matrix","result":[]}} {}`, "invalid_response"},
		{"null", `{"status":"success","data":{"resultType":"matrix","result":null}}`, "invalid_response"},
		{"NaN", `{"status":"success","data":{"resultType":"matrix","result":[{"metric":{},"values":[[1700000000.123,"NaN"]]}]}}`, "invalid_sample"},
		{"Infinity", `{"status":"success","data":{"resultType":"matrix","result":[{"metric":{},"values":[[1700000000.123,"+Inf"]]}]}}`, "invalid_sample"},
		{"bad sample", `{"status":"success","data":{"resultType":"matrix","result":[{"metric":{},"values":[[1700000000.123,"1",3]]}]}}`, "invalid_sample"},
		{"histogram", `{"status":"success","data":{"resultType":"matrix","result":[{"metric":{},"histograms":[]} ]}}`, "unsupported_sample"},
		{"query error", `{"status":"error","errorType":"bad_data","error":"provider private query"}`, "query"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := serveJSON(t, func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, tc.body) })
			p, _ := NewConfiguredHTTPClient(s.URL, TransportConfig{})
			_, err := rangeRead(p)
			if tc.code == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else {
				requireCode(t, err, tc.code)
			}
		})
	}
	t.Run("oversized", func(t *testing.T) {
		s := serveJSON(t, func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, strings.Repeat("x", 2048)) })
		p, _ := NewConfiguredHTTPClient(s.URL, TransportConfig{MaxResponseBytes: 1024})
		_, err := rangeRead(p)
		requireCode(t, err, "response_too_large")
	})
	t.Run("sample limit", func(t *testing.T) {
		s := serveJSON(t, func(w http.ResponseWriter, _ *http.Request) { matrixResponse(w) })
		p, _ := NewConfiguredHTTPClient(s.URL, TransportConfig{MaxSamples: 1})
		_, err := p.QueryInstant(context.Background(), "test", time.Now())
		requireCode(t, err, "response_limit")
	})
	t.Run("warnings", func(t *testing.T) {
		s := serveJSON(t, func(w http.ResponseWriter, _ *http.Request) {
			fmt.Fprint(w, `{"status":"success","warnings":["private-query-token"],"infos":["private-info"],"data":{"resultType":"matrix","result":[]}}`)
		})
		p, _ := NewConfiguredHTTPClient(s.URL, TransportConfig{})
		result, err := rangeRead(p)
		if err != nil || len(result.Warnings) != 1 || len(result.Infos) != 1 || p.Diagnostics().State != "degraded" {
			t.Fatalf("warnings lost: %+v %v", result, err)
		}
		b, _ := json.Marshal(result)
		if strings.Contains(string(b), "private-") {
			t.Fatal("raw annotation leaked")
		}
		start := time.UnixMilli(1700000000123)
		_, err = p.QueryRange(context.Background(), "query", start, start.Add(time.Second), time.Second)
		requireCode(t, err, "partial_response")
	})
}
func TestRetriesTimeoutCancellationRedirectAndConcurrency(t *testing.T) {
	var calls atomic.Int32
	s := serveJSON(t, func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(429)
			return
		}
		matrixResponse(w)
	})
	p, _ := NewConfiguredHTTPClient(s.URL, TransportConfig{RetryAttempts: 3, RetryBackoff: "1ms"})
	if _, err := rangeRead(p); err != nil || calls.Load() != 3 || p.Diagnostics().Attempts != 3 {
		t.Fatalf("bounded retry failed: %v %+v", err, p.Diagnostics())
	}
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := rangeRead(p); err != nil {
				t.Error(err)
			}
			_ = p.Diagnostics()
		}()
	}
	wg.Wait()
	slow := serveJSON(t, func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
	p, _ = NewConfiguredHTTPClient(slow.URL, TransportConfig{RequestTimeout: "20ms"})
	_, err := rangeRead(p)
	requireCode(t, err, "timeout")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("deadline identity lost")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = p.QueryInstant(ctx, "1", time.Now())
	requireCode(t, err, "canceled")
	if !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation identity lost")
	}
	var redirected atomic.Bool
	target := serveJSON(t, func(w http.ResponseWriter, _ *http.Request) { redirected.Store(true); matrixResponse(w) })
	redirect := serveJSON(t, func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 302) })
	p, _ = NewConfiguredHTTPClient(redirect.URL, TransportConfig{})
	_, err = rangeRead(p)
	requireCode(t, err, "redirect_blocked")
	if redirected.Load() {
		t.Fatal("redirect followed")
	}
	closed := httptest.NewServer(http.NotFoundHandler())
	address := closed.URL
	closed.Close()
	p, _ = NewConfiguredHTTPClient(address, TransportConfig{})
	_, err = rangeRead(p)
	requireCode(t, err, "network")
}
func TestInstantTypesAndHealth(t *testing.T) {
	for _, tc := range []struct{ kind, result string }{{"scalar", `[1700000000.123,"1"]`}, {"string", `[1700000000.123,"value"]`}, {"vector", `[{"metric":{},"value":[1700000000.123,"1"]}]`}} {
		s := serveJSON(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/-/ready" {
				fmt.Fprint(w, "ready")
				return
			}
			fmt.Fprintf(w, `{"status":"success","data":{"resultType":%q,"result":%s}}`, tc.kind, tc.result)
		})
		p, _ := NewConfiguredHTTPClient(s.URL, TransportConfig{})
		result, err := p.QueryInstant(context.Background(), "1", time.Now())
		if err != nil || result.ResultType != tc.kind {
			t.Fatalf("%s: %+v %v", tc.kind, result, err)
		}
		plugin, err := New(Config{BaseURL: s.URL})
		if err != nil {
			t.Fatal(err)
		}
		h := plugin.Health(context.Background())
		if h.Status != "healthy" || h.Details == nil {
			t.Fatalf("health: %+v", h)
		}
	}
}

func TestHTTPFailureClassificationAndRetryPolicy(t *testing.T) {
	for _, tc := range []struct {
		status   int
		code     string
		attempts int
	}{{400, "query", 1}, {401, "authentication", 1}, {403, "authorization", 1}, {404, "http_status", 1}, {422, "query", 1}, {429, "rate_limited", 3}, {500, "server", 3}, {503, "server", 3}} {
		t.Run(fmt.Sprint(tc.status), func(t *testing.T) {
			var calls atomic.Int32
			s := serveJSON(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.WriteHeader(tc.status)
				fmt.Fprint(w, "private-upstream-details")
			})
			p, _ := NewConfiguredHTTPClient(s.URL, TransportConfig{RetryAttempts: 3, RetryBackoff: "1ms"})
			_, err := rangeRead(p)
			requireCode(t, err, tc.code)
			if calls.Load() != int32(tc.attempts) {
				t.Fatalf("retry count %d", calls.Load())
			}
		})
	}
}
func TestQueryBoundsAndProtocolValidation(t *testing.T) {
	var calls atomic.Int32
	s := serveJSON(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1); matrixResponse(w) })
	p, _ := NewConfiguredHTTPClient(s.URL, TransportConfig{})
	start := time.UnixMilli(1700000000123)
	for _, tc := range []struct {
		query      string
		start, end time.Time
		step       time.Duration
	}{{"", start, start.Add(time.Second), time.Second}, {"query", start, start.Add(-time.Second), time.Second}, {"query", start, start.Add(time.Second), 0}, {"query", start, start.Add(time.Second), time.Nanosecond}, {"query", start, start.Add(365 * 24 * time.Hour), time.Millisecond}} {
		_, err := p.QueryRangeResult(context.Background(), tc.query, tc.start, tc.end, tc.step)
		requireCode(t, err, "configuration")
	}
	if calls.Load() != 0 {
		t.Fatal("invalid range reached provider")
	}
	if _, err := New(Config{BaseURL: s.URL, Window: time.Minute, Step: time.Hour}); err == nil {
		t.Fatal("step exceeds window")
	}
	s2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { matrixResponse(w) }))
	defer s2.Close()
	p, _ = NewConfiguredHTTPClient(s2.URL, TransportConfig{})
	_, err := rangeRead(p)
	requireCode(t, err, "invalid_response")
}

func TestProviderQueryTimeoutAndReadiness(t *testing.T) {
	s := serveJSON(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(503)
		fmt.Fprint(w, `{"status":"error","errorType":"timeout","error":"private-upstream-timeout"}`)
	})
	p, _ := NewConfiguredHTTPClient(s.URL, TransportConfig{})
	_, err := rangeRead(p)
	requireCode(t, err, "timeout")
	requireCode(t, p.Ready(context.Background()), "not_ready")
	plugin, err := New(Config{BaseURL: s.URL})
	if err != nil {
		t.Fatal(err)
	}
	if health := plugin.Health(context.Background()); health.Status != "unhealthy" || strings.Contains(health.Message, "private-") {
		t.Fatalf("unsafe health: %+v", health)
	}
}

func TestRetryAfterDoesNotViolateProviderBackoff(t *testing.T) {
	var calls atomic.Int32
	s := serveJSON(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(429)
	})
	p, _ := NewConfiguredHTTPClient(s.URL, TransportConfig{RetryAttempts: 3, RequestTimeout: "1s"})
	_, err := rangeRead(p)
	requireCode(t, err, "rate_limited")
	if calls.Load() != 1 || p.Diagnostics().LatestError.RetryAfterSeconds != 120 {
		t.Fatal("ignored provider backoff or exceeded request budget")
	}
}

func TestTruncatedNetworkResponseIsNotCleanJSON(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		connection, buffer, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer connection.Close()
		fmt.Fprint(buffer, "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: 1024\r\n\r\n{}")
		if err := buffer.Flush(); err != nil {
			t.Error(err)
		}
	}))
	defer s.Close()
	p, _ := NewConfiguredHTTPClient(s.URL, TransportConfig{})
	_, err := rangeRead(p)
	requireCode(t, err, "network")
}

func TestResolvedDestinationIsValidatedBeforeSendingCredentials(t *testing.T) {
	t.Setenv("CON17_DIAL_TOKEN", "private-dial-test-token")
	p, err := NewConfiguredHTTPClient("http://localhost:9090", TransportConfig{Auth: AuthConfig{BearerTokenEnv: "CON17_DIAL_TOKEN"}})
	if err != nil {
		t.Fatal(err)
	}
	transport := p.client.Transport.(*http.Transport)
	// Literal addresses use the same post-resolution check as DNS results. These
	// calls must return before attempting a socket or transmitting any headers.
	for _, destination := range []string{"169.254.169.254:80", "10.0.0.1:80", "[fd00:ec2::254]:80", "0.0.0.0:80"} {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		connection, err := transport.DialContext(ctx, "tcp", destination)
		cancel()
		if connection != nil {
			connection.Close()
			t.Fatal("connected to blocked destination")
		}
		requireCode(t, err, "endpoint_blocked")
	}
}
