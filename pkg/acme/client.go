// Package acme provides ACME client functionality for automated certificate management
package acme

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/c360/semstreams/errors"
	"github.com/go-acme/lego/v4/certcrypto"
	"github.com/go-acme/lego/v4/certificate"
	"github.com/go-acme/lego/v4/challenge/http01"
	"github.com/go-acme/lego/v4/challenge/tlsalpn01"
	"github.com/go-acme/lego/v4/lego"
	"github.com/go-acme/lego/v4/registration"
)

// Client manages ACME certificate lifecycle
type Client struct {
	config     Config
	legoClient *lego.Client
	account    *Account
}

// Config holds ACME client configuration
type Config struct {
	DirectoryURL  string
	Email         string
	Domains       []string
	ChallengeType string
	RenewBefore   time.Duration
	StoragePath   string
	CABundle      string
}

// Account represents ACME account registration
type Account struct {
	Email        string                 `json:"email"`
	Registration *registration.Resource `json:"registration"`
	key          crypto.PrivateKey
}

// GetEmail returns the account email address
func (a *Account) GetEmail() string {
	return a.Email
}

// GetRegistration returns the ACME registration resource
func (a *Account) GetRegistration() *registration.Resource {
	return a.Registration
}

// GetPrivateKey returns the account private key
func (a *Account) GetPrivateKey() crypto.PrivateKey {
	return a.key
}

// Validate checks if the ACME configuration is valid
func (c *Config) Validate() error {
	if c.DirectoryURL == "" {
		return errors.WrapInvalid(
			fmt.Errorf("directory_url is required"),
			"acme.Config", "Validate", "check directory URL")
	}
	if c.Email == "" {
		return errors.WrapInvalid(
			fmt.Errorf("email is required"),
			"acme.Config", "Validate", "check email")
	}
	if len(c.Domains) == 0 {
		return errors.WrapInvalid(
			fmt.Errorf("at least one domain is required"),
			"acme.Config", "Validate", "check domains")
	}
	if c.ChallengeType != "http-01" && c.ChallengeType != "tls-alpn-01" && c.ChallengeType != "" {
		return errors.WrapInvalid(
			fmt.Errorf("challenge_type must be 'http-01' or 'tls-alpn-01'"),
			"acme.Config", "Validate", "check challenge type")
	}
	if c.StoragePath == "" {
		return errors.WrapInvalid(
			fmt.Errorf("storage_path is required"),
			"acme.Config", "Validate", "check storage path")
	}
	if c.RenewBefore <= 0 {
		c.RenewBefore = 8 * time.Hour // Default
	}
	return nil
}

// NewClient creates a new ACME client
func NewClient(cfg Config) (*Client, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	// Ensure storage directory exists
	if err := os.MkdirAll(cfg.StoragePath, 0700); err != nil {
		return nil, errors.WrapFatal(err, "acme.Client", "NewClient", "create storage directory")
	}

	client := &Client{
		config: cfg,
	}

	// Load or create ACME account
	if err := client.loadOrCreateAccount(); err != nil {
		return nil, err
	}

	// Initialize lego client
	if err := client.initializeLegoClient(); err != nil {
		return nil, err
	}

	return client, nil
}

// loadOrCreateAccount loads existing account or creates a new one
func (c *Client) loadOrCreateAccount() error {
	accountPath := filepath.Join(c.config.StoragePath, "account.json")
	keyPath := filepath.Join(c.config.StoragePath, "account.key")

	// Check if account exists
	if _, err := os.Stat(accountPath); err == nil {
		// Load existing account
		accountData, err := os.ReadFile(accountPath)
		if err != nil {
			return errors.WrapFatal(err, "acme.Client", "loadOrCreateAccount", "read account file")
		}

		var account Account
		if err := json.Unmarshal(accountData, &account); err != nil {
			return errors.WrapFatal(err, "acme.Client", "loadOrCreateAccount", "unmarshal account")
		}

		// Load private key
		keyData, err := os.ReadFile(keyPath)
		if err != nil {
			return errors.WrapFatal(err, "acme.Client", "loadOrCreateAccount", "read key file")
		}

		privateKey, err := certcrypto.ParsePEMPrivateKey(keyData)
		if err != nil {
			return errors.WrapFatal(err, "acme.Client", "loadOrCreateAccount", "parse private key")
		}

		account.key = privateKey
		c.account = &account

		return nil
	}

	// Create new account
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return errors.WrapFatal(err, "acme.Client", "loadOrCreateAccount", "generate private key")
	}

	c.account = &Account{
		Email: c.config.Email,
		key:   privateKey,
	}

	// Save account (registration will be populated later)
	return c.saveAccount()
}

