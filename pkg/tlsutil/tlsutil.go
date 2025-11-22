// Package tlsutil provides TLS configuration utilities for secure connections.
package tlsutil

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"time"

	"github.com/c360/semstreams/errors"
	"github.com/c360/semstreams/pkg/acme"
	"github.com/c360/semstreams/pkg/security"
)

// LoadServerTLSConfig creates a tls.Config for HTTP/WebSocket servers from platform config
func LoadServerTLSConfig(cfg security.ServerTLSConfig) (*tls.Config, error) {
	if !cfg.Enabled {
		return nil, nil
	}

	cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
	if err != nil {
		return nil, errors.WrapFatal(err, "tlsutil", "LoadServerTLSConfig", "load certificate")
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   parseTLSVersion(cfg.MinVersion),
	}

	return tlsConfig, nil
}

// LoadClientTLSConfig creates a tls.Config for HTTP/WebSocket clients from platform config
// Always uses system CA bundle first, CAFiles are additional trusted CAs
func LoadClientTLSConfig(cfg security.ClientTLSConfig) (*tls.Config, error) {
	tlsConfig := &tls.Config{
		MinVersion: parseTLSVersion(cfg.MinVersion),
	}

	// Start with system CA pool
	rootCAs, err := x509.SystemCertPool()
	if err != nil {
		// If system pool unavailable, create empty pool
		rootCAs = x509.NewCertPool()
	}

	// Add additional CAs from config
	for _, caFile := range cfg.CAFiles {
		caPEM, err := os.ReadFile(caFile)
		if err != nil {
			return nil, errors.WrapFatal(err, "tlsutil", "LoadClientTLSConfig", fmt.Sprintf("read CA file %s", caFile))
		}
		if !rootCAs.AppendCertsFromPEM(caPEM) {
			return nil, errors.WrapFatal(
				fmt.Errorf("invalid PEM data"),
				"tlsutil",
				"LoadClientTLSConfig",
				fmt.Sprintf("parse CA certificate from %s", caFile),
			)
		}
	}

	tlsConfig.RootCAs = rootCAs

	// Handle InsecureSkipVerify
	// Note: Setting this is intentional via config - operators know the security implications
	if cfg.InsecureSkipVerify {
		tlsConfig.InsecureSkipVerify = true
	}

	return tlsConfig, nil
}

// LoadServerTLSConfigWithMTLS creates a tls.Config for HTTP/WebSocket servers with optional mTLS support
func LoadServerTLSConfigWithMTLS(cfg security.ServerTLSConfig, mtlsCfg security.ServerMTLSConfig) (*tls.Config, error) {
	// Start with base server TLS config
	tlsConfig, err := LoadServerTLSConfig(cfg)
	if err != nil {
		return nil, err
	}

	if !mtlsCfg.Enabled {
		return tlsConfig, nil
	}

	// Apply mTLS configuration
	if err := applyMTLSConfig(tlsConfig, mtlsCfg); err != nil {
		return nil, err
	}

	return tlsConfig, nil
}

// applyMTLSConfig applies mTLS settings to existing tls.Config
func applyMTLSConfig(tlsConfig *tls.Config, mtlsCfg security.ServerMTLSConfig) error {
	// Load client CA certificates for validation
	clientCAs := x509.NewCertPool()
	for _, caFile := range mtlsCfg.ClientCAFiles {
		caPEM, err := os.ReadFile(caFile)
		if err != nil {
			return errors.WrapFatal(err, "tlsutil", "applyMTLSConfig",
				fmt.Sprintf("read client CA file %s", caFile))
		}
		if !clientCAs.AppendCertsFromPEM(caPEM) {
			return errors.WrapFatal(
				fmt.Errorf("invalid PEM data"),
				"tlsutil", "applyMTLSConfig",
				fmt.Sprintf("parse client CA certificate from %s", caFile))
		}
	}

	tlsConfig.ClientCAs = clientCAs
	if mtlsCfg.RequireClientCert {
		tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert
	} else {
		tlsConfig.ClientAuth = tls.VerifyClientCertIfGiven
	}

	// Optional: CN whitelist verification
	if len(mtlsCfg.AllowedClientCNs) > 0 {
		tlsConfig.VerifyPeerCertificate = func(_ [][]byte, verifiedChains [][]*x509.Certificate) error {
			return verifyAllowedClientCN(verifiedChains, mtlsCfg.AllowedClientCNs)
		}
	}

	return nil
}

