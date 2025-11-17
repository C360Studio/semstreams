package tlsutil

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/c360/semstreams/pkg/security"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// generateTestCert creates a self-signed certificate for testing
func generateTestCert(t *testing.T) (certPEM, keyPEM []byte) {
	t.Helper()

	// Generate private key
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	// Create certificate template
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Test Org"},
			CommonName:   "localhost",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	// Create self-signed certificate
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	require.NoError(t, err)

	// Encode certificate to PEM
	certPEM = pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certDER,
	})

	// Encode private key to PEM
	keyPEM = pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
	})

	return certPEM, keyPEM
}

// setupTestFiles creates temporary cert/key files for testing
func setupTestFiles(t *testing.T) (certFile, keyFile, caFile string, cleanup func()) {
	t.Helper()

	tmpDir := t.TempDir()

	certPEM, keyPEM := generateTestCert(t)

	certFile = filepath.Join(tmpDir, "cert.pem")
	keyFile = filepath.Join(tmpDir, "key.pem")
	caFile = filepath.Join(tmpDir, "ca.pem")

	require.NoError(t, os.WriteFile(certFile, certPEM, 0644))
	require.NoError(t, os.WriteFile(keyFile, keyPEM, 0600))
	require.NoError(t, os.WriteFile(caFile, certPEM, 0644)) // Use same cert as CA for testing

	cleanup = func() {
		_ = os.RemoveAll(tmpDir)
	}

	return certFile, keyFile, caFile, cleanup
}

func TestLoadServerTLSConfig(t *testing.T) {
	certFile, keyFile, _, cleanup := setupTestFiles(t)
	defer cleanup()

	tests := []struct {
		name    string
		cfg     security.ServerTLSConfig
		wantNil bool
		wantErr bool
	}{
		{
			name: "disabled",
			cfg: security.ServerTLSConfig{
				Enabled: false,
			},
			wantNil: true,
			wantErr: false,
		},
		{
			name: "enabled with valid cert",
			cfg: security.ServerTLSConfig{
				Enabled:    true,
				CertFile:   certFile,
				KeyFile:    keyFile,
				MinVersion: "1.3",
			},
			wantNil: false,
			wantErr: false,
		},
		{
			name: "enabled with TLS 1.2",
			cfg: security.ServerTLSConfig{
				Enabled:    true,
				CertFile:   certFile,
				KeyFile:    keyFile,
				MinVersion: "1.2",
			},
			wantNil: false,
			wantErr: false,
		},
		{
			name: "missing cert file",
			cfg: security.ServerTLSConfig{
				Enabled:  true,
				CertFile: "/nonexistent/cert.pem",
				KeyFile:  keyFile,
			},
			wantNil: true,
			wantErr: true,
		},
		{
			name: "missing key file",
			cfg: security.ServerTLSConfig{
				Enabled:  true,
				CertFile: certFile,
				KeyFile:  "/nonexistent/key.pem",
			},
			wantNil: true,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := LoadServerTLSConfig(tt.cfg)

			if tt.wantErr {
				require.Error(t, err)
				assert.Nil(t, got)
				return
			}

			require.NoError(t, err)

			if tt.wantNil {
				assert.Nil(t, got)
				return
			}

			require.NotNil(t, got)
			assert.NotEmpty(t, got.Certificates)

			// Verify MinVersion
			expectedVersion := parseTLSVersion(tt.cfg.MinVersion)
			assert.Equal(t, expectedVersion, got.MinVersion)
		})
	}
}

func TestLoadClientTLSConfig(t *testing.T) {
	_, _, caFile, cleanup := setupTestFiles(t)
	defer cleanup()

	tests := []struct {
		name    string
		cfg     security.ClientTLSConfig
		wantErr bool
		checkFn func(*testing.T, *tls.Config)
	}{
		{
			name: "default config with system CA pool",
			cfg:  security.ClientTLSConfig{},
			checkFn: func(t *testing.T, tlsCfg *tls.Config) {
				assert.NotNil(t, tlsCfg.RootCAs)
				assert.Equal(t, uint16(tls.VersionTLS12), tlsCfg.MinVersion)
				assert.False(t, tlsCfg.InsecureSkipVerify)
			},
		},
		{
			name: "with additional CA file",
			cfg: security.ClientTLSConfig{
				CAFiles: []string{caFile},
			},
			checkFn: func(t *testing.T, tlsCfg *tls.Config) {
				assert.NotNil(t, tlsCfg.RootCAs)
				// Verify CA was added (pool should have system CAs + our CA)
				// We can't easily count certs, but RootCAs should not be empty
			},
		},
		{
			name: "with TLS 1.3",
			cfg: security.ClientTLSConfig{
				MinVersion: "1.3",
			},
			checkFn: func(t *testing.T, tlsCfg *tls.Config) {
				assert.Equal(t, uint16(tls.VersionTLS13), tlsCfg.MinVersion)
			},
		},
		{
			name: "with InsecureSkipVerify",
			cfg: security.ClientTLSConfig{
				InsecureSkipVerify: true,
			},
			checkFn: func(t *testing.T, tlsCfg *tls.Config) {
				assert.True(t, tlsCfg.InsecureSkipVerify)
			},
		},
		{
			name: "missing CA file",
			cfg: security.ClientTLSConfig{
				CAFiles: []string{"/nonexistent/ca.pem"},
			},
			wantErr: true,
		},
		{
			name: "multiple CA files",
			cfg: security.ClientTLSConfig{
				CAFiles: []string{caFile, caFile}, // Same file twice is fine for testing
			},
			checkFn: func(t *testing.T, tlsCfg *tls.Config) {
				assert.NotNil(t, tlsCfg.RootCAs)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := LoadClientTLSConfig(tt.cfg)

			if tt.wantErr {
				require.Error(t, err)
				assert.Nil(t, got)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, got)

			if tt.checkFn != nil {
				tt.checkFn(t, got)
			}
		})
	}
}

