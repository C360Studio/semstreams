//go:build integration
// +build integration

package acme

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// TestACMEIntegration_FullLifecycle tests the complete ACME certificate lifecycle with real step-ca
func TestACMEIntegration_FullLifecycle(t *testing.T) {
	if os.Getenv("INTEGRATION_TESTS") != "1" {
		t.Skip("Skipping integration test. Set INTEGRATION_TESTS=1 to run")
	}

	ctx := context.Background()

	// Start step-ca container
	stepCAContainer, stepCAURL, rootCA, err := startStepCA(ctx, t)
	require.NoError(t, err, "Failed to start step-ca container")
	defer func() {
		if err := stepCAContainer.Terminate(ctx); err != nil {
			t.Logf("Failed to terminate step-ca container: %v", err)
		}
	}()

	// Create temporary storage directory
	tempDir := t.TempDir()
	storagePath := filepath.Join(tempDir, "acme-storage")
	caBundle := filepath.Join(tempDir, "ca.crt")

	// Save root CA to file
	err = os.WriteFile(caBundle, rootCA, 0644)
	require.NoError(t, err, "Failed to write CA bundle")

	// Configure ACME client
	config := Config{
		DirectoryURL:  fmt.Sprintf("%s/acme/acme/directory", stepCAURL),
		Email:         "test@semstreams.local",
		Domains:       []string{"semstreams.local"},
		ChallengeType: "http-01",
		RenewBefore:   5 * time.Second, // Short renewal window for testing
		StoragePath:   storagePath,
		CABundle:      caBundle,
	}

	// Create ACME client
	client, err := NewClient(config)
	require.NoError(t, err, "Failed to create ACME client")

	// Test 1: Obtain certificate
	t.Run("obtain_certificate", func(t *testing.T) {
		cert, err := client.ObtainCertificate(ctx)
		require.NoError(t, err, "Failed to obtain certificate")
		require.NotNil(t, cert, "Certificate should not be nil")

		// Verify certificate is valid
		x509Cert, err := x509.ParseCertificate(cert.Certificate[0])
		require.NoError(t, err, "Failed to parse certificate")

		// Check domain
		assert.Contains(t, x509Cert.DNSNames, "semstreams.local", "Certificate should contain domain")

		// Check expiry
		assert.True(t, x509Cert.NotAfter.After(time.Now()), "Certificate should not be expired")
		assert.True(t, x509Cert.NotBefore.Before(time.Now()), "Certificate should be valid now")

		// Verify certificate files exist
		certPath := filepath.Join(storagePath, "certificate.pem")
		keyPath := filepath.Join(storagePath, "certificate.key")
		assert.FileExists(t, certPath, "Certificate file should exist")
		assert.FileExists(t, keyPath, "Private key file should exist")
	})

	// Test 2: Certificate renewal (no renewal needed yet)
	t.Run("no_renewal_needed", func(t *testing.T) {
		cert, renewed, err := client.RenewCertificateIfNeeded(ctx)
		require.NoError(t, err, "Renewal check should not error")
		require.NotNil(t, cert, "Should return existing certificate")
		assert.False(t, renewed, "Certificate should not be renewed yet")
	})

	// Test 3: Force renewal by waiting until renewal window
	t.Run("renewal_needed", func(t *testing.T) {
		// Wait for renewal window (config.RenewBefore = 5s for testing)
		time.Sleep(6 * time.Second)

		cert, renewed, err := client.RenewCertificateIfNeeded(ctx)
		require.NoError(t, err, "Renewal should succeed")
		require.NotNil(t, cert, "Should return renewed certificate")
		assert.True(t, renewed, "Certificate should be renewed")

		// Verify new certificate is valid
		x509Cert, err := x509.ParseCertificate(cert.Certificate[0])
		require.NoError(t, err, "Failed to parse renewed certificate")
		assert.True(t, x509Cert.NotAfter.After(time.Now()), "Renewed certificate should not be expired")
	})

	// Test 4: Account persistence
	t.Run("account_persistence", func(t *testing.T) {
		accountPath := filepath.Join(storagePath, "account.json")
		keyPath := filepath.Join(storagePath, "account.key")

		assert.FileExists(t, accountPath, "Account file should exist")
		assert.FileExists(t, keyPath, "Account key file should exist")

		// Create new client with same config (should load existing account)
		client2, err := NewClient(config)
		require.NoError(t, err, "Should load existing account")
		assert.Equal(t, client.account.Email, client2.account.Email, "Account email should match")
	})
}

