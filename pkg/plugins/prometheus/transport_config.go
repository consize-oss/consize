package prometheus

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// TransportConfig stores references, never credential values. Secrets are resolved
// once when constructing a client; reconstruct it to rotate credentials.
type TransportConfig struct {
	Auth             AuthConfig        `json:"auth,omitempty"`
	TLS              TLSConfig         `json:"tls,omitempty"`
	HeaderEnv        map[string]string `json:"header_env,omitempty"`
	RequestTimeout   string            `json:"request_timeout,omitempty"`
	QueryTimeout     string            `json:"query_timeout,omitempty"`
	MaxResponseBytes int64             `json:"max_response_bytes,omitempty"`
	MaxSeries        int               `json:"max_series,omitempty"`
	MaxSamples       int               `json:"max_samples,omitempty"`
	RetryAttempts    int               `json:"retry_attempts,omitempty"`
	RetryBackoff     string            `json:"retry_backoff,omitempty"`
}
type AuthConfig struct {
	BearerTokenEnv  string `json:"bearer_token_env,omitempty"`
	BearerTokenFile string `json:"bearer_token_file,omitempty"`
	UsernameEnv     string `json:"username_env,omitempty"`
	PasswordEnv     string `json:"password_env,omitempty"`
	PasswordFile    string `json:"password_file,omitempty"`
}
type TLSConfig struct {
	CAFile     string `json:"ca_file,omitempty"`
	CertFile   string `json:"cert_file,omitempty"`
	KeyFile    string `json:"key_file,omitempty"`
	ServerName string `json:"server_name,omitempty"`
}
type limits struct {
	request, query, backoff   time.Duration
	bytes                     int64
	series, samples, attempts int
}