func TestParseTLSVersion(t *testing.T) {
	tests := []struct {
		version string
		want    uint16
	}{
		{"1.3", tls.VersionTLS13},
		{"1.2", tls.VersionTLS12},
		{"", tls.VersionTLS12},        // Default
		{"invalid", tls.VersionTLS12}, // Default fallback
		{"1.1", tls.VersionTLS12},     // Old version defaults to 1.2
	}

	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			got := parseTLSVersion(tt.version)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestLoadServerTLSConfig_CertificateValidation(t *testing.T) {
	certFile, keyFile, _, cleanup := setupTestFiles(t)
	defer cleanup()

	cfg := security.ServerTLSConfig{
		Enabled:    true,
		CertFile:   certFile,
		KeyFile:    keyFile,
		MinVersion: "1.3",
	}

	tlsCfg, err := LoadServerTLSConfig(cfg)
	require.NoError(t, err)
	require.NotNil(t, tlsCfg)

	// Verify certificate was loaded
	assert.Len(t, tlsCfg.Certificates, 1)

	// Verify MinVersion
	assert.Equal(t, uint16(tls.VersionTLS13), tlsCfg.MinVersion)

	// Verify certificate is valid (has leaf)
	cert := tlsCfg.Certificates[0]
	assert.NotEmpty(t, cert.Certificate)
}

func TestLoadClientTLSConfig_SystemCAPool(t *testing.T) {
	cfg := security.ClientTLSConfig{}

	tlsCfg, err := LoadClientTLSConfig(cfg)
	require.NoError(t, err)
	require.NotNil(t, tlsCfg)

	// RootCAs should be populated with system pool
	assert.NotNil(t, tlsCfg.RootCAs)

	// Should be able to get subjects (will fail if pool is empty on most systems)
	// Note: This may be empty on some minimal systems, but that's OK
	subjects := tlsCfg.RootCAs.Subjects()
	t.Logf("System CA pool has %d subjects", len(subjects))
}

func TestLoadClientTLSConfig_AdditionalCA(t *testing.T) {
	_, _, caFile, cleanup := setupTestFiles(t)
	defer cleanup()

	cfg := security.ClientTLSConfig{
		CAFiles: []string{caFile},
	}

	tlsCfg, err := LoadClientTLSConfig(cfg)
	require.NoError(t, err)
	require.NotNil(t, tlsCfg)

	// RootCAs should have system pool + our CA
	assert.NotNil(t, tlsCfg.RootCAs)

	// Parse our test CA to verify it can be loaded
	caPEM, err := os.ReadFile(caFile)
	require.NoError(t, err)

	pool := x509.NewCertPool()
	ok := pool.AppendCertsFromPEM(caPEM)
	assert.True(t, ok, "Test CA should be valid PEM")
}

// generateTestCertWithCN creates a self-signed certificate with a specific CN
func generateTestCertWithCN(t *testing.T, cn string) (certPEM, keyPEM []byte) {
	t.Helper()

	// Generate private key
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	// Create certificate template
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Test Org"},
			CommonName:   cn,
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}

	// Create self-signed certificate
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	require.NoError(t, err)

	// Encode certificate to PEM
	certPEM = pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certDER,
	})

	// Encode private key to PEM
	keyPEM = pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
	})

	return certPEM, keyPEM
}

