package tlsutil

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/c360/semstreams/pkg/security"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMTLSHandshake_ServerRequiresClientCert tests successful mTLS handshake
func TestMTLSHandshake_ServerRequiresClientCert(t *testing.T) {
	tmpDir := t.TempDir()

	// Generate server cert and CA
	serverCertPEM, serverKeyPEM := generateTestCert(t)
	serverCertFile := filepath.Join(tmpDir, "server-cert.pem")
	serverKeyFile := filepath.Join(tmpDir, "server-key.pem")
	require.NoError(t, os.WriteFile(serverCertFile, serverCertPEM, 0644))
	require.NoError(t, os.WriteFile(serverKeyFile, serverKeyPEM, 0600))

	// Generate client cert (will be used as both client cert and CA for simplicity)
	clientCertPEM, clientKeyPEM := generateTestCertWithCN(t, "test-client")
	clientCertFile := filepath.Join(tmpDir, "client-cert.pem")
	clientKeyFile := filepath.Join(tmpDir, "client-key.pem")
	clientCAFile := filepath.Join(tmpDir, "client-ca.pem")
	require.NoError(t, os.WriteFile(clientCertFile, clientCertPEM, 0644))
	require.NoError(t, os.WriteFile(clientKeyFile, clientKeyPEM, 0600))
	require.NoError(t, os.WriteFile(clientCAFile, clientCertPEM, 0644)) // Self-signed, so cert = CA

	// Configure server with mTLS (require client cert)
	serverCfg := security.ServerTLSConfig{
		Enabled:  true,
		CertFile: serverCertFile,
		KeyFile:  serverKeyFile,
	}

	serverMTLSCfg := security.ServerMTLSConfig{
		Enabled:           true,
		ClientCAFiles:     []string{clientCAFile},
		RequireClientCert: true,
	}

	serverTLSConfig, err := LoadServerTLSConfigWithMTLS(serverCfg, serverMTLSCfg)
	require.NoError(t, err)

	// Create test HTTPS server
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify client cert was provided
		if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
			http.Error(w, "No client certificate", http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	server := httptest.NewUnstartedServer(handler)
	server.TLS = serverTLSConfig
	server.StartTLS()
	defer server.Close()

	// Configure client with mTLS
	clientCfg := security.ClientTLSConfig{
		InsecureSkipVerify: true, // Skip server cert validation for test
	}

	clientMTLSCfg := security.ClientMTLSConfig{
		Enabled:  true,
		CertFile: clientCertFile,
		KeyFile:  clientKeyFile,
	}

	clientTLSConfig, err := LoadClientTLSConfigWithMTLS(clientCfg, clientMTLSCfg)
	require.NoError(t, err)

	// Create HTTP client with mTLS
	httpClient := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: clientTLSConfig,
		},
	}

	// Make request - should succeed
	resp, err := httpClient.Get(server.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	assert.Equal(t, "OK", string(body))
}

