# R1 decomposed execution design review

Disposition: **DESIGN REVIEW PASS**.

## Reviewed artifacts

- `post-gs01-r1-decomposed-execution-design.md`
  - lines/bytes: 426 / 17,180
  - SHA-256: `e1d7c47898824b4bfdca33a4e53da75dd4d59af147315ba2871f2cbebe2c017f`
- `post-gs01-r1-roadmap-amendment.md`
  - lines/bytes: 193 / 9,034
  - SHA-256: `85c837aca8ccbf38483848f322c85aba929596f24f5e517b125b6bc42a883e5b`

The review also consulted the accepted R1 inventory and the independently reviewed but superseded composite R1
design.

## Review result

The five slices are coherent, independently reviewable, and main-ready in the required linear order. The mandatory
pattern census and extraction gate is sufficiently exact to expose repository-wide repetition without manufacturing a
universal runtime. Local consumer-owned interfaces remain the default; extraction requires measured semantic identity,
lower authored production cost, and lower adopter knowledge.

The review verified:

- R1a truthfully retains lifecycle `ListKeys`/`Watch`/`WatchAll` only until R1b deletes the global guard and
  `WatchAll` atomically;
- R1b reserves `docs/adr/092-lifecycle-poison-localization.md` as the narrow successor to ADR-081's lifecycle-wide
  sticky-guard ruling, keeps ADR-081 byte-identical, and leaves current mechanics to lifecycle OpenSpec;
- R1c characterizes rule behavior and preserves owner-local full-intent lifecycle retry without a shared helper;
- R1d names all four checked-in configurations, separates `KVReadPort` from Store federation, and owns the exact
  gated-DAG structural coverage gap without a production fault hook;
- R1e is a coherent catalog-only operator diagnostic boundary, with product middleware retaining authorization;
- the program-level index result/API freeze permits pattern inventory but prohibits R1 from changing or pre-designing
  query subjects, operations, result contracts, wire fields, pagination, absence/ambiguity, readiness/currency, or
  adopter-facing graph result shapes;
- a required result/API change stops its R1 slice, records a baton falsification, and moves to its owning R3–R6
  increment without a preparatory abstraction or dual contract;
- lifecycle, statistical, and core E2E are allocated only to the behavior-changing slices they prove;
- file reservations and baton evidence preserve context across the linear handoffs; and
- no compatibility shim, deprecated path, dual declaration, or speculative general client is admitted.

## Corrected blockers

The first review requested two changes:

1. bind the roadmap amendment to the materialized replacement design identity; and
2. reserve a durable successor ADR for the ADR-081 lifecycle ruling.

Both were corrected before this pass. The corrections introduced no contradiction with topology, reservations, E2E
allocation, completion gates, or the SemStreams identity packet.

After the owner requested an explicit index result/API freeze, that clarification was materialized and independently
re-reviewed. It does not freeze R1e's separately approved message-logger diagnostics contract and does not alter any
prior ruling.

## Historical integrity check

`docs/adr/081-graph-view-subscription.md` remained byte-identical to the reviewed baseline:

- SHA-256: `df5f6692225a4eddc2a4382592467ce943e329a466bc59664e30ca83f44635ec`

## Required owner rulings

Implementation remains unauthorized until the owner accepts:

1. R1a's truthful interim lifecycle `ListKeys`/`Watch`/`WatchAll`, followed by atomic R1b deletion of `WatchAll` and
   the Manager-wide guard;
2. linear execution R1a → R1b → R1c → R1d → R1e;
3. the gated-DAG structural coverage gap under the foundation program, assigned to `@cglusky`, targeting
   `task e2e:structural`; and
4. the exact design, amendment, and review identities.

No implementation files were changed during review.