// verifyAllowedClientCN checks if client certificate CN is in whitelist
func verifyAllowedClientCN(chains [][]*x509.Certificate, allowedCNs []string) error {
	if len(chains) == 0 {
		return fmt.Errorf("no verified certificate chains")
	}

	leafCert := chains[0][0]
	for _, allowedCN := range allowedCNs {
		if leafCert.Subject.CommonName == allowedCN {
			return nil
		}
	}

	return fmt.Errorf("client certificate CN '%s' not in allowed list",
		leafCert.Subject.CommonName)
}

// LoadClientTLSConfigWithMTLS creates a tls.Config for HTTP/WebSocket clients with optional mTLS support
func LoadClientTLSConfigWithMTLS(cfg security.ClientTLSConfig, mtlsCfg security.ClientMTLSConfig) (*tls.Config, error) {
	// Start with base client TLS config
	tlsConfig, err := LoadClientTLSConfig(cfg)
	if err != nil {
		return nil, err
	}

	if !mtlsCfg.Enabled {
		return tlsConfig, nil
	}

	// Load client certificate
	clientCert, err := tls.LoadX509KeyPair(mtlsCfg.CertFile, mtlsCfg.KeyFile)
	if err != nil {
		return nil, errors.WrapFatal(err, "tlsutil", "LoadClientTLSConfigWithMTLS",
			"load client certificate")
	}

	tlsConfig.Certificates = []tls.Certificate{clientCert}

	return tlsConfig, nil
}

// parseTLSVersion converts version string to crypto/tls constant
// Returns tls.VersionTLS12 if empty or invalid
func parseTLSVersion(version string) uint16 {
	switch version {
	case "1.3":
		return tls.VersionTLS13
	case "1.2":
		return tls.VersionTLS12
	default:
		return tls.VersionTLS12 // Safe default
	}
}

// LoadServerTLSConfigWithACME creates a tls.Config with ACME automation
// This function handles certificate obtainment, renewal, and hot-reload.
// If ACME is unavailable, it falls back to manual certificates if configured.
func LoadServerTLSConfigWithACME(ctx context.Context, cfg security.ServerTLSConfig) (*tls.Config, func(), error) {
	// Default to manual mode if not specified
	mode := cfg.Mode
	if mode == "" {
		mode = "manual"
	}

	// If not ACME mode, use standard manual TLS
	if mode != "acme" || !cfg.ACME.Enabled {
		tlsConfig, err := LoadServerTLSConfigWithMTLS(cfg, cfg.MTLS)
		return tlsConfig, func() {}, err
	}

	// Initialize ACME client
	acmeClient, err := initACMEClient(cfg.ACME)
	if err != nil {
		// ACME initialization failed - fall back to manual certificates if configured
		if cfg.CertFile != "" && cfg.KeyFile != "" {
			tlsConfig, fallbackErr := LoadServerTLSConfigWithMTLS(cfg, cfg.MTLS)
			if fallbackErr != nil {
				return nil, nil, errors.WrapFatal(fallbackErr, "tlsutil", "LoadServerTLSConfigWithACME",
					"fallback to manual TLS failed")
			}
			return tlsConfig, func() {}, nil
		}
		return nil, nil, err
	}

	// Obtain or renew certificate via ACME
	cert, _, err := acmeClient.RenewCertificateIfNeeded(ctx)
	if err != nil || cert == nil {
		// No existing cert or renewal failed, obtain new one
		cert, err = acmeClient.ObtainCertificate(ctx)
		if err != nil {
			// ACME failed - fall back to manual certificates if configured
			if cfg.CertFile != "" && cfg.KeyFile != "" {
				tlsConfig, fallbackErr := LoadServerTLSConfigWithMTLS(cfg, cfg.MTLS)
				if fallbackErr != nil {
					return nil, nil, errors.WrapFatal(fallbackErr, "tlsutil", "LoadServerTLSConfigWithACME",
						"fallback to manual TLS after ACME failure")
				}
				return tlsConfig, func() {}, nil
			}
			return nil, nil, errors.WrapTransient(err, "tlsutil", "LoadServerTLSConfigWithACME",
				"obtain ACME certificate")
		}
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{*cert},
		MinVersion:   parseTLSVersion(cfg.MinVersion),
	}

	// Apply mTLS if configured
	if cfg.MTLS.Enabled {
		if err := applyMTLSConfig(tlsConfig, cfg.MTLS); err != nil {
			return nil, nil, err
		}
	}

	// Start background renewal loop
	renewalCtx, cancel := context.WithCancel(ctx)
	renewalDone := make(chan struct{})

	go func() {
		defer close(renewalDone)
		_ = acmeClient.StartRenewalLoop(renewalCtx, 1*time.Hour,
			func(newCert *tls.Certificate) {
				// Hot-reload certificate
				tlsConfig.Certificates = []tls.Certificate{*newCert}
			})
	}()

	// Return cleanup function to stop renewal loop
	cleanup := func() {
		cancel()
		<-renewalDone // Wait for goroutine to exit
	}

	return tlsConfig, cleanup, nil
}