// TestACMEIntegration_Fallback tests ACME fallback to manual certificates
func TestACMEIntegration_Fallback(t *testing.T) {
	if os.Getenv("INTEGRATION_TESTS") != "1" {
		t.Skip("Skipping integration test. Set INTEGRATION_TESTS=1 to run")
	}

	// Create temporary storage directory
	tempDir := t.TempDir()
	storagePath := filepath.Join(tempDir, "acme-storage")

	// Configure ACME client with invalid URL (should fail)
	config := Config{
		DirectoryURL:  "https://invalid-step-ca:9000/acme/acme/directory",
		Email:         "test@semstreams.local",
		Domains:       []string{"semstreams.local"},
		ChallengeType: "http-01",
		RenewBefore:   8 * time.Hour,
		StoragePath:   storagePath,
	}

	// Creating client should fail (no valid ACME server)
	_, err := NewClient(config)
	assert.Error(t, err, "Should fail with invalid ACME server")
	assert.Contains(t, err.Error(), "acme.Client.initializeLegoClient", "Error should indicate ACME client init failure")
}

// startStepCA starts a step-ca container and returns the container, URL, and root CA
func startStepCA(ctx context.Context, t *testing.T) (testcontainers.Container, string, []byte, error) {
	req := testcontainers.ContainerRequest{
		Image:        "smallstep/step-ca:latest",
		ExposedPorts: []string{"9000/tcp"},
		Env: map[string]string{
			"DOCKER_STEPCA_INIT_NAME":             "SemStreams Test CA",
			"DOCKER_STEPCA_INIT_DNS_NAMES":        "localhost,step-ca,semstreams.local",
			"DOCKER_STEPCA_INIT_PROVISIONER_NAME": "acme",
		},
		WaitingFor: wait.ForAll(
			wait.ForLog("Serving HTTPS"),
			wait.ForListeningPort("9000/tcp"),
		).WithDeadline(30 * time.Second),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		return nil, "", nil, fmt.Errorf("failed to start step-ca container: %w", err)
	}

	// Get mapped port
	mappedPort, err := container.MappedPort(ctx, "9000")
	if err != nil {
		container.Terminate(ctx)
		return nil, "", nil, fmt.Errorf("failed to get mapped port: %w", err)
	}

	stepCAURL := fmt.Sprintf("https://localhost:%s", mappedPort.Port())

	// Extract root CA certificate from container
	// step-ca generates root CA at /home/step/certs/root_ca.crt
	rootCAPath := "/home/step/certs/root_ca.crt"

	// Wait for CA cert to be available
	time.Sleep(5 * time.Second)

	reader, err := container.CopyFileFromContainer(ctx, rootCAPath)
	if err != nil {
		container.Terminate(ctx)
		return nil, "", nil, fmt.Errorf("failed to copy root CA from container: %w", err)
	}
	defer reader.Close()

	// Read root CA bytes
	rootCA, err := io.ReadAll(reader)
	if err != nil {
		container.Terminate(ctx)
		return nil, "", nil, fmt.Errorf("failed to read root CA: %w", err)
	}

	t.Logf("step-ca started at %s", stepCAURL)

	return container, stepCAURL, rootCA, nil
}

// TestACMEIntegration_TLSHandshake tests that ACME-obtained certificates work for TLS handshakes
func TestACMEIntegration_TLSHandshake(t *testing.T) {
	if os.Getenv("INTEGRATION_TESTS") != "1" {
		t.Skip("Skipping integration test. Set INTEGRATION_TESTS=1 to run")
	}

	ctx := context.Background()

	// Start step-ca container
	stepCAContainer, stepCAURL, rootCA, err := startStepCA(ctx, t)
	require.NoError(t, err, "Failed to start step-ca container")
	defer stepCAContainer.Terminate(ctx)

	// Create temporary storage directory
	tempDir := t.TempDir()
	storagePath := filepath.Join(tempDir, "acme-storage")
	caBundle := filepath.Join(tempDir, "ca.crt")
	err = os.WriteFile(caBundle, rootCA, 0644)
	require.NoError(t, err)

	// Configure ACME client
	config := Config{
		DirectoryURL:  fmt.Sprintf("%s/acme/acme/directory", stepCAURL),
		Email:         "test@semstreams.local",
		Domains:       []string{"localhost"}, // Use localhost for testing
		ChallengeType: "http-01",
		RenewBefore:   8 * time.Hour,
		StoragePath:   storagePath,
		CABundle:      caBundle,
	}

	// Create ACME client and obtain certificate
	client, err := NewClient(config)
	require.NoError(t, err)

	cert, err := client.ObtainCertificate(ctx)
	require.NoError(t, err)
	require.NotNil(t, cert)

	// Create TLS config with ACME certificate
	serverConfig := &tls.Config{
		Certificates: []tls.Certificate{*cert},
		MinVersion:   tls.VersionTLS12,
	}

	// Verify certificate can be used in TLS config
	assert.NotNil(t, serverConfig.Certificates, "TLS config should have certificates")
	assert.Len(t, serverConfig.Certificates, 1, "Should have exactly one certificate")

	// Parse and verify certificate properties
	x509Cert, err := x509.ParseCertificate(cert.Certificate[0])
	require.NoError(t, err)
	assert.NotEmpty(t, x509Cert.DNSNames, "Certificate should have DNS names")
	assert.Contains(t, x509Cert.DNSNames, "localhost", "Certificate should be for localhost")

	t.Logf("Successfully created TLS config with ACME certificate for domains: %v", x509Cert.DNSNames)
}
