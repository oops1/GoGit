package transport

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/gitcore/progress"
)

type testAuthority struct {
	cert    *x509.Certificate
	key     *ecdsa.PrivateKey
	certPEM []byte
}

func newTestAuthority(t *testing.T) *testAuthority {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey returned error %v", err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "gogit test authority"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("CreateCertificate returned error %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("ParseCertificate returned error %v", err)
	}
	return &testAuthority{cert: cert, key: key, certPEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})}
}

func (a *testAuthority) issue(t *testing.T, serial int64, usage x509.ExtKeyUsage) (certPEM, keyPEM []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey returned error %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(serial),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{usage},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, a.cert, &key.PublicKey, a.key)
	if err != nil {
		t.Fatalf("CreateCertificate returned error %v", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("MarshalPKCS8PrivateKey returned error %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
}

func writeTestFile(t *testing.T, dir, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile returned error %v", err)
	}
	return filepath.ToSlash(path)
}

func writeServerCertificate(t *testing.T, server *httptest.Server) string {
	t.Helper()
	return writeTestFile(t, t.TempDir(), "server.pem", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}))
}

func newTLSRemote(t *testing.T, authority *testAuthority, requireClient bool) *httptest.Server {
	t.Helper()
	certPEM, keyPEM := authority.issue(t, 2, x509.ExtKeyUsageServerAuth)
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("X509KeyPair returned error %v", err)
	}
	server := httptest.NewUnstartedServer(http.HandlerFunc(advertiseV1Handler))
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.TLS = &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
	if requireClient {
		pool := x509.NewCertPool()
		pool.AddCert(authority.cert)
		server.TLS.ClientAuth = tls.RequireAndVerifyClientCert
		server.TLS.ClientCAs = pool
	}
	server.StartTLS()
	t.Cleanup(server.Close)
	return server
}

func TestHTTPTrustsTheCertificateAuthorityFromSSLCAInfo(t *testing.T) {
	clearProxyEnvironment(t)
	authority := newTestAuthority(t)
	server := newTLSRemote(t, authority, false)
	if err := advertiseThrough(t, server.URL+"/repo.git", Options{}); err == nil {
		t.Fatalf("advertise trusted an unknown certificate authority")
	}
	caFile := writeTestFile(t, t.TempDir(), "ca.pem", authority.certPEM)
	cfg := testGitConfig(t, "[http]\n\tsslCAInfo = "+caFile+"\n")
	if err := advertiseThrough(t, server.URL+"/repo.git", Options{Config: cfg}); err != nil {
		t.Fatalf("advertise with http.sslCAInfo returned error %v", err)
	}
}

func TestHTTPTrustsCertificatesFromSSLCAPath(t *testing.T) {
	clearProxyEnvironment(t)
	authority := newTestAuthority(t)
	server := newTLSRemote(t, authority, false)
	dir := t.TempDir()
	writeTestFile(t, dir, "notes.txt", []byte("not a certificate"))
	writeTestFile(t, dir, "ca.pem", authority.certPEM)
	if err := os.Mkdir(filepath.Join(dir, "nested"), 0o700); err != nil {
		t.Fatalf("Mkdir returned error %v", err)
	}
	cfg := testGitConfig(t, "[http]\n\tsslCAPath = "+filepath.ToSlash(dir)+"\n")
	if err := advertiseThrough(t, server.URL+"/repo.git", Options{Config: cfg}); err != nil {
		t.Fatalf("advertise with http.sslCAPath returned error %v", err)
	}
}

func TestHTTPSkipsVerificationForTheURLWithSSLVerifyFalseAndWarns(t *testing.T) {
	clearProxyEnvironment(t)
	server := newTLSRemote(t, newTestAuthority(t), false)
	cfg := testGitConfig(t, "[http \""+server.URL+"\"]\n\tsslVerify = false\n")
	var phases []string
	record := func(r progress.Report) { phases = append(phases, r.Phase) }
	if err := advertiseThrough(t, server.URL+"/repo.git", Options{Config: cfg, Progress: record}); err != nil {
		t.Fatalf("advertise with sslVerify=false returned error %v", err)
	}
	if !slices.Equal(phases, []string{progress.PhaseTLSVerifyDisabled}) {
		t.Fatalf("phases = %q, want one verification warning", phases)
	}
}