func TestLoadServerTLSConfigWithMTLS_Disabled(t *testing.T) {
	certFile, keyFile, _, cleanup := setupTestFiles(t)
	defer cleanup()

	serverCfg := security.ServerTLSConfig{
		Enabled:  true,
		CertFile: certFile,
		KeyFile:  keyFile,
	}

	mtlsCfg := security.ServerMTLSConfig{
		Enabled: false,
	}

	tlsCfg, err := LoadServerTLSConfigWithMTLS(serverCfg, mtlsCfg)
	require.NoError(t, err)
	require.NotNil(t, tlsCfg)

	// Should not require client certs when mTLS is disabled
	assert.Equal(t, tls.NoClientCert, tlsCfg.ClientAuth)
	assert.Nil(t, tlsCfg.ClientCAs)
}

func TestLoadServerTLSConfigWithMTLS_RequireClientCert(t *testing.T) {
	certFile, keyFile, caFile, cleanup := setupTestFiles(t)
	defer cleanup()

	serverCfg := security.ServerTLSConfig{
		Enabled:  true,
		CertFile: certFile,
		KeyFile:  keyFile,
	}

	mtlsCfg := security.ServerMTLSConfig{
		Enabled:           true,
		ClientCAFiles:     []string{caFile},
		RequireClientCert: true,
	}

	tlsCfg, err := LoadServerTLSConfigWithMTLS(serverCfg, mtlsCfg)
	require.NoError(t, err)
	require.NotNil(t, tlsCfg)

	// Should require and verify client certs
	assert.Equal(t, tls.RequireAndVerifyClientCert, tlsCfg.ClientAuth)
	assert.NotNil(t, tlsCfg.ClientCAs)
}

func TestLoadServerTLSConfigWithMTLS_OptionalClientCert(t *testing.T) {
	certFile, keyFile, caFile, cleanup := setupTestFiles(t)
	defer cleanup()

	serverCfg := security.ServerTLSConfig{
		Enabled:  true,
		CertFile: certFile,
		KeyFile:  keyFile,
	}

	mtlsCfg := security.ServerMTLSConfig{
		Enabled:           true,
		ClientCAFiles:     []string{caFile},
		RequireClientCert: false,
	}

	tlsCfg, err := LoadServerTLSConfigWithMTLS(serverCfg, mtlsCfg)
	require.NoError(t, err)
	require.NotNil(t, tlsCfg)

	// Should verify client certs if given
	assert.Equal(t, tls.VerifyClientCertIfGiven, tlsCfg.ClientAuth)
	assert.NotNil(t, tlsCfg.ClientCAs)
}

func TestLoadServerTLSConfigWithMTLS_WithCNWhitelist(t *testing.T) {
	certFile, keyFile, caFile, cleanup := setupTestFiles(t)
	defer cleanup()

	serverCfg := security.ServerTLSConfig{
		Enabled:  true,
		CertFile: certFile,
		KeyFile:  keyFile,
	}

	mtlsCfg := security.ServerMTLSConfig{
		Enabled:           true,
		ClientCAFiles:     []string{caFile},
		RequireClientCert: true,
		AllowedClientCNs:  []string{"allowed-client", "another-client"},
	}

	tlsCfg, err := LoadServerTLSConfigWithMTLS(serverCfg, mtlsCfg)
	require.NoError(t, err)
	require.NotNil(t, tlsCfg)

	// Should have custom verification callback
	assert.NotNil(t, tlsCfg.VerifyPeerCertificate)
}

func TestLoadServerTLSConfigWithMTLS_MissingClientCA(t *testing.T) {
	certFile, keyFile, _, cleanup := setupTestFiles(t)
	defer cleanup()

	serverCfg := security.ServerTLSConfig{
		Enabled:  true,
		CertFile: certFile,
		KeyFile:  keyFile,
	}

	mtlsCfg := security.ServerMTLSConfig{
		Enabled:           true,
		ClientCAFiles:     []string{"/nonexistent/ca.pem"},
		RequireClientCert: true,
	}

	_, err := LoadServerTLSConfigWithMTLS(serverCfg, mtlsCfg)
	require.Error(t, err)
}

func TestVerifyAllowedClientCN_Allowed(t *testing.T) {
	// Create test cert with specific CN
	certPEM, _ := generateTestCertWithCN(t, "allowed-client")

	// Parse cert
	block, _ := pem.Decode(certPEM)
	require.NotNil(t, block)

	cert, err := x509.ParseCertificate(block.Bytes)
	require.NoError(t, err)

	// Create verified chains
	chains := [][]*x509.Certificate{
		{cert},
	}

	allowedCNs := []string{"allowed-client", "another-client"}

	err = verifyAllowedClientCN(chains, allowedCNs)
	assert.NoError(t, err)
}

