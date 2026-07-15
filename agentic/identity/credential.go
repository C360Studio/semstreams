package identity

import (
	"encoding/json"
	"fmt"
	"time"
)

// VerifiableCredential represents a W3C Verifiable Credential.
// See: https://www.w3.org/TR/vc-data-model/
type VerifiableCredential struct {
	// Context specifies the JSON-LD context(s).
	Context []string `json:"@context"`

	// ID is the unique identifier for this credential.
	ID string `json:"id"`

	// Type specifies the credential type(s).
	Type []string `json:"type"`

	// Issuer is the DID of the entity that issued this credential.
	Issuer string `json:"issuer"`

	// IssuanceDate is when the credential was issued.
	IssuanceDate time.Time `json:"issuanceDate"`

	// ExpirationDate is when the credential expires (optional).
	ExpirationDate *time.Time `json:"expirationDate,omitempty"`

	// CredentialSubject contains the claims about the subject.
	CredentialSubject json.RawMessage `json:"credentialSubject"`

	// Proof contains the cryptographic proof (optional).
	Proof *Proof `json:"proof,omitempty"`

	// CredentialStatus contains revocation/status information (optional).
	CredentialStatus *CredentialStatus `json:"credentialStatus,omitempty"`
}

// Proof represents a cryptographic proof for a verifiable credential.
type Proof struct {
	// Type specifies the proof type (e.g., "Ed25519Signature2020").
	Type string `json:"type"`

	// Created is when the proof was created.
	Created time.Time `json:"created"`

	// VerificationMethod is the DID URL of the key used for the proof.
	VerificationMethod string `json:"verificationMethod"`

	// ProofPurpose describes the purpose of the proof.
	ProofPurpose string `json:"proofPurpose"`

	// ProofValue is the actual cryptographic signature.
	ProofValue string `json:"proofValue,omitempty"`

	// JWS is an alternative proof format using JSON Web Signature.
	JWS string `json:"jws,omitempty"`
}

// CredentialStatus represents the status of a credential (e.g., revocation).
type CredentialStatus struct {
	// ID is the URL for checking credential status.
	ID string `json:"id"`

	// Type specifies the status method type.
	Type string `json:"type"`
}

// Common credential types
const (
	// TypeVerifiableCredential is the base type for all VCs.
	TypeVerifiableCredential = "VerifiableCredential"

	// TypeAgentCapabilityCredential is for agent capability attestations.
	TypeAgentCapabilityCredential = "AgentCapabilityCredential"

	// TypeAgentDelegationCredential is for delegation authority.
	TypeAgentDelegationCredential = "AgentDelegationCredential"

	// TypeAgentIdentityCredential is for identity verification.
	TypeAgentIdentityCredential = "AgentIdentityCredential"
)

// Common proof purposes
const (
	// PurposeAssertionMethod means the proof asserts a claim.
	PurposeAssertionMethod = "assertionMethod"

	// PurposeAuthentication means the proof authenticates the subject.
	PurposeAuthentication = "authentication"

	// PurposeCapabilityDelegation means the proof delegates a capability.
	PurposeCapabilityDelegation = "capabilityDelegation"

	// PurposeCapabilityInvocation means the proof invokes a capability.
	PurposeCapabilityInvocation = "capabilityInvocation"
)

// Default context URLs
var (
	// ContextVC is the standard W3C VC context.
	ContextVC = "https://www.w3.org/2018/credentials/v1"
)

// NewVerifiableCredential creates a new verifiable credential.
func NewVerifiableCredential(id, issuer string, credType string, subject any) (*VerifiableCredential, error) {
	subjectData, err := json.Marshal(subject)
	if err != nil {
		return nil, fmt.Errorf("marshal subject: %w", err)
	}

	types := []string{TypeVerifiableCredential}
	if credType != "" && credType != TypeVerifiableCredential {
		types = append(types, credType)
	}

	return &VerifiableCredential{
		Context:           []string{ContextVC},
		ID:                id,
		Type:              types,
		Issuer:            issuer,
		IssuanceDate:      time.Now().UTC(),
		CredentialSubject: subjectData,
	}, nil
}

