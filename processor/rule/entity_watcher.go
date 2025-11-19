package rule

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/c360/semstreams/errors"
	"github.com/c360/semstreams/message"
	gtypes "github.com/c360/semstreams/types/graph"
	"github.com/nats-io/nats.go/jetstream"
)

// watchEntityStates creates KV watchers for entity state changes
func (rp *Processor) watchEntityStates(ctx context.Context) error {
	// Skip if no entity watch patterns configured
	if len(rp.config.EntityWatchPatterns) == 0 {
		rp.logger.Info("No entity watch patterns configured, skipping KV watch setup")
		return nil
	}

	// Get ENTITY_STATES bucket
	// Create or get ENTITY_STATES bucket using resilient pattern
	entityBucket, err := rp.natsClient.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket:      "ENTITY_STATES",
		Description: "Entity state storage",
		History:     10,
		TTL:         7 * 24 * time.Hour, // 7 days
		MaxBytes:    -1,                  // Unlimited
	})
	if err != nil {
		rp.logger.Warn("Failed to create/access ENTITY_STATES bucket, skipping entity watch", "error", err)
		return nil // Don't fail - rules can still process messages
	}

	// Use configured patterns
	patterns := rp.config.EntityWatchPatterns

	for _, pattern := range patterns {
		watcher, err := entityBucket.Watch(ctx, pattern)
		if err != nil {
			return errors.Wrap(err, "RuleProcessor", "watchEntityStates", "create watcher")
		}

		// Store watcher for cleanup
		rp.entityWatchers = append(rp.entityWatchers, watcher)

		// Start goroutine to handle updates
		go rp.handleEntityUpdates(ctx, watcher)

		rp.logger.Info("Started KV watcher", "pattern", pattern)
	}

	return nil
}

// handleEntityUpdates processes updates from a NATS KV watcher
func (rp *Processor) handleEntityUpdates(ctx context.Context, watcher jetstream.KeyWatcher) {
	defer func() {
		if r := recover(); r != nil {
			rp.logger.Error("Panic in handleEntityUpdates", "error", r)
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-rp.shutdown:
			return
		case entry, ok := <-watcher.Updates():
			if !ok {
				// Channel closed, watcher stopped
				return
			}
			if entry == nil {
				// Nil entry indicates initial state complete, continue watching
				continue
			}

			// Determine action based on operation and revision
			action := "UPDATED"
			if entry.Operation() == jetstream.KeyValueDelete {
				action = "DELETED"
			} else if entry.Revision() == 1 {
				action = "CREATED"
			}

			// Unmarshal EntityState from KV
			var entityState *gtypes.EntityState
			if entry.Operation() != jetstream.KeyValueDelete {
				var state gtypes.EntityState
				if err := json.Unmarshal(entry.Value(), &state); err != nil {
					rp.recordError(fmt.Sprintf("failed to unmarshal entity state: %v", err))
					continue
				}
				entityState = &state
			}

			// Convert to message for rule evaluation
			msg := rp.entityStateToMessage(action, entry.Key(), entityState)

			// Process through rules
			rp.evaluateRulesForMessage(ctx, entry.Key(), msg)
		}
	}
}

// entityStateToMessage converts EntityState to Message for rule evaluation
func (rp *Processor) entityStateToMessage(action, entityKey string, entityState *gtypes.EntityState) message.Message {
	// Create message type for entity state
	msgType := message.Type{
		Domain:   "entity",
		Category: "state",
		Version:  "v1",
	}

	// Create payload data
	payloadData := map[string]any{
		"action":     action,
		"entity_id":  entityKey,
		"timestamp":  time.Now(),
		"source":     "kv-watch",
	}

	// Add entity state data if not deleted
	if entityState != nil {
		payloadData["entity_type"] = entityState.Node.Type
		payloadData["triples"] = entityState.Triples
		payloadData["version"] = entityState.Version
		payloadData["updated_at"] = entityState.UpdatedAt

		// Extract structured properties from triples for rule matching
		// Build nested maps from predicates (e.g., "robotics.battery.level" -> {"battery": {"level": value}})
		for _, triple := range entityState.Triples {
			parts := strings.Split(triple.Predicate, ".")
			if len(parts) >= 3 {
				// Skip domain (first part), use remaining parts to build nested structure
				// e.g., "robotics.battery.level" -> battery.level
				nestParts := parts[1:] // Skip domain

				// Navigate/create nested maps
				current := payloadData
				for i, part := range nestParts {
					if i == len(nestParts)-1 {
						// Last part - set the value
						current[part] = triple.Object
					} else {
						// Intermediate part - ensure map exists
						if _, exists := current[part]; !exists {
							current[part] = make(map[string]any)
						}
						// Navigate deeper
						if nextMap, ok := current[part].(map[string]any); ok {
							current = nextMap
						}
					}
				}
			}
		}
	}

	// Create generic payload
	payload := message.NewGenericJSON(payloadData)

	// Create base message
	return message.NewBaseMessage(msgType, payload, "kv-watch")
}
