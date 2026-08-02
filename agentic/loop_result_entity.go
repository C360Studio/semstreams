package agentic

import (
	"github.com/c360studio/semstreams/message"
)

// loopResultContentField is the field name the offloaded result body is
// stored under in the ObjectStore content envelope. Readers do not hardcode
// it — they resolve it through the persisted ContentFields map via the
// message.ContentRoleBody role, per the ContentStorable contract.
const loopResultContentField = "result"

// LoopResultEntity is the message.ContentStorable carrier for a loop's
// offloaded terminal result body (payload-size-chokepoints D4). When a
// completion Result exceeds the offload threshold, the agentic-loop
// component stores this entity's content in the AGENT_CONTENT ObjectStore
// and rewrites the COMPLETE_ KV value to the ref-bearing shape.
//
// Content-only entity: Triples returns nil because the loop's semantic
// metadata already lives on the loop execution entity (WriteLoopCompletion);
// this entity exists solely to give the bulky body a validated storage
// address. Construct via NewLoopResultEntity — the constructor validates the
// entity identity so the persistence hot path never panics on malformed
// platform config (the TryLoopExecutionEntityID panic-class concern).
type LoopResultEntity struct {
	entityID   string
	result     string
	storageRef *message.StorageReference
}

// NewLoopResultEntity validates the identity parts and returns the
// ContentStorable carrier for the loop's result body. Returns an error when
// org/platform/loopID cannot form a valid 6-part entity ID — callers skip
// the offload and let the seam guard rule on the inline write.
func NewLoopResultEntity(org, platform, loopID, result string) (*LoopResultEntity, error) {
	id, err := TryLoopResultEntityID(org, platform, loopID)
	if err != nil {
		return nil, err
	}
	return &LoopResultEntity{entityID: id, result: result}, nil
}

// EntityID returns the pre-validated 6-part entity ID.
func (e *LoopResultEntity) EntityID() string { return e.entityID }

// Triples returns nil: the result body is content, not semantics. Loop
// metadata triples are stamped on the loop execution entity by the graph
// writer.
func (e *LoopResultEntity) Triples() []message.Triple { return nil }

// StorageRef returns the ObjectStore reference once stored, nil before.
func (e *LoopResultEntity) StorageRef() *message.StorageReference { return e.storageRef }

// SetStorageRef records the ObjectStore reference after content is stored.
func (e *LoopResultEntity) SetStorageRef(ref *message.StorageReference) { e.storageRef = ref }

// ContentFields maps the body role to the stored field name so readers
// (read_loop_result hydration, embedding workers) find the result without
// hardcoding field names.
func (e *LoopResultEntity) ContentFields() map[string]string {
	return map[string]string{message.ContentRoleBody: loopResultContentField}
}

// RawContent returns the result body for ObjectStore storage.
func (e *LoopResultEntity) RawContent() map[string]string {
	return map[string]string{loopResultContentField: e.result}
}
