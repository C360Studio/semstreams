## 1. Landed implementation

- [x] Change `Service.Stop`, `Manager.StopAll`, and `LifecycleComponent.Stop` to accept `context.Context`.
- [x] Migrate all SemStreams implementations and direct callers without an adapter.
- [x] Preserve reverse-order StopAll execution, continued stop attempts, genuine error aggregation, and completed
  repeated Stop success.
- [x] Remove exported `ManagedComponent.Cancel`.
- [x] Remove retained production contexts from the inventoried lifecycle owners.
- [x] Add the type-aware production context/cancellation contract guard.
- [x] Publish the compiler-directed migration guide and read-only downstream census.
- [x] Record the original reviewer, race, integration, contract, schema, core-E2E, and semantic-E2E evidence.

## 2. Archive closeout

- [x] Reconcile the migration guide to landed current truth and remove pending-mega-design language.
- [x] Validate the narrowed change and all baseline specs strictly.
- [x] Run the final issue #1011 race, contract, schema, OpenSpec, and relevant E2E verification.
- [x] Obtain independent review and archive the change.
