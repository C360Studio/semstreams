package identity

import (
	"context"
)

// Provider defines the interface for creating and managing agent identities.
type Provider interface {
	// CreateIdentity creates a new agent identity.
	CreateIdentity(ctx context.Context, opts CreateIdentityOptions) (*AgentIdentity, error)

	// ResolveIdentity resolves a DID to an agent identity.
	ResolveIdentity(ctx context.Context, did DID) (*AgentIdentity, error)

	// IssueCredential issues a verifiable credential for the subject.
	IssueCredential(ctx context.Context, subject DID, credType string, claims any) (*VerifiableCredential, error)

	// VerifyCredential verifies a credential's signature and validity.
	VerifyCredential(ctx context.Context, cred *VerifiableCredential) (bool, error)

	// UpdateIdentity updates an existing identity.
	UpdateIdentity(ctx context.Context, identity *AgentIdentity) error

	// DeleteIdentity removes an identity.
	DeleteIdentity(ctx context.Context, did DID) error
}

// CreateIdentityOptions configures identity creation.
type CreateIdentityOptions struct {
	// DisplayName is the human-readable name for the agent.
	DisplayName string

	// InternalRole is the agent's role in the local system.
	InternalRole string

	// Method specifies which DID method to use.
	// Defaults to "key" for local provider.
	Method string

	// Metadata contains additional identity metadata.
	Metadata map[string]any

	// InitialCapabilities are capabilities to attest via credentials.
	InitialCapabilities []string
}

// Validate validates the options.
func (o *CreateIdentityOptions) Validate() error {
	if o.DisplayName == "" {
		return ErrDisplayNameRequired
	}
	return nil
}

// ProviderConfig holds configuration for identity providers.
type ProviderConfig struct {
	// ProviderType identifies the provider. The core implementation is "local".
	ProviderType string `json:"provider_type"`

	// IssuerDID is the DID used for issuing credentials.
	IssuerDID string `json:"issuer_did,omitempty"`

	// KeyStorePath is the path to store private keys (for local provider).
	KeyStorePath string `json:"key_store_path,omitempty"`
}

// ProviderFactory creates an identity provider based on configuration.
type ProviderFactory func(config ProviderConfig) (Provider, error)

// DefaultProviderFactory creates providers based on provider type.
var DefaultProviderFactory ProviderFactory = func(config ProviderConfig) (Provider, error) {
	switch config.ProviderType {
	case "local", "":
		return NewLocalProvider(config)
	default:
		return nil, ErrUnknownProviderType
	}
}