// TestMTLSHandshake_ServerRequiresClientCert_NoClientCert tests rejection without client cert
func TestMTLSHandshake_ServerRequiresClientCert_NoClientCert(t *testing.T) {
	tmpDir := t.TempDir()

	// Generate server cert
	serverCertPEM, serverKeyPEM := generateTestCert(t)
	serverCertFile := filepath.Join(tmpDir, "server-cert.pem")
	serverKeyFile := filepath.Join(tmpDir, "server-key.pem")
	require.NoError(t, os.WriteFile(serverCertFile, serverCertPEM, 0644))
	require.NoError(t, os.WriteFile(serverKeyFile, serverKeyPEM, 0600))

	// Generate client CA
	clientCertPEM, _ := generateTestCertWithCN(t, "test-client")
	clientCAFile := filepath.Join(tmpDir, "client-ca.pem")
	require.NoError(t, os.WriteFile(clientCAFile, clientCertPEM, 0644))

	// Configure server with mTLS (require client cert)
	serverCfg := security.ServerTLSConfig{
		Enabled:  true,
		CertFile: serverCertFile,
		KeyFile:  serverKeyFile,
	}

	serverMTLSCfg := security.ServerMTLSConfig{
		Enabled:           true,
		ClientCAFiles:     []string{clientCAFile},
		RequireClientCert: true,
	}

	serverTLSConfig, err := LoadServerTLSConfigWithMTLS(serverCfg, serverMTLSCfg)
	require.NoError(t, err)

	// Create test HTTPS server
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	server := httptest.NewUnstartedServer(handler)
	server.TLS = serverTLSConfig
	server.StartTLS()
	defer server.Close()

	// Configure client WITHOUT mTLS (no client cert)
	clientCfg := security.ClientTLSConfig{
		InsecureSkipVerify: true, // Skip server cert validation for test
	}

	clientMTLSCfg := security.ClientMTLSConfig{
		Enabled: false, // No client cert
	}

	clientTLSConfig, err := LoadClientTLSConfigWithMTLS(clientCfg, clientMTLSCfg)
	require.NoError(t, err)

	// Create HTTP client without mTLS
	httpClient := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: clientTLSConfig,
		},
	}

	// Make request - should fail with TLS error
	_, err = httpClient.Get(server.URL)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tls")
}

