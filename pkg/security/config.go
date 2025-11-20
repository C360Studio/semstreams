// Package security provides platform-wide security configuration types
package security

// Config holds platform-wide security configuration
type Config struct {
	TLS TLSConfig `json:"tls,omitempty"`
}

// TLSConfig holds TLS configuration for HTTP/WebSocket servers and clients
type TLSConfig struct {
	Server ServerTLSConfig `json:"server,omitempty"`
	Client ClientTLSConfig `json:"client,omitempty"`
}

// ACMEConfig holds ACME client configuration for automated certificate management
type ACMEConfig struct {
	Enabled       bool     `json:"enabled"`
	DirectoryURL  string   `json:"directory_url,omitempty"`  // step-ca ACME directory
	Email         string   `json:"email,omitempty"`          // Contact email
	Domains       []string `json:"domains,omitempty"`        // Domains for certificate
	ChallengeType string   `json:"challenge_type,omitempty"` // "http-01" or "tls-alpn-01"
	RenewBefore   string   `json:"renew_before,omitempty"`   // Duration string (e.g., "8h")
	StoragePath   string   `json:"storage_path,omitempty"`   // Certificate storage path
	CABundle      string   `json:"ca_bundle,omitempty"`      // Optional: CA cert for step-ca
}

// ServerMTLSConfig holds mTLS configuration for servers (client certificate validation)
type ServerMTLSConfig struct {
	Enabled           bool     `json:"enabled"`
	ClientCAFiles     []string `json:"client_ca_files,omitempty"`     // CA certs to trust for client validation
	RequireClientCert bool     `json:"require_client_cert,omitempty"` // true = require, false = optional
	AllowedClientCNs  []string `json:"allowed_client_cns,omitempty"`  // Optional CN whitelist
}

// ServerTLSConfig holds TLS configuration for HTTP/WebSocket servers
type ServerTLSConfig struct {
	Enabled    bool   `json:"enabled"`
	Mode       string `json:"mode,omitempty"` // "manual" (default) or "acme"
	CertFile   string `json:"cert_file,omitempty"`
	KeyFile    string `json:"key_file,omitempty"`
	MinVersion string `json:"min_version,omitempty"` // "1.2" or "1.3"

	// ACME mode (Tier 3)
	ACME ACMEConfig `json:"acme,omitempty"`

	// mTLS support (both modes)
	MTLS ServerMTLSConfig `json:"mtls,omitempty"`
}

// ClientMTLSConfig holds mTLS configuration for clients (client certificate provision)
type ClientMTLSConfig struct {
	Enabled  bool   `json:"enabled"`
	CertFile string `json:"cert_file,omitempty"` // Client certificate
	KeyFile  string `json:"key_file,omitempty"`  // Client private key
}

// ClientTLSConfig holds TLS configuration for HTTP/WebSocket clients
// Always uses system CA bundle first, CAFiles are ADDITIONAL trusted CAs
type ClientTLSConfig struct {
	Mode               string   `json:"mode,omitempty"` // "manual" (default) or "acme"
	CAFiles            []string `json:"ca_files,omitempty"`
	InsecureSkipVerify bool     `json:"insecure_skip_verify,omitempty"` // DEV/TEST ONLY
	MinVersion         string   `json:"min_version,omitempty"`

	// ACME mode (Tier 3)
	ACME ACMEConfig `json:"acme,omitempty"`

	// mTLS support (both modes)
	MTLS ClientMTLSConfig `json:"mtls,omitempty"`
}