func TestVerifyAllowedClientCN_NotAllowed(t *testing.T) {
	// Create test cert with specific CN
	certPEM, _ := generateTestCertWithCN(t, "unauthorized-client")

	// Parse cert
	block, _ := pem.Decode(certPEM)
	require.NotNil(t, block)

	cert, err := x509.ParseCertificate(block.Bytes)
	require.NoError(t, err)

	// Create verified chains
	chains := [][]*x509.Certificate{
		{cert},
	}

	allowedCNs := []string{"allowed-client", "another-client"}

	err = verifyAllowedClientCN(chains, allowedCNs)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not in allowed list")
}

func TestVerifyAllowedClientCN_NoChains(t *testing.T) {
	chains := [][]*x509.Certificate{}
	allowedCNs := []string{"allowed-client"}

	err := verifyAllowedClientCN(chains, allowedCNs)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no verified certificate chains")
}

func TestLoadClientTLSConfigWithMTLS_Disabled(t *testing.T) {
	_, _, caFile, cleanup := setupTestFiles(t)
	defer cleanup()

	clientCfg := security.ClientTLSConfig{
		CAFiles: []string{caFile},
	}

	mtlsCfg := security.ClientMTLSConfig{
		Enabled: false,
	}

	tlsCfg, err := LoadClientTLSConfigWithMTLS(clientCfg, mtlsCfg)
	require.NoError(t, err)
	require.NotNil(t, tlsCfg)

	// Should not have client certificates
	assert.Empty(t, tlsCfg.Certificates)
}

func TestLoadClientTLSConfigWithMTLS_Enabled(t *testing.T) {
	certFile, keyFile, caFile, cleanup := setupTestFiles(t)
	defer cleanup()

	clientCfg := security.ClientTLSConfig{
		CAFiles: []string{caFile},
	}

	mtlsCfg := security.ClientMTLSConfig{
		Enabled:  true,
		CertFile: certFile,
		KeyFile:  keyFile,
	}

	tlsCfg, err := LoadClientTLSConfigWithMTLS(clientCfg, mtlsCfg)
	require.NoError(t, err)
	require.NotNil(t, tlsCfg)

	// Should have client certificate
	assert.Len(t, tlsCfg.Certificates, 1)
	assert.NotEmpty(t, tlsCfg.Certificates[0].Certificate)
}

func TestLoadClientTLSConfigWithMTLS_MissingCert(t *testing.T) {
	_, keyFile, caFile, cleanup := setupTestFiles(t)
	defer cleanup()

	clientCfg := security.ClientTLSConfig{
		CAFiles: []string{caFile},
	}

	mtlsCfg := security.ClientMTLSConfig{
		Enabled:  true,
		CertFile: "/nonexistent/cert.pem",
		KeyFile:  keyFile,
	}

	_, err := LoadClientTLSConfigWithMTLS(clientCfg, mtlsCfg)
	require.Error(t, err)
}

func TestLoadClientTLSConfigWithMTLS_MissingKey(t *testing.T) {
	certFile, _, caFile, cleanup := setupTestFiles(t)
	defer cleanup()

	clientCfg := security.ClientTLSConfig{
		CAFiles: []string{caFile},
	}

	mtlsCfg := security.ClientMTLSConfig{
		Enabled:  true,
		CertFile: certFile,
		KeyFile:  "/nonexistent/key.pem",
	}

	_, err := LoadClientTLSConfigWithMTLS(clientCfg, mtlsCfg)
	require.Error(t, err)
}

// TestBackwardCompatibility ensures existing code works without mTLS config
func TestBackwardCompatibility_ServerWithoutMTLS(t *testing.T) {
	certFile, keyFile, _, cleanup := setupTestFiles(t)
	defer cleanup()

	// Old-style config without mTLS
	serverCfg := security.ServerTLSConfig{
		Enabled:  true,
		CertFile: certFile,
		KeyFile:  keyFile,
	}

	// Empty mTLS config (backwards compatible)
	mtlsCfg := security.ServerMTLSConfig{}

	tlsCfg, err := LoadServerTLSConfigWithMTLS(serverCfg, mtlsCfg)
	require.NoError(t, err)
	require.NotNil(t, tlsCfg)

	// Should not require client certs
	assert.Equal(t, tls.NoClientCert, tlsCfg.ClientAuth)
}

// TestBackwardCompatibility ensures existing code works without mTLS config
func TestBackwardCompatibility_ClientWithoutMTLS(t *testing.T) {
	_, _, caFile, cleanup := setupTestFiles(t)
	defer cleanup()

	// Old-style config without mTLS
	clientCfg := security.ClientTLSConfig{
		CAFiles: []string{caFile},
	}

	// Empty mTLS config (backwards compatible)
	mtlsCfg := security.ClientMTLSConfig{}

	tlsCfg, err := LoadClientTLSConfigWithMTLS(clientCfg, mtlsCfg)
	require.NoError(t, err)
	require.NotNil(t, tlsCfg)

	// Should not have client certificates
	assert.Empty(t, tlsCfg.Certificates)
}
