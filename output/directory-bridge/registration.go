package directorybridge

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/c360studio/semstreams/agentic/identity"
	oasfgenerator "github.com/c360studio/semstreams/processor/oasf-generator"
)

// RegistrationManager handles the lifecycle of agent registrations.
// It owns the heartbeat scheduler and the entityID→Registration map; the
// concrete protocol/wire format is delegated to a Backend.
type RegistrationManager struct {
	backend          Backend
	identityProvider identity.Provider
	config           Config
	logger           *slog.Logger

	// Active registrations
	registrations map[string]*Registration // entityID -> registration
	mu            sync.RWMutex

	// Heartbeat management
	heartbeatStop chan struct{}
	heartbeatWg   sync.WaitGroup
	stopOnce      sync.Once
	started       bool
	startMu       sync.Mutex
}

// Registration represents an active directory registration.
type Registration struct {
	// EntityID is the SemStreams entity ID.
	EntityID string `json:"entity_id"`

	// RegistrationID is the directory's registration ID.
	RegistrationID string `json:"registration_id"`

	// AgentDID is the agent's decentralized identifier.
	AgentDID string `json:"agent_did"`

	// OASFRecord is the agent's OASF specification.
	OASFRecord *oasfgenerator.OASFRecord `json:"oasf_record"`

	// RegisteredAt is when the registration was created.
	RegisteredAt time.Time `json:"registered_at"`

	// ExpiresAt is when the registration expires.
	ExpiresAt time.Time `json:"expires_at"`

	// LastHeartbeat is when the last heartbeat was sent.
	LastHeartbeat time.Time `json:"last_heartbeat"`

	// Retries is the number of registration retries.
	Retries int `json:"retries"`
}

// NewRegistrationManager creates a new registration manager bound to the
// given Backend. Callers wrap a *DirectoryClient via NewHTTPBackend or
// NewHTTPBackendFromClient when constructing for the current HTTP wire
// format.
func NewRegistrationManager(backend Backend, identityProvider identity.Provider, config Config, logger *slog.Logger) *RegistrationManager {
	return &RegistrationManager{
		backend:          backend,
		identityProvider: identityProvider,
		config:           config,
		logger:           logger,
		registrations:    make(map[string]*Registration),
	}
}

// Start begins the heartbeat goroutine.
func (rm *RegistrationManager) Start(ctx context.Context) error {
	rm.startMu.Lock()
	defer rm.startMu.Unlock()

	if rm.started {
		return nil // Already started
	}

	rm.heartbeatStop = make(chan struct{})
	rm.stopOnce = sync.Once{} // Reset for potential restart
	rm.heartbeatWg.Add(1)
	go rm.heartbeatLoop(ctx)
	rm.started = true
	return nil
}

// Stop stops the heartbeat goroutine and deregisters all agents.
func (rm *RegistrationManager) Stop(ctx context.Context) error {
	rm.startMu.Lock()
	if !rm.started {
		rm.startMu.Unlock()
		return nil
	}
	rm.started = false
	rm.startMu.Unlock()

	// Use sync.Once to safely close the channel exactly once
	rm.stopOnce.Do(func() {
		if rm.heartbeatStop != nil {
			close(rm.heartbeatStop)
		}
	})
	rm.heartbeatWg.Wait()

	// Deregister all agents
	rm.mu.RLock()
	registrations := make([]*Registration, 0, len(rm.registrations))
	for _, reg := range rm.registrations {
		registrations = append(registrations, reg)
	}
	rm.mu.RUnlock()

	for _, reg := range registrations {
		if err := rm.Deregister(ctx, reg.EntityID); err != nil {
			rm.logger.Warn("Failed to deregister agent on shutdown",
				slog.String("entity_id", reg.EntityID),
				slog.Any("error", err))
		}
	}

	return nil
}

// RegisterAgent registers an agent with the directory.
func (rm *RegistrationManager) RegisterAgent(ctx context.Context, entityID string, record *oasfgenerator.OASFRecord, agentIdentity *identity.AgentIdentity) error {
	// Get or create DID
	var agentDID string
	if agentIdentity != nil {
		agentDID = agentIdentity.DIDString()
	} else if rm.identityProvider != nil {
		// Create identity for the agent
		newIdentity, err := rm.identityProvider.CreateIdentity(ctx, identity.CreateIdentityOptions{
			DisplayName: record.Name,
		})
		if err != nil {
			return err
		}
		agentDID = newIdentity.DIDString()
	}

	// Publish via the backend.
	result, err := rm.backend.Publish(ctx, &PublishRequest{
		EntityID: entityID,
		AgentDID: agentDID,
		Record:   record,
		TTL:      rm.config.GetRegistrationTTL(),
		Metadata: map[string]any{
			"semstreams_entity_id": entityID,
			"source":               "semstreams",
		},
	})
	if err != nil {
		return err
	}

	// Store registration
	registration := &Registration{
		EntityID:       entityID,
		RegistrationID: result.RecordID,
		AgentDID:       agentDID,
		OASFRecord:     record,
		RegisteredAt:   time.Now(),
		ExpiresAt:      result.ExpiresAt,
		LastHeartbeat:  time.Now(),
	}

	rm.mu.Lock()
	rm.registrations[entityID] = registration
	rm.mu.Unlock()

	rm.logger.Info("Registered agent with directory",
		slog.String("entity_id", entityID),
		slog.String("registration_id", result.RecordID),
		slog.String("agent_did", agentDID))

	return nil
}