func TestHTTPPresentsTheClientCertificate(t *testing.T) {
	clearProxyEnvironment(t)
	authority := newTestAuthority(t)
	server := newTLSRemote(t, authority, true)
	dir := t.TempDir()
	caFile := writeTestFile(t, dir, "ca.pem", authority.certPEM)
	certPEM, keyPEM := authority.issue(t, 3, x509.ExtKeyUsageClientAuth)
	certFile := writeTestFile(t, dir, "client.pem", certPEM)
	keyFile := writeTestFile(t, dir, "client.key", keyPEM)
	combined := writeTestFile(t, dir, "combined.pem", append(append([]byte(nil), certPEM...), keyPEM...))

	without := testGitConfig(t, "[http]\n\tsslCAInfo = "+caFile+"\n")
	if err := advertiseThrough(t, server.URL+"/repo.git", Options{Config: without}); err == nil {
		t.Fatalf("advertise succeeded without the required client certificate")
	}
	separate := testGitConfig(t, "[http]\n\tsslCAInfo = "+caFile+"\n\tsslCert = "+certFile+"\n\tsslKey = "+keyFile+"\n")
	if err := advertiseThrough(t, server.URL+"/repo.git", Options{Config: separate}); err != nil {
		t.Fatalf("advertise with http.sslCert and http.sslKey returned error %v", err)
	}
	single := testGitConfig(t, "[http]\n\tsslCAInfo = "+caFile+"\n\tsslCert = "+combined+"\n")
	if err := advertiseThrough(t, server.URL+"/repo.git", Options{Config: single}); err != nil {
		t.Fatalf("advertise with the key inside http.sslCert returned error %v", err)
	}
}

func TestHTTPRefusesAnEncryptedClientKey(t *testing.T) {
	authority := newTestAuthority(t)
	certPEM, _ := authority.issue(t, 4, x509.ExtKeyUsageClientAuth)
	dir := t.TempDir()
	certFile := writeTestFile(t, dir, "client.pem", certPEM)
	keys := map[string][]byte{
		"pkcs8": pem.EncodeToMemory(&pem.Block{Type: "ENCRYPTED PRIVATE KEY", Bytes: []byte{1, 2, 3}}),
		"legacy": pem.EncodeToMemory(&pem.Block{
			Type:    "EC PRIVATE KEY",
			Headers: map[string]string{"Proc-Type": "4,ENCRYPTED", "DEK-Info": "AES-128-CBC,00"},
			Bytes:   []byte{1},
		}),
	}
	for name, keyPEM := range keys {
		t.Run(name, func(t *testing.T) {
			keyFile := writeTestFile(t, dir, name+".key", keyPEM)
			cfg := testGitConfig(t, "[http]\n\tsslCert = "+certFile+"\n\tsslKey = "+keyFile+"\n")
			_, err := Dial(t.Context(), "https://127.0.0.1:1/repo.git", UploadPack, Options{Config: cfg})
			if !errors.Is(err, ErrClientKeyEncrypted) {
				t.Fatalf("Dial returned %v, want ErrClientKeyEncrypted", err)
			}
		})
	}
}

func TestBuildTLSConfigReportsUnusableFiles(t *testing.T) {
	authority := newTestAuthority(t)
	dir := t.TempDir()
	certPEM, _ := authority.issue(t, 5, x509.ExtKeyUsageClientAuth)
	_, otherKeyPEM := authority.issue(t, 6, x509.ExtKeyUsageClientAuth)
	certFile := writeTestFile(t, dir, "client.pem", certPEM)
	otherKey := writeTestFile(t, dir, "other.key", otherKeyPEM)
	junk := writeTestFile(t, dir, "junk.pem", []byte("junk"))
	missing := filepath.Join(dir, "missing")
	tests := map[string]httpSettings{
		"missing ca file":     {caInfo: missing},
		"ca file without pem": {caInfo: junk},
		"missing ca dir":      {caPath: missing},
		"missing cert":        {sslCert: missing},
		"missing key":         {sslCert: certFile, sslKey: missing},
		"mismatched key":      {sslCert: certFile, sslKey: otherKey},
	}
	for name, settings := range tests {
		t.Run(name, func(t *testing.T) {
			settings.sslVerify = true
			if _, err := buildTLSConfig(settings); !errors.Is(err, ErrTLSConfig) {
				t.Fatalf("buildTLSConfig returned %v, want ErrTLSConfig", err)
			}
		})
	}
}

func TestExtraRootPoolStartsEmptyWithoutASystemPool(t *testing.T) {
	original := systemCertPool
	systemCertPool = func() (*x509.CertPool, error) { return nil, errors.New("no system store") }
	t.Cleanup(func() { systemCertPool = original })
	caFile := writeTestFile(t, t.TempDir(), "ca.pem", newTestAuthority(t).certPEM)
	pool, err := extraRootPool(caFile, "")
	if err != nil || pool == nil {
		t.Fatalf("extraRootPool returned %v, %v; want a pool with the extra authority", pool, err)
	}
}
