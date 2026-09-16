package transport

import (
	"cmp"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var systemCertPool = x509.SystemCertPool

func buildTLSConfig(s httpSettings) (*tls.Config, error) {
	cfg := &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: !s.sslVerify}
	if s.caInfo != "" || s.caPath != "" {
		pool, err := extraRootPool(s.caInfo, s.caPath)
		if err != nil {
			return nil, err
		}
		cfg.RootCAs = pool
	}
	if s.sslCert != "" {
		cert, err := loadClientCertificate(s.sslCert, cmp.Or(s.sslKey, s.sslCert))
		if err != nil {
			return nil, err
		}
		cfg.Certificates = []tls.Certificate{cert}
	}
	return cfg, nil
}

func extraRootPool(caInfo, caPath string) (*x509.CertPool, error) {
	pool, err := systemCertPool()
	if err != nil {
		pool = x509.NewCertPool()
	}
	if caInfo != "" {
		data, err := os.ReadFile(caInfo)
		if err != nil {
			return nil, fmt.Errorf("%w: http.sslCAInfo: %w", ErrTLSConfig, err)
		}
		if !pool.AppendCertsFromPEM(data) {
			return nil, fmt.Errorf("%w: http.sslCAInfo %s holds no certificate", ErrTLSConfig, caInfo)
		}
	}
	if caPath != "" {
		entries, err := os.ReadDir(caPath)
		if err != nil {
			return nil, fmt.Errorf("%w: http.sslCAPath: %w", ErrTLSConfig, err)
		}
		for _, entry := range entries {
			if data, err := os.ReadFile(filepath.Join(caPath, entry.Name())); err == nil {
				pool.AppendCertsFromPEM(data)
			}
		}
	}
	return pool, nil
}

func loadClientCertificate(certPath, keyPath string) (tls.Certificate, error) {
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("%w: http.sslCert: %w", ErrTLSConfig, err)
	}
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("%w: http.sslKey: %w", ErrTLSConfig, err)
	}
	if pemHasEncryptedBlock(keyPEM) {
		return tls.Certificate{}, fmt.Errorf("%w: %s", ErrClientKeyEncrypted, keyPath)
	}
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("%w: %w", ErrTLSConfig, err)
	}
	return cert, nil
}

func pemHasEncryptedBlock(data []byte) bool {
	for {
		block, rest := pem.Decode(data)
		if block == nil {
			return false
		}
		if block.Type == "ENCRYPTED PRIVATE KEY" || strings.Contains(block.Headers["Proc-Type"], "ENCRYPTED") {
			return true
		}
		data = rest
	}
}
