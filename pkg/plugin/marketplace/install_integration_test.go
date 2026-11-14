package marketplace

import (
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/consize-oss/consize/pkg/plugins/prometheus"
)

func TestDownloadAndRunPackagedPrometheus(t *testing.T) {
	dir := t.TempDir()
	dir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	binaryPath := filepath.Join(dir, "built-plugin")
	build := exec.Command("go", "build", "-o", binaryPath, "../../../cmd/plugins/prometheus-metrics")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build packaged plugin: %v: %s", err, output)
	}
	binary, err := os.ReadFile(binaryPath)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(binary) }))
	defer server.Close()
	// Trust only the fixture TLS server for this test; production never skips TLS verification.
	transport := http.DefaultTransport.(*http.Transport).Clone()
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	transport.TLSClientConfig = server.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	transport.TLSClientConfig.RootCAs = roots
	old := http.DefaultTransport
	http.DefaultTransport = transport
	defer func() { http.DefaultTransport = old; transport.CloseIdleConnections() }()
	r := release(binary)
	r.Manifest = prometheus.NewWithClient(nil, prometheus.Config{}).Manifest()
	r.URL = server.URL
	envelope, key := signed(t, r, time.Now().Add(time.Hour))
	root := filepath.Join(dir, "installed")
	installed, err := Install(context.Background(), root, envelope, key, r.Manifest.ID, r.Manifest.Version)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Install(context.Background(), root, envelope, key, r.Manifest.ID, r.Manifest.Version); err == nil {
		t.Fatal("installed version overwritten")
	}
	keyPath := filepath.Join(dir, "publisher.pub")
	if err = os.WriteFile(keyPath, []byte(base64.StdEncoding.EncodeToString(key)), 0600); err != nil {
		t.Fatal(err)
	}
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"success","data":{"resultType":"matrix","result":[]}}`))
	}))
	defer endpoint.Close()
	cfg, _ := json.Marshal(map[string]string{"base_url": endpoint.URL, "window": "30m", "step": "30s"})
	remote, err := Load(ExternalConfig{Directory: installed, PublicKeyPath: keyPath, Config: cfg})
	if err != nil {
		t.Fatal(err)
	}
	if !remote.executionEnvelope {
		t.Fatal("packaged plugin envelope support was not negotiated")
	}
	if h := remote.Health(context.Background()); h.Status != "healthy" {
		t.Fatalf("packaged health: %+v", h)
	}
	// A checksum failure must stop a later call before the executable runs.
	if err = os.WriteFile(filepath.Join(installed, "plugin"), []byte("tampered"), 0700); err != nil {
		t.Fatal(err)
	}
	if h := remote.Health(context.Background()); h.Status != "unhealthy" {
		t.Fatal("tampered plugin executed")
	}
}