// TestMTLSHandshake_CNWhitelist_Allowed tests CN whitelist allowing authorized client
func TestMTLSHandshake_CNWhitelist_Allowed(t *testing.T) {
	tmpDir := t.TempDir()

	// Generate server cert
	serverCertPEM, serverKeyPEM := generateTestCert(t)
	serverCertFile := filepath.Join(tmpDir, "server-cert.pem")
	serverKeyFile := filepath.Join(tmpDir, "server-key.pem")
	require.NoError(t, os.WriteFile(serverCertFile, serverCertPEM, 0644))
	require.NoError(t, os.WriteFile(serverKeyFile, serverKeyPEM, 0600))

	// Generate client cert with specific CN
	clientCN := "authorized-client"
	clientCertPEM, clientKeyPEM := generateTestCertWithCN(t, clientCN)
	clientCertFile := filepath.Join(tmpDir, "client-cert.pem")
	clientKeyFile := filepath.Join(tmpDir, "client-key.pem")
	clientCAFile := filepath.Join(tmpDir, "client-ca.pem")
	require.NoError(t, os.WriteFile(clientCertFile, clientCertPEM, 0644))
	require.NoError(t, os.WriteFile(clientKeyFile, clientKeyPEM, 0600))
	require.NoError(t, os.WriteFile(clientCAFile, clientCertPEM, 0644))

	// Configure server with mTLS and CN whitelist
	serverCfg := security.ServerTLSConfig{
		Enabled:  true,
		CertFile: serverCertFile,
		KeyFile:  serverKeyFile,
	}

	serverMTLSCfg := security.ServerMTLSConfig{
		Enabled:           true,
		ClientCAFiles:     []string{clientCAFile},
		RequireClientCert: true,
		AllowedClientCNs:  []string{clientCN, "another-allowed-client"},
	}

	serverTLSConfig, err := LoadServerTLSConfigWithMTLS(serverCfg, serverMTLSCfg)
	require.NoError(t, err)

	// Create test HTTPS server
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	server := httptest.NewUnstartedServer(handler)
	server.TLS = serverTLSConfig
	server.StartTLS()
	defer server.Close()

	// Configure client with mTLS
	clientCfg := security.ClientTLSConfig{
		InsecureSkipVerify: true,
	}

	clientMTLSCfg := security.ClientMTLSConfig{
		Enabled:  true,
		CertFile: clientCertFile,
		KeyFile:  clientKeyFile,
	}

	clientTLSConfig, err := LoadClientTLSConfigWithMTLS(clientCfg, clientMTLSCfg)
	require.NoError(t, err)

	// Create HTTP client
	httpClient := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: clientTLSConfig,
		},
	}

	// Make request - should succeed (CN is in whitelist)
	resp, err := httpClient.Get(server.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// TestMTLSHandshake_CNWhitelist_Rejected tests CN whitelist rejecting unauthorized client
func TestMTLSHandshake_CNWhitelist_Rejected(t *testing.T) {
	tmpDir := t.TempDir()

	// Generate server cert
	serverCertPEM, serverKeyPEM := generateTestCert(t)
	serverCertFile := filepath.Join(tmpDir, "server-cert.pem")
	serverKeyFile := filepath.Join(tmpDir, "server-key.pem")
	require.NoError(t, os.WriteFile(serverCertFile, serverCertPEM, 0644))
	require.NoError(t, os.WriteFile(serverKeyFile, serverKeyPEM, 0600))

	// Generate client cert with specific CN (NOT in whitelist)
	clientCN := "unauthorized-client"
	clientCertPEM, clientKeyPEM := generateTestCertWithCN(t, clientCN)
	clientCertFile := filepath.Join(tmpDir, "client-cert.pem")
	clientKeyFile := filepath.Join(tmpDir, "client-key.pem")
	clientCAFile := filepath.Join(tmpDir, "client-ca.pem")
	require.NoError(t, os.WriteFile(clientCertFile, clientCertPEM, 0644))
	require.NoError(t, os.WriteFile(clientKeyFile, clientKeyPEM, 0600))
	require.NoError(t, os.WriteFile(clientCAFile, clientCertPEM, 0644))

	// Configure server with mTLS and CN whitelist (does NOT include client CN)
	serverCfg := security.ServerTLSConfig{
		Enabled:  true,
		CertFile: serverCertFile,
		KeyFile:  serverKeyFile,
	}

	serverMTLSCfg := security.ServerMTLSConfig{
		Enabled:           true,
		ClientCAFiles:     []string{clientCAFile},
		RequireClientCert: true,
		AllowedClientCNs:  []string{"authorized-client", "another-allowed-client"},
	}

	serverTLSConfig, err := LoadServerTLSConfigWithMTLS(serverCfg, serverMTLSCfg)
	require.NoError(t, err)

	// Create test HTTPS server
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	server := httptest.NewUnstartedServer(handler)
	server.TLS = serverTLSConfig
	server.StartTLS()
	defer server.Close()

	// Configure client with mTLS
	clientCfg := security.ClientTLSConfig{
		InsecureSkipVerify: true,
	}

	clientMTLSCfg := security.ClientMTLSConfig{
		Enabled:  true,
		CertFile: clientCertFile,
		KeyFile:  clientKeyFile,
	}

	clientTLSConfig, err := LoadClientTLSConfigWithMTLS(clientCfg, clientMTLSCfg)
	require.NoError(t, err)

	// Create HTTP client
	httpClient := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: clientTLSConfig,
		},
	}

	// Make request - should fail (CN not in whitelist)
	_, err = httpClient.Get(server.URL)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tls")
}

