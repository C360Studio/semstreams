# Migration — the Graphable lane validates what it decodes (#1112)

SemStreams-owned record of what a downstream product must check when it picks up the SemStreams version that
carries #1112. Sister repositories are **read-only** to SemStreams agents: every obligation is recorded here, and
each sister's owner applies it in their own repository.

## What changed

graph-ingest's Graphable lane now calls `BaseMessage.Validate()` on every decoded arrival, before any method of the
payload runs. A payload whose own `Validate()` fails is a poison message on the lane's existing path: counted in the
component's error count, logged at WARN with the subject and the reason, acknowledged and dropped, never written to
`ENTITY_STATES`. The lane also recovers a panic raised by payload code on the lane (a registered type's
`UnmarshalJSON`, `Validate()`, `EntityID()`, `Triples()`, `StorageRef()` or `IndexingProfile()`). For such a payload
the delivery changes from Nak-and-redeliver (the stream handler's panic recovery) to a counted ack-drop.

`LoopExecutionEntity.EntityID()` now returns `""` for an identity it cannot construct, `Validate()` rejects a
malformed parent, reply-to or run ID, and `Triples()` omits such a triple instead of panicking. The exported
`LoopExecutionEntityID` constructor still panics on malformed input; its contract is unchanged.

## Who is affected

Only a producer that bypasses `BaseMessage.MarshalJSON` and publishes wire JSON for a registered Graphable type that
fails that type's own `Validate()`. `MarshalJSON` already refuses such a payload, so a producer that publishes
through it sees no change. Before this change the lane persisted the invalid payload; now it drops it.

## The check

Search your repository for any code that builds the `BaseMessage` wire envelope (`"type"`, `"payload"`, `"meta"`)
by hand, or publishes to a graph-ingest input subject without going through `BaseMessage` and `json.Marshal`. For each
hit, confirm the payload passes its type's `Validate()`, or move the publish onto `message.NewBaseMessage` plus
`json.Marshal`, which enforces it at the producer.

After upgrading, a message this change drops shows up as a graph-ingest WARN
`graph-ingest: decode/extract failed; dropping` carrying `payload validation failed` or `panicked on the Graphable
lane`.
