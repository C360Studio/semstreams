# Review of post-GS-01 graph read and derived-state foundation design

## Review status

**DESIGN REVIEW PASS — PRE-OWNER**

This record covers the complete pre-owner design at:

- artifact: `docs/proposals/post-gs01-graph-read-derived-foundation-design.md`
- baseline: `d1570ef81b23096021af0d7bf3321b4c08c7e54b`
- lines: 1,996
- bytes: 132,759
- SHA-256: `533b2010a4da24f55915fbebf046530dcb7ae7c8cdaa134eb6853a5ce36385fb`
- accepted embedded inventory: lines 9–637, byte-identical to
  `post-gs01-graph-state-reality-audit.md`
- embedded inventory SHA-256: `869be8fdfaef9c141dd7697071da0ff9fb5ffa1c4e3fbb5863837b25fb3be4ba`
- independent reviewer: SemStreams reviewer role
- final review date: 2026-08-06

The pass means the artifact is complete enough for binding owner review. It does not confer owner approval, establish an
implementation roadmap, schedule issues, authorize implementation, or revive the invalidated GS-02-through-GS-14
sequence.

## Review method

The reviewer read the complete exact artifact at each submitted identity, checked the design against the accepted
inventory and fixed owner boundaries, and traced disputed claims back to current source. Review did not treat issue
titles, historical ADR wording, or prior program order as target truth.

The final posture verifies that the target remains:

- offline-first, edge-capable, and tiered;
- pragmatic and eventually consistent;
- one current-state authority with graph-ingest as sole physical writer;
- request/reply mutation with CAS and no mutation stream;
- conformant GraphQL for external graph reads and narrow typed internal adapters;
- gqlgen schema-first generated execution with thin canonical-operation resolvers, not a hand-written GraphQL stack;
- free of semantic ownership, CQRS/event-store machinery, auto-stubs, recovery product, compatibility shims, and NATS
  CLI dependency; and
- bounded by focused proof before final relevant E2E, rather than repeated full-ladder test runs.

## Review rounds and dispositions

### Round 1

Changes were requested because authority acquisition collided, operation and status sets were deferred, the status wire
was underdefined, and stale program guidance plus the active GS-01 change lacked complete dispositions.

The design added a package-local catalog-open acquisition seam, fixed the complete operation/provider/status sets,
defined role-specific status wires, and enumerated the required current-artifact corrections and GS-01 archive truth.

### Round 2

Changes were requested because the design invented a nonexistent injected catalog source, incorrectly moved gated-DAG
onto lifecycle watch semantics, omitted predicate operations, derived `CoveredAt` from the wrong timestamp, and left the
correction set incomplete.

The design deleted the injector, kept gated-DAG's declared arbitrary-prefix watch, admitted all four predicate
operations, bound `CoveredAt` to the same atomic watermark observation, and completed the live-source disposition set.

### Round 3

Changes were requested because three predicate operations were labeled typed-internal without present consumers, the
operator status front remained unresolved, and binding readiness/adopter documentation was missing.

The design corrected predicate visibility, retained only the one present internal predicate-list consumer, fixed the
operator surface as configured HTTP with closed role keys and no aggregate verdict, and added binding readiness and
adopter documentation dispositions.

### Round 4

Changes were requested because deleting graph-ingest and rule status would lose real semdragon, SemMachina, and E2E
behavior, and broad word greps would reject valid history and unrelated code.

The design retained all four justified `GRAPH_STATUS` keys with distinct role types and replaced broad greps with
targeted structural, wire, compiler, and behavioral proof.

### Round 5

Changes were requested because hierarchy statistics silently reported first-page-only counts after the provider's
1,000-row page, and suffix resolution retained a lossy, collision-unsafe derived index.

The design made hierarchy statistics cursor-exhausted complete-or-error with a framework-owned resource ceiling and
observed-count naming. It retired suffix identity guessing and `ENTITY_SUFFIX_INDEX` completely, with no compatibility
reader or cleanup dependency.

