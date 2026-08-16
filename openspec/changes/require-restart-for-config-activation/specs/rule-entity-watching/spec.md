## MODIFIED Requirements

### Requirement: dynamic watcher authority is atomic and generation-scoped

Each boot-configured `(bucket, pattern)` watcher SHALL have an exact generation identity. The configured watcher
identity set SHALL be immutable for the process lifetime. A post-boot desired configuration edit SHALL NOT add,
remove, or replace an identity in the running Rule processor.

After unexpected transport loss, the Start-owned supervisor MAY prepare a repair generation only for the same
boot-authoritative `(bucket, pattern)` identity. Preparation SHALL not grant dispatch authority. Commit SHALL publish
the repair generation and retire the failed generation atomically under the dispatch gate. A retired generation SHALL
lose authority before physical Stop and SHALL remain unauthorized even if Stop fails or delayed work later arrives.

If preparation or replay fails, the Rule entity-watch lane SHALL remain degraded, the failed generation SHALL not
regain authority, and no desired watcher-set change SHALL be interpreted as a repair. Full-set add/remove/replacement
is a next-successful-boot operation.

#### Scenario: Desired pattern addition does not mutate runtime authority

- **GIVEN** boot B admitted configured watcher set W
- **WHEN** desired configuration commits W plus pattern P
- **THEN** B's authoritative watcher set remains W
- **AND** no transport for P is prepared or admitted before the next successful boot

#### Scenario: Failed repair cannot broaden the watcher set

- **GIVEN** boot-authoritative watcher W loses transport
- **WHEN** preparation of a replacement transport for W fails
- **THEN** the lane remains degraded and no new generation gains authority
- **AND** no pending desired addition or removal is applied as part of repair

#### Scenario: Stale repair work cannot cross generations

- **GIVEN** generation 1 queued or decoded work before losing authority
- **AND** generation 2 repairs the same boot-authoritative watcher identity
- **WHEN** old callback or debounce work reaches dispatch
- **THEN** generation-1 work is rejected before fetch, evaluation, metric, transition, or action
- **AND** valid generation-2 work remains eligible
