package identity

import (
	"fmt"
	"time"
)

// AgentIdentity represents the local DID and credentials of an agent.
// It combines a DID with associated credentials and metadata.
type AgentIdentity struct {
	// DID is the decentralized identifier for this agent.
	DID DID `json:"did"`

	// DisplayName is a human-readable name for the agent.
	DisplayName string `json:"display_name"`

	// Credentials are verifiable credentials held by this agent.
	Credentials []VerifiableCredential `json:"credentials,omitempty"`

	// InternalRole is the agent's role in the local system (preserved for compatibility).
	// Example values: "architect", "editor", "reviewer"
	InternalRole string `json:"internal_role,omitempty"`

	// Created is when this identity was created.
	Created time.Time `json:"created,omitempty"`

	// Updated is when this identity was last updated.
	Updated time.Time `json:"updated,omitempty"`

	// Metadata contains additional identity metadata.
	Metadata map[string]any `json:"metadata,omitempty"`
}

// NewAgentIdentity creates a new agent identity.
func NewAgentIdentity(did DID, displayName string) *AgentIdentity {
	now := time.Now().UTC()
	return &AgentIdentity{
		DID:         did,
		DisplayName: displayName,
		Credentials: []VerifiableCredential{},
		Created:     now,
		Updated:     now,
		Metadata:    make(map[string]any),
	}
}

// Validate checks if the agent identity is valid.
func (ai *AgentIdentity) Validate() error {
	if err := ai.DID.Validate(); err != nil {
		return fmt.Errorf("invalid DID: %w", err)
	}
	if ai.DisplayName == "" {
		return fmt.Errorf("display_name is required")
	}
	// Validate all credentials
	for i, cred := range ai.Credentials {
		if err := cred.Validate(); err != nil {
			return fmt.Errorf("credential[%d]: %w", i, err)
		}
	}
	return nil
}

// AddCredential adds a credential to the identity.
func (ai *AgentIdentity) AddCredential(cred VerifiableCredential) {
	ai.Credentials = append(ai.Credentials, cred)
	ai.Updated = time.Now().UTC()
}

// RemoveCredential removes a credential by ID.
func (ai *AgentIdentity) RemoveCredential(credID string) bool {
	for i, cred := range ai.Credentials {
		if cred.ID == credID {
			ai.Credentials = append(ai.Credentials[:i], ai.Credentials[i+1:]...)
			ai.Updated = time.Now().UTC()
			return true
		}
	}
	return false
}

// GetCredential returns a credential by ID.
func (ai *AgentIdentity) GetCredential(credID string) *VerifiableCredential {
	for i := range ai.Credentials {
		if ai.Credentials[i].ID == credID {
			return &ai.Credentials[i]
		}
	}
	return nil
}

// GetCredentialsByType returns all credentials of the specified type.
func (ai *AgentIdentity) GetCredentialsByType(credType string) []VerifiableCredential {
	var result []VerifiableCredential
	for _, cred := range ai.Credentials {
		if cred.HasType(credType) {
			result = append(result, cred)
		}
	}
	return result
}

// GetValidCredentials returns all non-expired credentials.
func (ai *AgentIdentity) GetValidCredentials() []VerifiableCredential {
	var result []VerifiableCredential
	for _, cred := range ai.Credentials {
		if !cred.IsExpired() {
			result = append(result, cred)
		}
	}
	return result
}

// HasCapability checks if the agent has a capability credential for the given capability.
func (ai *AgentIdentity) HasCapability(capability string) bool {
	capCreds := ai.GetCredentialsByType(TypeAgentCapabilityCredential)
	for _, cred := range capCreds {
		if cred.IsExpired() {
			continue
		}
		var subject AgentCapabilitySubject
		if err := cred.GetSubject(&subject); err == nil {
			if subject.Capability == capability {
				return true
			}
		}
	}
	return false
}

// GetCapabilities returns all capabilities from valid capability credentials.
func (ai *AgentIdentity) GetCapabilities() []string {
	var capabilities []string
	seen := make(map[string]bool)

	capCreds := ai.GetCredentialsByType(TypeAgentCapabilityCredential)
	for _, cred := range capCreds {
		if cred.IsExpired() {
			continue
		}
		var subject AgentCapabilitySubject
		if err := cred.GetSubject(&subject); err == nil {
			if !seen[subject.Capability] {
				capabilities = append(capabilities, subject.Capability)
				seen[subject.Capability] = true
			}
		}
	}
	return capabilities
}

// SetMetadata sets a metadata value.
func (ai *AgentIdentity) SetMetadata(key string, value any) {
	if ai.Metadata == nil {
		ai.Metadata = make(map[string]any)
	}
	ai.Metadata[key] = value
	ai.Updated = time.Now().UTC()
}

// GetMetadata gets a metadata value.
func (ai *AgentIdentity) GetMetadata(key string) (any, bool) {
	if ai.Metadata == nil {
		return nil, false
	}
	v, ok := ai.Metadata[key]
	return v, ok
}

// DIDString returns the string representation of the agent's DID.
func (ai *AgentIdentity) DIDString() string {
	return ai.DID.String()
}

// WithInternalRole returns a copy with the specified internal role.
func (ai *AgentIdentity) WithInternalRole(role string) *AgentIdentity {
	aiCopy := *ai
	aiCopy.InternalRole = role
	aiCopy.Updated = time.Now().UTC()
	return &aiCopy
}