// Validate checks if the credential is structurally valid.
func (vc *VerifiableCredential) Validate() error {
	if len(vc.Context) == 0 {
		return fmt.Errorf("context is required")
	}
	if vc.ID == "" {
		return fmt.Errorf("id is required")
	}
	if len(vc.Type) == 0 {
		return fmt.Errorf("type is required")
	}
	if vc.Issuer == "" {
		return fmt.Errorf("issuer is required")
	}
	if vc.IssuanceDate.IsZero() {
		return fmt.Errorf("issuanceDate is required")
	}
	if len(vc.CredentialSubject) == 0 {
		return fmt.Errorf("credentialSubject is required")
	}
	return nil
}

// IsExpired checks if the credential has expired.
func (vc *VerifiableCredential) IsExpired() bool {
	if vc.ExpirationDate == nil {
		return false
	}
	return time.Now().After(*vc.ExpirationDate)
}

// HasType checks if the credential has the specified type.
func (vc *VerifiableCredential) HasType(credType string) bool {
	for _, t := range vc.Type {
		if t == credType {
			return true
		}
	}
	return false
}

// GetSubject unmarshals the credential subject into the provided value.
func (vc *VerifiableCredential) GetSubject(v any) error {
	return json.Unmarshal(vc.CredentialSubject, v)
}

// SetSubject marshals and sets the credential subject.
func (vc *VerifiableCredential) SetSubject(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	vc.CredentialSubject = data
	return nil
}

// WithExpiration returns a copy with the specified expiration date.
func (vc *VerifiableCredential) WithExpiration(exp time.Time) *VerifiableCredential {
	vcCopy := *vc
	vcCopy.ExpirationDate = &exp
	return &vcCopy
}

// WithProof returns a copy with the specified proof.
func (vc *VerifiableCredential) WithProof(proof *Proof) *VerifiableCredential {
	vcCopy := *vc
	vcCopy.Proof = proof
	return &vcCopy
}

// AgentCapabilitySubject represents the subject of an agent capability credential.
type AgentCapabilitySubject struct {
	// ID is the DID of the agent.
	ID string `json:"id"`

	// Capability is the capability being attested.
	Capability string `json:"capability"`

	// Confidence is the confidence level (0.0-1.0).
	Confidence float64 `json:"confidence,omitempty"`

	// Scope limits where the capability applies.
	Scope string `json:"scope,omitempty"`
}

// AgentDelegationSubject represents the subject of an agent delegation credential.
type AgentDelegationSubject struct {
	// ID is the DID of the delegate (agent receiving authority).
	ID string `json:"id"`

	// Delegator is the DID of the delegating agent.
	Delegator string `json:"delegator"`

	// Capabilities are the delegated capabilities.
	Capabilities []string `json:"capabilities"`

	// Scope limits where the delegation applies.
	Scope string `json:"scope,omitempty"`

	// ValidUntil is when the delegation expires.
	ValidUntil *time.Time `json:"validUntil,omitempty"`
}

// NewAgentCapabilityCredential creates a new agent capability credential.
func NewAgentCapabilityCredential(id, issuer, agentDID, capability string, confidence float64) (*VerifiableCredential, error) {
	subject := AgentCapabilitySubject{
		ID:         agentDID,
		Capability: capability,
		Confidence: confidence,
	}
	return NewVerifiableCredential(id, issuer, TypeAgentCapabilityCredential, subject)
}

// NewAgentDelegationCredential creates a new agent delegation credential.
func NewAgentDelegationCredential(id, issuer, delegateDID, delegatorDID string, capabilities []string) (*VerifiableCredential, error) {
	subject := AgentDelegationSubject{
		ID:           delegateDID,
		Delegator:    delegatorDID,
		Capabilities: capabilities,
	}
	return NewVerifiableCredential(id, issuer, TypeAgentDelegationCredential, subject)
}
