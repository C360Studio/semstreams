# Tasks

## 1. Evidence

- [ ] 1.1 Inventory every seam accepting a request that names an existing loop, line-pinned, per-seam
      check matrix {form, existence, ownership, classified refusal, observed signal}
- [ ] 1.2 Answer: does the dispatch `LoopTracker` rehydrate from durable state on restart? Read PR #1159's
      branch, not only `main`
- [ ] 1.3 Answer: does legitimate continuation preserve conversation context, given `CreateLoopWithID`
      replaces the context manager?

## 2. Design read

- [ ] 2.1 Is a primitive missing? Where does it live, what does it own, and does it subsume #1227/#1228/#1225?
- [ ] 2.2 Adopter seam inventory for whatever surface the answer implies
- [ ] 2.3 HALT — owner ruling on the design fork before any target state is written

## 3. Target state

- [ ] 3.1 (not written — gated on 2.3)
