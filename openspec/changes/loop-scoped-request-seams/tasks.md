# Tasks

## 1. Evidence

- [x] 1.1 Inventory every seam accepting a request that names an existing loop, line-pinned, per-seam
      check matrix {form, existence, ownership, classified refusal, observed signal}
- [x] 1.2 Answer: does the dispatch `LoopTracker` rehydrate from durable state on restart? Read PR #1159's
      branch, not only `main`
- [x] 1.3 Answer: does legitimate continuation preserve conversation context, given `CreateLoopWithID`
      replaces the context manager?

**1.2 / 1.3 answered — both NO. See `findings-decisive-questions.md`.**

## 2. Design read

- [~] 2.1 Is a primitive missing? Where does it live, what does it own, and does it subsume #1227/#1228/#1225?
- [ ] 2.2 Adopter seam inventory for whatever surface the answer implies
- [ ] 2.3 HALT — owner ruling on the design fork before any target state is written

## 3. Target state

- [ ] 3.1 (not written — gated on 2.3)


## Status at last update

- Evidence complete: `inventory-attach.md`, `inventory-carriers.md` (95 pins, verify clean),
  `inventory-precedent.md`, `findings-decisive-questions.md`.
- 2.1 is with `semstreams-judge` as a bounded A/B fork: admission gate vs state-authority
  correction. Codex PR #1159 is on hold pending that answer (owner, 2026-09-01).
- Spawned #1232 — the architect inventory is fact-scoped and cannot surface a cross-plane pattern.
