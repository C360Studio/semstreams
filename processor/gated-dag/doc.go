// Package gateddagexec is the executor component for gated-DAG dispatch
// (ADR-046 Phase 2, gh#357). It composes the pure selection brain
// (pkg/gateddag) with live framework substrate so a set of work units with
// depends_on edges is dispatched in dependency order, exactly once per unit,
// with restart recovery, failure isolation, and stall detection.
//
// # Why a component (not a rule action or condition)
//
// Gated-DAG dispatch needs a whole-set, authoritative, re-evaluated-on-every-
// change view of the unit set. The rule engine evaluates against the changed
// entity plus one related read (processor/rule/entity_watcher.go →
// evaluateRulesForEntityState) and a gate on a dependent never re-fires when a
// prerequisite completes (the engine evaluates rules against the prerequisite,
// not the dependent). A component watching the unit set provides the whole-set
// view natively. See ADR-046 "Why a component".
//
// # The wedge it kills: derived, never mutated
//
// Each evaluation re-reads the whole unit set authoritatively from the graph
// and re-derives every unit's status from marker membership (pkg/gateddag is a
// pure function of markers + edges). There is no projected status field to go
// stale and nothing to race the markers — the root of the wedge family that
// reverse-edge propagation could not escape (ADR-046 correctness requirement
// #1). The only state this component writes is the durable claim marker, which
// the brain ignores and the executor reads authoritatively for in-flight dedup.
//
// # The four load-bearing invariants (do not regress)
//
// An adversarial review of an earlier ADR draft caught it asserting guarantees
// the substrate does NOT provide. These hold here:
//
//  1. Single-flight per fan-out, one instance per fan-out. replace_owned is
//     last-write-wins, not CAS, and the owner-lease fence is default-off +
//     cross-incarnation only — neither provides concurrent mutual exclusion.
//     Dedup holds only because reEvaluate runs one pass at a time per instance
//     (evalMu + a single eval goroutine) and exactly one component instance owns
//     a given fan-out (singleton in the flow). If true cross-writer exclusion is
//     ever needed, switch the claim to an ExpectedRevision-based CAS write (see
//     the CAS-UPGRADE POINT in claim.go).
//  2. Claim before dispatch. The claim marker is committed BEFORE the dispatch
//     is published. A crash between publish and claim would re-derive the unit
//     as Ready on restart and double-run.
//  3. Re-eval + recovery ride the lifecycle Watch (which wraps KV WatchAll's
//     bootstrap replay), NOT the pkg/dispatch completion-watcher (which
//     suppresses bootstrap replay). The dispatcher is the bounded-concurrency
//     leg only.
//  4. Stall detection + the periodic backstop are net-new. No production code
//     implements them; they are the core build of this phase, not reuse.
//
// # The reset contract (hard requirement on consumers)
//
// To re-dispatch a unit (recovery / re-run), the consumer MUST clear the unit's
// terminal markers (completed/failed) AND the claim marker, and set the dirtied
// marker. The brain's Dirtied precedence then re-derives the unit as Ready. As a
// defensive measure this executor treats a dirtied unit's stale claim as absent
// (dirtied overrides claim), so a reset that forgot to clear the claim still
// re-dispatches rather than wedging idle.
//
// # Framework / consumer boundary
//
// This component is domain-agnostic. It consumes a resolved depends_on edge set
// plus completion/failure/reset markers on per-unit entities, and on a
// dispatchable unit it (1) commits a durable claim marker then (2) publishes a
// reference (the unit entity ID — never content) to the configured dispatch
// subject. The consumer derives the edge set, mints fresh-per-run unit IDs,
// layers any domain gate, and wires its own handler on the dispatch subject to
// turn the reference into real work. See ADR-046 "Framework/consumer boundary".
package gateddagexec
