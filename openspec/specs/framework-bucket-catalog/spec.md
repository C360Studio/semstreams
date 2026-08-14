# framework-bucket-catalog Specification

## Purpose

Define the single descriptor catalog for KV buckets whose ownership or retention SemStreams guarantees. The catalog
binds each bucket to its owner and storage policy, gives owners a create-or-open and reconcile acquisition seam, gives
readers a must-exist non-creating seam, and derives owner-only generic-write guards from the same descriptors. An
adopter does not select catalog membership, bucket identity, or retention for framework-owned state.

This capability does not catalog application/product buckets, authenticate owner identity at runtime, provide generic
KV mutation authority, or create an adopter-facing bucket, ownership, or retention configuration surface.

## Requirements
### Requirement: The framework catalog SHALL own durable tool-call outcomes

The catalog SHALL declare `TOOL_CALL_OUTCOMES` as operational, owner-only state owned and created by `agentic-tools`.
It SHALL have History 1, Replicas 1, MaxAge zero, MaxBytes unlimited, and no lifecycle reclamation. `agentic-tools`
SHALL acquire it through `EnsureFrameworkBucket` before subscriptions or consumers. No adopter-facing bucket or
retention configuration SHALL be added.

#### Scenario: generic KV mutation targets the outcome ledger

- **WHEN** a generic framework KV writer targets `TOOL_CALL_OUTCOMES`
- **THEN** the catalog-derived owner-only guard rejects the write
- **AND** identifies `agentic-tools` as owner