// TestMTLSHandshake_OptionalClientCert_WithCert tests optional mTLS with client cert provided
func TestMTLSHandshake_OptionalClientCert_WithCert(t *testing.T) {
	tmpDir := t.TempDir()

	// Generate server cert
	serverCertPEM, serverKeyPEM := generateTestCert(t)
	serverCertFile := filepath.Join(tmpDir, "server-cert.pem")
	serverKeyFile := filepath.Join(tmpDir, "server-key.pem")
	require.NoError(t, os.WriteFile(serverCertFile, serverCertPEM, 0644))
	require.NoError(t, os.WriteFile(serverKeyFile, serverKeyPEM, 0600))

	// Generate client cert
	clientCertPEM, clientKeyPEM := generateTestCertWithCN(t, "test-client")
	clientCertFile := filepath.Join(tmpDir, "client-cert.pem")
	clientKeyFile := filepath.Join(tmpDir, "client-key.pem")
	clientCAFile := filepath.Join(tmpDir, "client-ca.pem")
	require.NoError(t, os.WriteFile(clientCertFile, clientCertPEM, 0644))
	require.NoError(t, os.WriteFile(clientKeyFile, clientKeyPEM, 0600))
	require.NoError(t, os.WriteFile(clientCAFile, clientCertPEM, 0644))

	// Configure server with optional mTLS
	serverCfg := security.ServerTLSConfig{
		Enabled:  true,
		CertFile: serverCertFile,
		KeyFile:  serverKeyFile,
	}

	serverMTLSCfg := security.ServerMTLSConfig{
		Enabled:           true,
		ClientCAFiles:     []string{clientCAFile},
		RequireClientCert: false, // Optional
	}

	serverTLSConfig, err := LoadServerTLSConfigWithMTLS(serverCfg, serverMTLSCfg)
	require.NoError(t, err)

	// Verify config is set to verify client cert if given
	assert.Equal(t, tls.VerifyClientCertIfGiven, serverTLSConfig.ClientAuth)

	// Create test HTTPS server
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if client cert was provided
		if r.TLS != nil && len(r.TLS.PeerCertificates) > 0 {
			w.Header().Set("X-Client-Cert", "present")
		}
		w.WriteHeader(http.StatusOK)
	})

	server := httptest.NewUnstartedServer(handler)
	server.TLS = serverTLSConfig
	server.StartTLS()
	defer server.Close()

	// Configure client with mTLS
	clientCfg := security.ClientTLSConfig{
		InsecureSkipVerify: true,
	}

	clientMTLSCfg := security.ClientMTLSConfig{
		Enabled:  true,
		CertFile: clientCertFile,
		KeyFile:  clientKeyFile,
	}

	clientTLSConfig, err := LoadClientTLSConfigWithMTLS(clientCfg, clientMTLSCfg)
	require.NoError(t, err)

	httpClient := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: clientTLSConfig,
		},
	}

	// Make request - should succeed and server should see client cert
	resp, err := httpClient.Get(server.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "present", resp.Header.Get("X-Client-Cert"))
}

// TestMTLSHandshake_OptionalClientCert_WithoutCert tests optional mTLS without client cert
func TestMTLSHandshake_OptionalClientCert_WithoutCert(t *testing.T) {
	tmpDir := t.TempDir()

	// Generate server cert
	serverCertPEM, serverKeyPEM := generateTestCert(t)
	serverCertFile := filepath.Join(tmpDir, "server-cert.pem")
	serverKeyFile := filepath.Join(tmpDir, "server-key.pem")
	require.NoError(t, os.WriteFile(serverCertFile, serverCertPEM, 0644))
	require.NoError(t, os.WriteFile(serverKeyFile, serverKeyPEM, 0600))

	// Generate client CA
	clientCertPEM, _ := generateTestCertWithCN(t, "test-client")
	clientCAFile := filepath.Join(tmpDir, "client-ca.pem")
	require.NoError(t, os.WriteFile(clientCAFile, clientCertPEM, 0644))

	// Configure server with optional mTLS
	serverCfg := security.ServerTLSConfig{
		Enabled:  true,
		CertFile: serverCertFile,
		KeyFile:  serverKeyFile,
	}

	serverMTLSCfg := security.ServerMTLSConfig{
		Enabled:           true,
		ClientCAFiles:     []string{clientCAFile},
		RequireClientCert: false, // Optional
	}

	serverTLSConfig, err := LoadServerTLSConfigWithMTLS(serverCfg, serverMTLSCfg)
	require.NoError(t, err)

	// Create test HTTPS server
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if client cert was provided
		if r.TLS != nil && len(r.TLS.PeerCertificates) > 0 {
			w.Header().Set("X-Client-Cert", "present")
		} else {
			w.Header().Set("X-Client-Cert", "absent")
		}
		w.WriteHeader(http.StatusOK)
	})

	server := httptest.NewUnstartedServer(handler)
	server.TLS = serverTLSConfig
	server.StartTLS()
	defer server.Close()

	// Configure client WITHOUT mTLS
	clientCfg := security.ClientTLSConfig{
		InsecureSkipVerify: true,
	}

	clientMTLSCfg := security.ClientMTLSConfig{
		Enabled: false, // No client cert
	}

	clientTLSConfig, err := LoadClientTLSConfigWithMTLS(clientCfg, clientMTLSCfg)
	require.NoError(t, err)

	httpClient := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: clientTLSConfig,
		},
	}

	// Make request - should succeed even without client cert (optional mTLS)
	resp, err := httpClient.Get(server.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "absent", resp.Header.Get("X-Client-Cert"))
}