// UpdateRegistration updates an existing registration with new OASF data.
func (rm *RegistrationManager) UpdateRegistration(ctx context.Context, entityID string, record *oasfgenerator.OASFRecord) error {
	rm.mu.RLock()
	existing, ok := rm.registrations[entityID]
	rm.mu.RUnlock()

	if !ok {
		// Not registered yet, register now
		return rm.RegisterAgent(ctx, entityID, record, nil)
	}

	// Re-publish via the backend with the updated OASF record.
	result, err := rm.backend.Publish(ctx, &PublishRequest{
		EntityID: entityID,
		AgentDID: existing.AgentDID,
		Record:   record,
		TTL:      rm.config.GetRegistrationTTL(),
		Metadata: map[string]any{
			"semstreams_entity_id": entityID,
			"source":               "semstreams",
		},
	})
	if err != nil {
		return err
	}

	// Update registration
	rm.mu.Lock()
	existing.OASFRecord = record
	existing.RegistrationID = result.RecordID
	existing.ExpiresAt = result.ExpiresAt
	rm.mu.Unlock()

	rm.logger.Debug("Updated agent registration",
		slog.String("entity_id", entityID),
		slog.String("registration_id", result.RecordID))

	return nil
}

// Deregister removes an agent from the directory.
func (rm *RegistrationManager) Deregister(ctx context.Context, entityID string) error {
	rm.mu.Lock()
	registration, ok := rm.registrations[entityID]
	if ok {
		delete(rm.registrations, entityID)
	}
	rm.mu.Unlock()

	if !ok {
		return nil // Not registered
	}

	err := rm.backend.Withdraw(ctx, &WithdrawRequest{
		RecordID: registration.RegistrationID,
		AgentDID: registration.AgentDID,
	})
	if err != nil {
		return err
	}

	rm.logger.Info("Deregistered agent from directory",
		slog.String("entity_id", entityID),
		slog.String("registration_id", registration.RegistrationID))

	return nil
}

// GetRegistration returns the registration for an entity.
func (rm *RegistrationManager) GetRegistration(entityID string) *Registration {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	return rm.registrations[entityID]
}

// ListRegistrations returns all active registrations.
func (rm *RegistrationManager) ListRegistrations() []*Registration {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	result := make([]*Registration, 0, len(rm.registrations))
	for _, reg := range rm.registrations {
		result = append(result, reg)
	}
	return result
}

// heartbeatLoop sends periodic heartbeats to maintain registrations.
func (rm *RegistrationManager) heartbeatLoop(ctx context.Context) {
	defer rm.heartbeatWg.Done()

	ticker := time.NewTicker(rm.config.GetHeartbeatInterval())
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-rm.heartbeatStop:
			return
		case <-ticker.C:
			rm.sendHeartbeats(ctx)
		}
	}
}

// sendHeartbeats sends heartbeats for all registrations.
func (rm *RegistrationManager) sendHeartbeats(ctx context.Context) {
	rm.mu.RLock()
	registrations := make([]*Registration, 0, len(rm.registrations))
	for _, reg := range rm.registrations {
		registrations = append(registrations, reg)
	}
	rm.mu.RUnlock()

	for _, reg := range registrations {
		// Backends that don't expire records (CID-anchored — see Backend
		// interface contract on PublishResult.ExpiresAt) set ExpiresAt to
		// the zero value. Skip heartbeat entirely for those; otherwise
		// the time.Until below would return a large-negative duration and
		// fire a pointless Refresh every interval.
		if reg.ExpiresAt.IsZero() {
			continue
		}
		// Check if heartbeat is needed (expiry approaching)
		if time.Until(reg.ExpiresAt) > rm.config.GetHeartbeatInterval()*2 {
			continue
		}

		result, err := rm.backend.Refresh(ctx, &RefreshRequest{
			RecordID: reg.RegistrationID,
			AgentDID: reg.AgentDID,
		})
		if err != nil {
			// Self-healing path for CID-anchored backends: a non-zero
			// ExpiresAt got into the map (operator forge, future bug),
			// the backend's Refresh returns ErrRefreshNotSupported, and
			// without intervention we'd log "Heartbeat failed" forever
			// at every interval. Zero out ExpiresAt so the IsZero guard
			// above starts skipping this registration cleanly.
			if errors.Is(err, ErrRefreshNotSupported) {
				rm.logger.Warn("Backend does not support refresh; clearing expiry to stop further heartbeat attempts",
					slog.String("entity_id", reg.EntityID))
				rm.mu.Lock()
				reg.ExpiresAt = time.Time{}
				rm.mu.Unlock()
				continue
			}
			rm.logger.Warn("Heartbeat failed",
				slog.String("entity_id", reg.EntityID),
				slog.Any("error", err))
			continue
		}

		rm.mu.Lock()
		reg.LastHeartbeat = time.Now()
		reg.ExpiresAt = result.ExpiresAt
		rm.mu.Unlock()

		rm.logger.Debug("Heartbeat sent",
			slog.String("entity_id", reg.EntityID),
			slog.Time("expires_at", result.ExpiresAt))
	}
}

// RegistrationError represents a registration failure.
type RegistrationError struct {
	EntityID string
	Message  string
}

func (e *RegistrationError) Error() string {
	return "registration failed for " + e.EntityID + ": " + e.Message
}