func resolveLimits(c TransportConfig) (limits, error) {
	l := limits{request: 10 * time.Second, query: 8 * time.Second, backoff: 250 * time.Millisecond, bytes: 4 << 20, series: 1024, samples: 100000, attempts: 1}
	var err error
	if c.RequestTimeout != "" {
		l.request, err = time.ParseDuration(c.RequestTimeout)
		if err != nil {
			return l, failure("configuration")
		}
	}
	if c.QueryTimeout != "" {
		l.query, err = time.ParseDuration(c.QueryTimeout)
		if err != nil {
			return l, failure("configuration")
		}
	} else if l.query > l.request {
		l.query = l.request
	}
	if c.RetryBackoff != "" {
		l.backoff, err = time.ParseDuration(c.RetryBackoff)
		if err != nil {
			return l, failure("configuration")
		}
	}
	if c.MaxResponseBytes != 0 {
		l.bytes = c.MaxResponseBytes
	}
	if c.MaxSeries != 0 {
		l.series = c.MaxSeries
	}
	if c.MaxSamples != 0 {
		l.samples = c.MaxSamples
	}
	if c.RetryAttempts != 0 {
		l.attempts = c.RetryAttempts
	}
	if l.request <= 0 || l.request > 2*time.Minute || l.query <= 0 || l.query > l.request || l.backoff < time.Millisecond || l.backoff > 5*time.Second || l.bytes < 1024 || l.bytes > 16<<20 || l.series < 1 || l.series > 4096 || l.samples < 1 || l.samples > 1000000 || l.attempts < 1 || l.attempts > 3 {
		return l, failure("configuration")
	}
	return l, nil
}
func validateBase(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u == nil || len(raw) > 2048 || strings.TrimSpace(raw) != raw || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawFragment != "" || u.RawPath != "" || u.Opaque != "" || strings.Contains(u.Hostname(), "%") {
		return nil, failure("configuration")
	}
	if port := u.Port(); port != "" {
		n, e := strconv.Atoi(port)
		if e != nil || n < 1 || n > 65535 {
			return nil, failure("configuration")
		}
	}
	for _, part := range strings.Split(u.Path, "/") {
		if part == "." || part == ".." {
			return nil, failure("configuration")
		}
	}
	if ip := net.ParseIP(u.Hostname()); ip != nil && !permittedIP(ip) {
		return nil, failure("endpoint_blocked")
	}
	u.Path = strings.TrimRight(u.Path, "/")
	return u, nil
}
func permittedIP(ip net.IP) bool {
	return (ip.IsGlobalUnicast() || ip.IsLoopback()) && !ip.IsLinkLocalUnicast() && !ip.Equal(net.ParseIP("100.100.100.200")) && !ip.Equal(net.ParseIP("fd00:ec2::254")) && !ip.Equal(net.ParseIP("168.63.129.16"))
}
func isLoopbackHost(host string) bool {
	ip := net.ParseIP(host)
	return strings.EqualFold(host, "localhost") || ip != nil && ip.IsLoopback()
}
func secret(env, file string) (string, error) {
	if env != "" && file != "" {
		return "", failure("configuration")
	}
	var value string
	if env != "" {
		value = os.Getenv(env)
	} else if file != "" {
		b, err := readBoundedFile(file, 65536)
		if err != nil {
			return "", failure("configuration")
		}
		value = strings.TrimRight(string(b), "\r\n")
	}
	if (env != "" || file != "") && (value == "" || len(value) > 65536 || strings.ContainsAny(value, "\r\n\x00")) {
		return "", failure("configuration")
	}
	return value, nil
}
func readBoundedFile(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, failure("configuration")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > limit {
		return nil, failure("configuration")
	}
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(b)) > limit {
		return nil, failure("configuration")
	}
	return b, nil
}
func transportFor(u *url.URL, c TransportConfig) (*http.Client, http.Header, limits, error) {
	l, err := resolveLimits(c)
	if err != nil {
		return nil, nil, l, err
	}
	headers := http.Header{}
	bearer, err := secret(c.Auth.BearerTokenEnv, c.Auth.BearerTokenFile)
	if err != nil {
		return nil, nil, l, err
	}
	user, err := secret(c.Auth.UsernameEnv, "")
	if err != nil {
		return nil, nil, l, err
	}
	password, err := secret(c.Auth.PasswordEnv, c.Auth.PasswordFile)
	if err != nil {
		return nil, nil, l, err
	}
	basic := c.Auth.UsernameEnv != "" || c.Auth.PasswordEnv != "" || c.Auth.PasswordFile != ""
	if basic && (user == "" || password == "" || bearer != "") {
		return nil, nil, l, failure("configuration")
	}
	if len(c.HeaderEnv) > 16 {
		return nil, nil, l, failure("configuration")
	}
	for name, env := range c.HeaderEnv {
		if !validHeaderName(name) || env == "" {
			return nil, nil, l, failure("configuration")
		}
		value, e := secret(env, "")
		if e != nil || len(value) > 8192 {
			return nil, nil, l, failure("configuration")
		}
		if _, exists := headers[http.CanonicalHeaderKey(name)]; exists {
			return nil, nil, l, failure("configuration")
		}
		for _, c := range value {
			if c < 32 || c == 127 {
				return nil, nil, l, failure("configuration")
			}
		}
		headers.Set(name, value)
	}
	if u.Scheme != "https" && !isLoopbackHost(u.Hostname()) && (basic || bearer != "" || len(headers) > 0 || c.TLS.CertFile != "") {
		return nil, nil, l, failure("configuration")
	}
	if bearer != "" {
		headers.Set("Authorization", "Bearer "+bearer)
	}
	if basic {
		r := http.Request{Header: headers}
		r.SetBasicAuth(user, password)
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: c.TLS.ServerName}
	if u.Scheme != "https" && (c.TLS.CAFile != "" || c.TLS.CertFile != "" || c.TLS.KeyFile != "" || c.TLS.ServerName != "") {
		return nil, nil, l, failure("configuration")
	}
	if c.TLS.CAFile != "" {
		pem, e := readBoundedFile(c.TLS.CAFile, 2<<20)
		if e != nil {
			return nil, nil, l, e
		}
		pool, e := x509.SystemCertPool()
		if e != nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(pem) {
			return nil, nil, l, failure("configuration")
		}
		tlsConfig.RootCAs = pool
	}
	if (c.TLS.CertFile == "") != (c.TLS.KeyFile == "") {
		return nil, nil, l, failure("configuration")
	}
	if c.TLS.CertFile != "" {
		cert, e := readBoundedFile(c.TLS.CertFile, 2<<20)
		if e != nil {
			return nil, nil, l, e
		}
		key, e := readBoundedFile(c.TLS.KeyFile, 2<<20)
		if e != nil {
			return nil, nil, l, e
		}
		pair, e := tls.X509KeyPair(cert, key)
		if e != nil {
			return nil, nil, l, failure("configuration")
		}
		tlsConfig.Certificates = []tls.Certificate{pair}
	}
	tr := &http.Transport{TLSClientConfig: tlsConfig, ForceAttemptHTTP2: true, DisableCompression: true, MaxConnsPerHost: 8, MaxIdleConns: 8, MaxIdleConnsPerHost: 8, IdleConnTimeout: 30 * time.Second, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: l.request, MaxResponseHeaderBytes: 32 << 10}
	dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
	tr.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, e := net.SplitHostPort(address)
		if e != nil {
			return nil, failure("endpoint_blocked")
		}
		ips, e := net.DefaultResolver.LookupIPAddr(ctx, host)
		if e != nil {
			return nil, e
		}
		if len(ips) == 0 || len(ips) > 64 {
			return nil, failure("endpoint_blocked")
		}
		for _, ip := range ips {
			if !permittedIP(ip.IP) || u.Scheme == "http" && len(headers) > 0 && !ip.IP.IsLoopback() {
				return nil, failure("endpoint_blocked")
			}
		}
		var last error
		for _, ip := range ips {
			conn, e := dialer.DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
			if e == nil {
				return conn, nil
			}
			last = e
			if ctx.Err() != nil {
				break
			}
		}
		return nil, last
	}
	return &http.Client{Transport: tr, Timeout: l.request, CheckRedirect: func(*http.Request, []*http.Request) error { return failure("redirect_blocked") }}, headers, l, nil
}
func validHeaderName(name string) bool {
	lower := strings.ToLower(name)
	switch lower {
	case "authorization", "proxy-authorization", "host", "cookie", "connection", "transfer-encoding", "content-length", "content-type", "accept-encoding", "accept", "trailer", "te", "upgrade":
		return false
	}
	if len(name) == 0 || len(name) > 128 {
		return false
	}
	for _, c := range name {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("!#$%&'*+-.^_`|~", c)) {
			return false
		}
	}
	return true
}
