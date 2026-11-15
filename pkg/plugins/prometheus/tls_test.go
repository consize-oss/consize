package prometheus

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writePEM(t *testing.T, name, kind string, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: kind, Bytes: data}), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}
func TestTLSAndBasicAuthentication(t *testing.T) {
	t.Setenv("TRANSPORT_USER", "operator")
	t.Setenv("TRANSPORT_PASSWORD", "private-password-test")
	s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, password, ok := r.BasicAuth()
		if !ok || user != "operator" || password != "private-password-test" {
			t.Error("basic authentication missing")
		}
		w.Header().Set("Content-Type", "application/json")
		matrixResponse(w)
	}))
	defer s.Close()
	p, _ := NewConfiguredHTTPClient(s.URL, TransportConfig{})
	_, err := rangeRead(p)
	requireCode(t, err, "tls")
	ca := writePEM(t, "ca.pem", "CERTIFICATE", s.Certificate().Raw)
	p, err = NewConfiguredHTTPClient(s.URL, TransportConfig{TLS: TLSConfig{CAFile: ca}, Auth: AuthConfig{UsernameEnv: "TRANSPORT_USER", PasswordEnv: "TRANSPORT_PASSWORD"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = rangeRead(p); err != nil {
		t.Fatal(err)
	}
	p, err = NewConfiguredHTTPClient(s.URL, TransportConfig{TLS: TLSConfig{CAFile: ca, ServerName: "wrong.example.test"}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = rangeRead(p)
	requireCode(t, err, "tls")
	if _, err = NewConfiguredHTTPClient(s.URL, TransportConfig{TLS: TLSConfig{CAFile: writePEM(t, "bad.pem", "NOT CERTIFICATE", []byte("invalid"))}}); err == nil {
		t.Fatal("invalid CA accepted")
	}
}
func TestMutualTLS(t *testing.T) {
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	caTemplate := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test-ca"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	caCertificate, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}
	clientKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	clientTemplate := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "test-client"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}
	cert, err := x509.CreateCertificate(rand.Reader, clientTemplate, caCertificate, &clientKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	key, err := x509.MarshalPKCS8PrivateKey(clientKey)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(caCertificate)
	s := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(r.TLS.PeerCertificates) == 0 {
			t.Error("missing client identity")
		}
		w.Header().Set("Content-Type", "application/json")
		matrixResponse(w)
	}))
	s.TLS = &tls.Config{MinVersion: tls.VersionTLS12, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: pool}
	s.StartTLS()
	defer s.Close()
	root := writePEM(t, "server.pem", "CERTIFICATE", s.Certificate().Raw)
	p, err := NewConfiguredHTTPClient(s.URL, TransportConfig{TLS: TLSConfig{CAFile: root, CertFile: writePEM(t, "client.pem", "CERTIFICATE", cert), KeyFile: writePEM(t, "client.key", "PRIVATE KEY", key)}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = rangeRead(p); err != nil {
		t.Fatal(err)
	}
	p, _ = NewConfiguredHTTPClient(s.URL, TransportConfig{TLS: TLSConfig{CAFile: root}})
	_, err = p.QueryInstant(context.Background(), "1", time.Now())
	requireCode(t, err, "tls")
}