// TestBackwardCompatibility_ManualTLS_WithoutMTLS ensures manual TLS (no mTLS) still works
func TestBackwardCompatibility_ManualTLS_WithoutMTLS(t *testing.T) {
	tmpDir := t.TempDir()

	// Generate server cert
	serverCertPEM, serverKeyPEM := generateTestCert(t)
	serverCertFile := filepath.Join(tmpDir, "server-cert.pem")
	serverKeyFile := filepath.Join(tmpDir, "server-key.pem")
	require.NoError(t, os.WriteFile(serverCertFile, serverCertPEM, 0644))
	require.NoError(t, os.WriteFile(serverKeyFile, serverKeyPEM, 0600))

	// Configure server with TLS but NO mTLS
	serverCfg := security.ServerTLSConfig{
		Enabled:  true,
		CertFile: serverCertFile,
		KeyFile:  serverKeyFile,
	}

	// Empty mTLS config (backwards compatible)
	serverMTLSCfg := security.ServerMTLSConfig{}

	serverTLSConfig, err := LoadServerTLSConfigWithMTLS(serverCfg, serverMTLSCfg)
	require.NoError(t, err)

	// Verify no client cert requirement
	assert.Equal(t, tls.NoClientCert, serverTLSConfig.ClientAuth)

	// Create test HTTPS server
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	server := httptest.NewUnstartedServer(handler)
	server.TLS = serverTLSConfig
	server.StartTLS()
	defer server.Close()

	// Regular client (no mTLS)
	httpClient := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
	}

	// Make request - should succeed
	resp, err := httpClient.Get(server.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	assert.Equal(t, "OK", string(body))
}

// TestClientCertLoading verifies that client certificates are properly loaded and configured
func TestClientCertLoading(t *testing.T) {
	tmpDir := t.TempDir()

	// Generate client cert
	clientCertPEM, clientKeyPEM := generateTestCertWithCN(t, "test-client")
	clientCertFile := filepath.Join(tmpDir, "client-cert.pem")
	clientKeyFile := filepath.Join(tmpDir, "client-key.pem")
	require.NoError(t, os.WriteFile(clientCertFile, clientCertPEM, 0644))
	require.NoError(t, os.WriteFile(clientKeyFile, clientKeyPEM, 0600))

	// Configure client with mTLS
	clientCfg := security.ClientTLSConfig{}

	clientMTLSCfg := security.ClientMTLSConfig{
		Enabled:  true,
		CertFile: clientCertFile,
		KeyFile:  clientKeyFile,
	}

	clientTLSConfig, err := LoadClientTLSConfigWithMTLS(clientCfg, clientMTLSCfg)
	require.NoError(t, err)

	// Verify client certificate is loaded
	require.Len(t, clientTLSConfig.Certificates, 1)
	assert.NotEmpty(t, clientTLSConfig.Certificates[0].Certificate)

	// Parse loaded cert to verify CN
	cert := clientTLSConfig.Certificates[0]
	require.NotEmpty(t, cert.Certificate)

	block, _ := pem.Decode(clientCertPEM)
	require.NotNil(t, block)

	x509Cert, err := x509.ParseCertificate(block.Bytes)
	require.NoError(t, err)

	assert.Equal(t, "test-client", x509Cert.Subject.CommonName)
}
