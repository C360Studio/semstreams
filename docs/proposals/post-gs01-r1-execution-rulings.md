# Post-GS01 R1 execution rulings

Status: **accepted owner clarification**.

These rulings clarify execution of the accepted R1 design without changing its semantic outcomes. They do not reopen
or replace the content-addressed design, amendment, review, or approval.

## Planning stop rule

Planning ends when behavior is precise enough to express as a falsifiable test. A review finding that requests more
evidence but does not change the target becomes implementation or test work. Only a semantic contradiction returns to
architecture and owner ruling.

Each implementation PR receives one architect boundary pass and one implementation review. Missing evidence does not
start another content-addressed prose loop.

## Shared mechanism and vocabulary

The shared framework surface is:

1. catalog acquisition; and
2. canonical outcome classification.

Poison, not-found, unavailable, and revision-mismatch retain one framework meaning wherever they occur. Readers and
components MUST preserve the canonical type, class, code, detail, and causal error. An owner may not reclassify an
outcome to make its local response easier.

The owner-specific surface is policy response: affected scope, retry decision, subscription closure, request failure,
degraded posture, readiness publication, and operator diagnostics.

Therefore “poison policy is local” means that blast radius and response are local. It does not permit lifecycle,
indexes, rules, gateways, or other readers to invent different poison semantics.

## Layer boundary

| Layer | Owns | Does not own |
|---|---|---|
| Catalog reader | One must-exist read-only acquisition and canonical acquisition outcomes | Retry or readiness |
| Startup watcher | Component-local retry budget and terminal response | Storage ownership or classification |
| Lifecycle | Phase and transition policy for the touched entity | Whole-graph availability |
| Derived owner | Projection work and honest typed status publication | Domain lifecycle policy |
| Status consumer | Typed freshness/currency gating for a required projection | Data acquisition or reclassification |

The framework shares acquisition and classification, not a general graph client, watcher supervisor, poison
coordinator, retry engine, or universal readiness runtime.

## R1a execution boundary

R1a proceeds as small green changes:

1. replace the write-capable reader front door with a read-only catalog reader and migrate existing callers;
2. narrow stateless helper inputs and add structural contract tests;
3. migrate graph-index, spatial, temporal, and embedding retained capabilities; and
4. migrate clustering, rule, lifecycle acquisition, and gated-DAG retained capabilities.

The old reader front door is deleted, with no shim. Lifecycle global-guard deletion remains R1b. Typed
`GRAPH_STATUS` restructuring remains R3. Index results and APIs remain frozen throughout R1.