// LoadClientTLSConfigWithACME creates a client tls.Config with ACME automation for mTLS
func LoadClientTLSConfigWithACME(ctx context.Context, cfg security.ClientTLSConfig) (*tls.Config, func(), error) {
	// Default to manual mode if not specified
	mode := cfg.Mode
	if mode == "" {
		mode = "manual"
	}

	// If not ACME mode, use standard manual TLS
	if mode != "acme" || !cfg.ACME.Enabled {
		tlsConfig, err := LoadClientTLSConfigWithMTLS(cfg, cfg.MTLS)
		return tlsConfig, func() {}, err
	}

	// Start with base client TLS config
	tlsConfig, err := LoadClientTLSConfig(cfg)
	if err != nil {
		return nil, nil, err
	}

	// Initialize ACME client for client certificate
	acmeClient, err := initACMEClient(cfg.ACME)
	if err != nil {
		// ACME initialization failed - fall back to manual mTLS if configured
		if cfg.MTLS.Enabled && cfg.MTLS.CertFile != "" && cfg.MTLS.KeyFile != "" {
			tlsConfig, fallbackErr := LoadClientTLSConfigWithMTLS(cfg, cfg.MTLS)
			if fallbackErr != nil {
				return nil, nil, errors.WrapFatal(fallbackErr, "tlsutil", "LoadClientTLSConfigWithACME",
					"fallback to manual client TLS failed")
			}
			return tlsConfig, func() {}, nil
		}
		return nil, nil, err
	}

	// Obtain or renew client certificate via ACME
	cert, _, err := acmeClient.RenewCertificateIfNeeded(ctx)
	if err != nil || cert == nil {
		cert, err = acmeClient.ObtainCertificate(ctx)
		if err != nil {
			// ACME failed - fall back to manual mTLS if configured
			if cfg.MTLS.Enabled && cfg.MTLS.CertFile != "" && cfg.MTLS.KeyFile != "" {
				tlsConfig, fallbackErr := LoadClientTLSConfigWithMTLS(cfg, cfg.MTLS)
				if fallbackErr != nil {
					return nil, nil, errors.WrapFatal(fallbackErr, "tlsutil", "LoadClientTLSConfigWithACME",
						"fallback to manual client TLS after ACME failure")
				}
				return tlsConfig, func() {}, nil
			}
			return nil, nil, errors.WrapTransient(err, "tlsutil", "LoadClientTLSConfigWithACME",
				"obtain ACME client certificate")
		}
	}

	tlsConfig.Certificates = []tls.Certificate{*cert}

	// Start background renewal loop
	renewalCtx, cancel := context.WithCancel(ctx)
	renewalDone := make(chan struct{})

	go func() {
		defer close(renewalDone)
		_ = acmeClient.StartRenewalLoop(renewalCtx, 1*time.Hour,
			func(newCert *tls.Certificate) {
				// Hot-reload certificate
				tlsConfig.Certificates = []tls.Certificate{*newCert}
			})
	}()

	// Return cleanup function
	cleanup := func() {
		cancel()
		<-renewalDone // Wait for goroutine to exit
	}

	return tlsConfig, cleanup, nil
}

// initACMEClient creates an ACME client from security config
func initACMEClient(cfg security.ACMEConfig) (*acme.Client, error) {
	renewBefore, err := time.ParseDuration(cfg.RenewBefore)
	if err != nil {
		renewBefore = 8 * time.Hour // Default
	}

	return acme.NewClient(acme.Config{
		DirectoryURL:  cfg.DirectoryURL,
		Email:         cfg.Email,
		Domains:       cfg.Domains,
		ChallengeType: cfg.ChallengeType,
		RenewBefore:   renewBefore,
		StoragePath:   cfg.StoragePath,
		CABundle:      cfg.CABundle,
	})
}
