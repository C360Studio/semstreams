package acme

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config with http-01",
			config: Config{
				DirectoryURL:  "https://step-ca:9000/acme/acme/directory",
				Email:         "admin@semstreams.local",
				Domains:       []string{"semstreams.local"},
				ChallengeType: "http-01",
				RenewBefore:   8 * time.Hour,
				StoragePath:   "/tmp/acme-test",
			},
			wantErr: false,
		},
		{
			name: "valid config with tls-alpn-01",
			config: Config{
				DirectoryURL:  "https://step-ca:9000/acme/acme/directory",
				Email:         "admin@semstreams.local",
				Domains:       []string{"semstreams.local"},
				ChallengeType: "tls-alpn-01",
				RenewBefore:   8 * time.Hour,
				StoragePath:   "/tmp/acme-test",
			},
			wantErr: false,
		},
		{
			name: "missing directory URL",
			config: Config{
				Email:         "admin@semstreams.local",
				Domains:       []string{"semstreams.local"},
				ChallengeType: "http-01",
				StoragePath:   "/tmp/acme-test",
			},
			wantErr: true,
			errMsg:  "directory_url is required",
		},
		{
			name: "missing email",
			config: Config{
				DirectoryURL:  "https://step-ca:9000/acme/acme/directory",
				Domains:       []string{"semstreams.local"},
				ChallengeType: "http-01",
				StoragePath:   "/tmp/acme-test",
			},
			wantErr: true,
			errMsg:  "email is required",
		},
		{
			name: "missing domains",
			config: Config{
				DirectoryURL:  "https://step-ca:9000/acme/acme/directory",
				Email:         "admin@semstreams.local",
				ChallengeType: "http-01",
				StoragePath:   "/tmp/acme-test",
			},
			wantErr: true,
			errMsg:  "at least one domain is required",
		},
		{
			name: "invalid challenge type",
			config: Config{
				DirectoryURL:  "https://step-ca:9000/acme/acme/directory",
				Email:         "admin@semstreams.local",
				Domains:       []string{"semstreams.local"},
				ChallengeType: "dns-01",
				StoragePath:   "/tmp/acme-test",
			},
			wantErr: true,
			errMsg:  "challenge_type must be 'http-01' or 'tls-alpn-01'",
		},
		{
			name: "missing storage path",
			config: Config{
				DirectoryURL:  "https://step-ca:9000/acme/acme/directory",
				Email:         "admin@semstreams.local",
				Domains:       []string{"semstreams.local"},
				ChallengeType: "http-01",
			},
			wantErr: true,
			errMsg:  "storage_path is required",
		},
		{
			name: "default challenge type (empty string)",
			config: Config{
				DirectoryURL: "https://step-ca:9000/acme/acme/directory",
				Email:        "admin@semstreams.local",
				Domains:      []string{"semstreams.local"},
				StoragePath:  "/tmp/acme-test",
			},
			wantErr: false, // Empty string is allowed, defaults to http-01
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestConfig_ValidateDefaults(t *testing.T) {
	config := Config{
		DirectoryURL: "https://step-ca:9000/acme/acme/directory",
		Email:        "admin@semstreams.local",
		Domains:      []string{"semstreams.local"},
		StoragePath:  "/tmp/acme-test",
		RenewBefore:  0, // Should default to 8h
	}

	err := config.Validate()
	require.NoError(t, err)
	assert.Equal(t, 8*time.Hour, config.RenewBefore, "RenewBefore should default to 8 hours")
}

func TestAccount_GetMethods(t *testing.T) {
	account := &Account{
		Email: "test@example.com",
	}

	assert.Equal(t, "test@example.com", account.GetEmail())
	assert.Nil(t, account.GetRegistration())
	assert.Nil(t, account.GetPrivateKey())
}

func TestNewClient_StorageCreation(t *testing.T) {
	// Create temporary directory for test
	tempDir := t.TempDir()
	storagePath := filepath.Join(tempDir, "acme-storage")

	config := Config{
		DirectoryURL:  "https://step-ca:9000/acme/acme/directory",
		Email:         "test@example.com",
		Domains:       []string{"test.local"},
		ChallengeType: "http-01",
		RenewBefore:   8 * time.Hour,
		StoragePath:   storagePath,
	}

	// Note: This will fail because we don't have a real ACME server,
	// but it should create the storage directory
	_, err := NewClient(config)

	// Verify storage directory was created
	info, statErr := os.Stat(storagePath)
	require.NoError(t, statErr, "Storage directory should be created")
	assert.True(t, info.IsDir(), "Storage path should be a directory")

	// The client creation will fail due to no ACME server, but that's expected
	// We're just testing that validation and storage setup work
	if err != nil {
		// Expected - no real ACME server to connect to
		t.Logf("Client creation failed as expected (no ACME server): %v", err)
	}
}

func TestSaveAndLoadAccount(t *testing.T) {
	// This is a placeholder for future account persistence tests
	// Full testing requires mocking the ACME server interaction
	t.Skip("Account save/load requires ACME server mock - see integration tests")
}

func TestObtainCertificate(t *testing.T) {
	// This is a placeholder for future certificate obtainment tests
	// Full testing requires mocking the ACME server
	t.Skip("Certificate obtainment requires ACME server mock - see integration tests")
}

func TestRenewCertificateIfNeeded(t *testing.T) {
	// This is a placeholder for future renewal tests
	// Full testing requires mocking the ACME server and existing certificates
	t.Skip("Certificate renewal requires ACME server mock - see integration tests")
}