// saveAccount persists the ACME account to disk
func (c *Client) saveAccount() error {
	accountPath := filepath.Join(c.config.StoragePath, "account.json")
	keyPath := filepath.Join(c.config.StoragePath, "account.key")

	// Marshal account
	accountData, err := json.MarshalIndent(c.account, "", "  ")
	if err != nil {
		return errors.WrapFatal(err, "acme.Client", "saveAccount", "marshal account")
	}

	if err := os.WriteFile(accountPath, accountData, 0600); err != nil {
		return errors.WrapFatal(err, "acme.Client", "saveAccount", "write account file")
	}

	// Save private key
	keyData := certcrypto.PEMEncode(c.account.key)

	if err := os.WriteFile(keyPath, keyData, 0600); err != nil {
		return errors.WrapFatal(err, "acme.Client", "saveAccount", "write key file")
	}

	return nil
}

// initializeLegoClient creates and configures the lego ACME client
func (c *Client) initializeLegoClient() error {
	config := lego.NewConfig(c.account)
	config.CADirURL = c.config.DirectoryURL
	config.Certificate.KeyType = certcrypto.EC256

	// Custom HTTP client if CA bundle is specified
	if c.config.CABundle != "" {
		caCert, err := os.ReadFile(c.config.CABundle)
		if err != nil {
			return errors.WrapFatal(err, "acme.Client", "initializeLegoClient", "read CA bundle")
		}

		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			return errors.WrapFatal(
				fmt.Errorf("failed to parse CA certificate"),
				"acme.Client", "initializeLegoClient", "parse CA bundle")
		}

		config.HTTPClient = &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					RootCAs: caCertPool,
				},
			},
		}
	}

	client, err := lego.NewClient(config)
	if err != nil {
		return errors.WrapFatal(err, "acme.Client", "initializeLegoClient", "create lego client")
	}

	// Set up challenge provider
	challengeType := c.config.ChallengeType
	if challengeType == "" {
		challengeType = "http-01" // Default
	}

	switch challengeType {
	case "http-01":
		if err := client.Challenge.SetHTTP01Provider(http01.NewProviderServer("", "80")); err != nil {
			return errors.WrapFatal(err, "acme.Client", "initializeLegoClient", "setup HTTP-01 challenge")
		}
	case "tls-alpn-01":
		if err := client.Challenge.SetTLSALPN01Provider(tlsalpn01.NewProviderServer("", "443")); err != nil {
			return errors.WrapFatal(err, "acme.Client", "initializeLegoClient", "setup TLS-ALPN-01 challenge")
		}
	default:
		return errors.WrapInvalid(
			fmt.Errorf("unsupported challenge type: %s", challengeType),
			"acme.Client", "initializeLegoClient", "setup challenge provider")
	}

	// Register account if not already registered
	if c.account.Registration == nil {
		reg, err := client.Registration.Register(registration.RegisterOptions{TermsOfServiceAgreed: true})
		if err != nil {
			return errors.WrapTransient(err, "acme.Client", "initializeLegoClient", "register account")
		}
		c.account.Registration = reg

		// Save updated account with registration
		if err := c.saveAccount(); err != nil {
			return err
		}
	}

	c.legoClient = client
	return nil
}