### Round 6

Before final disposition, source tracing found that `ALIAS_INDEX` had the same foundational defect: raw alias to one
entity, last-writer-wins collision, no owner-complete replacement reconciliation, and ghost rows after change or delete.

The design retained the legitimate exact-alias feature but replaced the physical contract with graph-index-owned paired
lookup/owner memberships. Exact resolution now distinguishes absent, singular, and ambiguous; it never chooses among
collisions. Replacement, deletion, bootstrap repair, failed-work readiness, inert legacy rows, public mapping, and
focused proof are explicit. No general alias runtime or compatibility path was added.

### Round 7

Changes were requested because alias-view currency was promised but had no query-visible wire path. The hydrated
entity's KV revision described authority, not the alias resolution view, while the operator status surface was separate
from GraphQL.

The final design added operation-specific currency rather than a general metadata envelope. The provider captures its
greatest contiguous completed authority revision immediately before the alias scan. Singular resolution returns
`AliasedExactEntity` with independent `kvRevision` and `aliasCoveredThroughRevision`; absent and ambiguous outcomes
carry the same lower bound through classified detail and GraphQL extensions. The contract explicitly rejects snapshot,
upper-bound, and linearizability interpretations.

### Round 8 — Fable review adjudication

Fable independently re-derived the deletion census and endorsed Option B, then requested an explicit lifecycle poison
disposition, an established GraphQL library ruling, an explicit suffix-history supersession, and reconsideration of the
rule one-attempt policy. Fable also noted that implementation sequencing belongs in later slices.

The design accepted the two structural additions:

- deleting lifecycle's Manager-wide full-graph guard now explicitly localizes poison. Affected synchronous scope fails
  with typed poison; a malformed matching watch is never acted upon and closes/degrades observably; bounded structured
  logs/metrics identify entity and revision; unrelated lifecycle work continues. No lifecycle status product or new
  global gate was introduced; and
- gqlgen is the binding schema-first, type-safe generated executor with thin canonical-operation resolvers. SemStreams
  does not hand-write parsing, execution, introspection, selection projection, or response machinery. Mutations,
  subscriptions, and playground routes remain absent.

The suffix-history premise was corrected while accepting the requested explicit supersession. The current disposition
was graph-ingest-owned GS-05 in the accepted archived scope audit. Revision 35's future graph-index ownership was
rejected and never became current; PR #898 did not establish it. The target explicitly supersedes both the live GS-05
future disposition and the rejected proposal without rewriting historical records.

The request to restore one bounded automatic rule retry was considered and rejected. The exact GS-01 owner approval at
`9c6913ad558205b89c4197bb813228f40133432364698e1413666df8fe11f161` expressly selected one framework attempt.
More importantly, lifecycle is not a symmetry precedent: every lifecycle retry rereads and revalidates current phase,
transition legality, audit chain, and optional mutator. A rule action was evaluated against an older
`ExecutionContext`; obtaining a fresh revision alone would replay stale intent without re-evaluating its trigger.
Future rule retry remains possible only through a separately approved contract that re-evaluates predicate and intent.

The design also makes its target-state boundary explicit: dependency-ordered implementation slices are derived only
after owner approval and cannot amend the target implicitly. The exact amended artifact received a new independent
`DESIGN REVIEW PASS` on 2026-08-06.

## Final reviewer disposition

`DESIGN REVIEW PASS`

No reviewed acquisition, operation, provider, status-key, status-field, alias, suffix, hierarchy, lifecycle-poison,
GraphQL-executor, retry-boundary, adopter-seam, documentation, or proof decision remains deferred to implementation.

## Required next decision

The owner must approve or reject the complete 17 rulings in design §19 before any roadmap is derived. If approved, the
roadmap must be generated from this exact target and its dependencies, not from issue order or the superseded GS
program. Any later material target change requires a new exact artifact identity and another independent review.