// ObtainCertificate requests a new certificate from ACME server
func (c *Client) ObtainCertificate(_ context.Context) (*tls.Certificate, error) {
	request := certificate.ObtainRequest{
		Domains: c.config.Domains,
		Bundle:  true,
	}

	certificates, err := c.legoClient.Certificate.Obtain(request)
	if err != nil {
		return nil, errors.WrapTransient(err, "acme.Client", "ObtainCertificate", "obtain certificate")
	}

	// Save certificate to storage
	certPath := filepath.Join(c.config.StoragePath, "certificate.pem")
	keyPath := filepath.Join(c.config.StoragePath, "certificate.key")

	if err := os.WriteFile(certPath, certificates.Certificate, 0644); err != nil {
		return nil, errors.WrapFatal(err, "acme.Client", "ObtainCertificate", "write certificate")
	}

	if err := os.WriteFile(keyPath, certificates.PrivateKey, 0600); err != nil {
		return nil, errors.WrapFatal(err, "acme.Client", "ObtainCertificate", "write private key")
	}

	// Load as tls.Certificate
	tlsCert, err := tls.X509KeyPair(certificates.Certificate, certificates.PrivateKey)
	if err != nil {
		return nil, errors.WrapFatal(err, "acme.Client", "ObtainCertificate", "load certificate")
	}

	return &tlsCert, nil
}

// RenewCertificateIfNeeded checks expiry and renews if necessary
func (c *Client) RenewCertificateIfNeeded(_ context.Context) (*tls.Certificate, bool, error) {
	certPath := filepath.Join(c.config.StoragePath, "certificate.pem")
	keyPath := filepath.Join(c.config.StoragePath, "certificate.key")

	// Check if certificate exists
	if _, err := os.Stat(certPath); os.IsNotExist(err) {
		return nil, false, nil // No certificate, caller should obtain
	}

	// Load existing certificate
	tlsCert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, false, errors.WrapFatal(err, "acme.Client", "RenewCertificateIfNeeded",
			"load existing certificate")
	}

	// Parse to check expiry
	cert, err := x509.ParseCertificate(tlsCert.Certificate[0])
	if err != nil {
		return nil, false, errors.WrapFatal(err, "acme.Client", "RenewCertificateIfNeeded",
			"parse certificate")
	}

	// Check if renewal needed
	renewalTime := cert.NotAfter.Add(-c.config.RenewBefore)
	if time.Now().Before(renewalTime) {
		return &tlsCert, false, nil // No renewal needed
	}

	// Renew certificate
	certData, err := os.ReadFile(certPath)
	if err != nil {
		return nil, false, errors.WrapFatal(err, "acme.Client", "RenewCertificateIfNeeded",
			"read certificate for renewal")
	}

	certResource := certificate.Resource{
		Domain:      c.config.Domains[0],
		Certificate: certData,
	}

	renewed, err := c.legoClient.Certificate.Renew(certResource, true, false, "")
	if err != nil {
		return nil, false, errors.WrapTransient(err, "acme.Client", "RenewCertificateIfNeeded",
			"renew certificate")
	}

	// Save renewed certificate
	if err := os.WriteFile(certPath, renewed.Certificate, 0644); err != nil {
		return nil, false, errors.WrapFatal(err, "acme.Client", "RenewCertificateIfNeeded",
			"write renewed certificate")
	}

	if err := os.WriteFile(keyPath, renewed.PrivateKey, 0600); err != nil {
		return nil, false, errors.WrapFatal(err, "acme.Client", "RenewCertificateIfNeeded",
			"write renewed private key")
	}

	// Load renewed certificate
	renewedTLS, err := tls.X509KeyPair(renewed.Certificate, renewed.PrivateKey)
	if err != nil {
		return nil, false, errors.WrapFatal(err, "acme.Client", "RenewCertificateIfNeeded",
			"load renewed certificate")
	}

	return &renewedTLS, true, nil
}

// StartRenewalLoop runs background renewal checks
func (c *Client) StartRenewalLoop(ctx context.Context, checkInterval time.Duration,
	onRenewal func(*tls.Certificate)) error {
	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			cert, renewed, err := c.RenewCertificateIfNeeded(ctx)
			if err != nil {
				// Log error but continue (transient failures shouldn't crash service)
				// TODO: Add structured logging when available
				continue
			}

			if renewed && onRenewal != nil {
				onRenewal(cert)
			}
		}
	}
}
