# R8 retained-facts decision draft

base: 68c14c8eb25c512e988f740cbf7ea14b6815976f

Status: advisory design for independent pre-owner review. No runtime, spec, configuration, or task-truth promotion is authorized by this draft.

## Accepted evidence

The complete accepted inventory is incorporated unchanged as the binding appendix identified by SHA256 `53fe8a90022dac6dd6a036c32a648109d2c073758c2cd2e85fed5bb4fd9dee6b`, independently verified at 71/71 pins. Its three earlier reviewed inventory appendices remain unchanged.

The retained-task reuse correction, existing RED, and existing native expiry evidence remain intact. The research completion-format collision stays with #1288.

## Outcome and obstruction

The required safety guarantee is **lost work must not be mistaken for new work**. Successful continuation after reconstruction content expires is a separate, stronger guarantee.

Safety may permit an explicit unavailable/refused outcome instead of retaining reconstruction content indefinitely. Ordinary multi-turn operation and restart remain supported while the necessary evidence exists. That alternative still requires reliable identity authority and a distinction between valid partial birth and previously progressed work; refusal alone does not resolve all three absence cases.

Identity storage and AGENT_LOOPS co-tenancy remain unresolved under either guarantee.

## Genuine options

| Option | Behavior and cost |
|---|---|
| Leave this arm open | Keep the reviewed task-reuse improvement and other completed work. The demonstrated identity-loss failures remain unresolved; R8 cannot claim them fixed. |
| Retain sufficient identity/progress facts; explicitly refuse when reconstruction content is unavailable | Preserve ordinary multi-turn/restart while evidence exists; prevent expired content from triggering an original-prompt restart. Requires an owner-approved task-scoped refusal outcome and a reliable partial-birth distinction. It does not require indefinite full-content retention merely to establish safety. Exact authority representation, lifetime and co-tenancy remain unresolved. |
| Extend existing KV authority and Store custody | Preserve task identity and prepared request content before publication. No scheduler, supervisor, output log, or new storage API. Requires additional durable representation, mandatory storage dependencies, and an accepted non-expiring authority boundary. Records/content accumulate without a proven reclamation rule. |
| Remove the co-tenancy constraint first | Allows separate retention decisions but reaches the research/result-reader boundary and #1288 territory. This is not a small invisible addition to #1146. |

## Higher-guarantee option: retain prepared task/request content

The following mechanism illustrates an option for successful reconstruction beyond stream-content expiry. It is **not the minimum required for safety, not selected, and not an implementation recommendation**. Full TaskMessage binding content, Store-backed request custody, and mandatory provider dependencies are costs of this option—not established requirements of the narrower safety contract.

### Dispatch identity

Persist one source-derived TaskID binding, owned by dispatch, before publishing its task.

One candidate representation is the existing registered TaskMessage envelope as the value of a private, TaskID-keyed KV record. This introduces a durable key family, not a new wire payload. It is not compact: prompt and prior-message content remain present.

Use KV Create to select the binding; on an existing key, read and apply the existing full correlation validation. Do not use generic Store Put to select identity: Store has no Create/CAS contract.

The binding records prepared identity, **not publication success**. A matching retained task still proves publication and is reused without publishing again. If stream evidence is absent, publish the bound task with the same identity and require the normal PubAck.

Preserve durable USER response settlement and HTTP’s existing synchronous/optional-mirror behavior. A mapping write failure must precede task publication and must not mint a different identity on successful retry.

### Loop authority and prepared request

Retain the existing LoopEntity authority rather than introducing a second loop state machine or ledger.

Add one explicit current prepared-request reference to that authority. Store the existing registered AgentRequest bytes through the existing Store provider; the reference identifies those bytes and their integrity. This is a real persisted-model addition requiring owner approval.

Required order:

1. Prepare and validate the request.
2. Store its content; resolve an uncertain write using the existing read-and-compare pattern.
3. Persist LoopEntity with the corresponding request reference.
4. Publish the request and require PubAck.
5. Settle the durable input only after its existing required effects complete.

A current-request reference must never be cleared by ordinary state updates or continuation attachment. A replacement reference is committed together with the corresponding current authority.

This is content-before-reference ordering, not a background outbox dispatcher. The existing component still performs all work synchronously under its delivery owner.

### Resulting absence distinctions

| Observation | Meaning and disposition |
|---|---|
| Dispatch binding absent in correctly retained, fresh-provisioned authority | First preparation may select identity with Create. |
| Loop authority absent in that same supported storage contract | First loop birth remains possible; publication must not precede committed authority. This does not promise recovery after administrative deletion. |
| Nonterminal authority exists without a request reference | Valid partial birth: no request could have been published under the new ordering. Prepare the first request. |
| Authority contains a valid request reference | Restore that prepared request, not the original task prompt, even when stream request evidence has expired. |
| Authority is terminal | Preserve terminal suppression and existing selected-outcome behavior. |
| Referenced object is definitively missing or conflicts | Proposed task-scoped classified quarantine/refusal, not initial-prompt reconstruction and not endless transient retry. Operator intervention is required. This is a new contract requiring approval. |
| KV/Store read or persistence fails transiently | Retry without source ACK or replacement identity. |

The missing-object proposal does not create a `continuation_unavailable` loop terminal state or extend the approval-only ruling. Approval, governance and provider absence contracts remain unchanged.

## Lifetime and capacity are the owner decision

The mechanism requires identity/current authority not to disappear through ordinary TTL or capacity eviction while accepted work can return.

Applying that requirement to existing AGENT_LOOPS makes research intent, stage outputs, snapshots and COMPLETE records non-expiring too. Its existing ten-revision policy bounds revisions per key, **not key count**. This is a material broadening and is not approved by the earlier retained-facts direction.

Separating those records instead is another storage/consumer-boundary change; this draft does not conceal it as a new bucket constant.

Costs are explicit:

- One persistent dispatch binding per distinct accepted source/task, including its task envelope.
- One persistent loop authority per loop, plus existing terminal records.
- Prepared request content can grow with turns/requests, not merely loop count.
- Content-addressed replacement objects and interrupted preparations can leave unreferenced objects.
- No measured safe reclamation predicate currently permits deleting those objects or identity records.
- No byte estimate or bounded-cardinality claim is supported.
- Storage exhaustion must cause observed write failure and unsettled input—not eviction followed by resurrection.
- The task-envelope KV carrier may introduce a stricter size constraint; acceptance must either permit explicit pre-publication refusal or separately approve Store-backed binding content.

No cleanup service, retention timer, automatic purge, or indefinite recovery guarantee is proposed.

## Startup and adopter consequences

This changes the current 24h loop-bucket contract. Acquisition must observe the selected policy and refuse incompatible storage; silently stripping TTL cannot recover facts already lost.

All co-tenant provisioning paths must agree before startup can be claimed correct. Existing adopters doing nothing must receive a clear configuration/readiness refusal, not silently acquire a different guarantee. Greenfield breaking adoption uses fresh provisioned storage and documented configuration changes.

The loop already has StoreRegistry and a configured trajectory storage instance. Making request custody mandatory is **not** the same as today’s best-effort trajectory audit use. Provider selection/configuration meaning must be explicitly approved; this draft does not silently repurpose the trajectory setting.

R3 retired an additional approval Store obligation. Reusing the existing Store primitive avoids inventing a storage abstraction, but mandatory task/request custody would introduce obligations again. That product and dependency cost requires a fresh owner decision; approval recovery itself is not being redesigned.

## Bounded verification after acceptance

The promoted spec must state the ordering and absence invariants above before tests encode them.

Use unit tests first for identity selection, partial birth, request-reference preservation, terminal suppression, classified missing-content refusal and every failure boundary. Expected results come from these invariants, not implementation-generated state.

PBT is applicable prospectively because restart/replay histories matter. A small bounded history model should vary preparation, persistence failure, uncertain publication, restart and redelivery. Assert stable selected identity, no publication before authority/content commitment, and no original-prompt resurrection after a request reference exists.

Plausible mutations: bypass binding Create, clear a current-request reference, publish before its authority write, or interpret missing referenced content as new work. Demonstrate that the relevant assertions detect them; no new mutation dependency is required.

Use existing native fixtures for the irreducible KV/Create, Store persistence and stream-expiry semantics, through the canonical runner with one isolated NATS dependency and bounded observations. No 24h waits. Relevant agentic E2E remains required before a breaking merge. No tests were executed in this design pass.

## Recommendation and product decision

Decide the required guarantee before selecting storage mechanics:

- **Safety with explicit refusal:** when reconstruction content is definitively unavailable, refuse visibly instead of restarting the original work. Normal continuation/restart remains supported while evidence exists.
- **Recovery beyond stream-content expiry:** retain additional reconstruction content, accepting its accumulated storage, provider-dependency and lifecycle costs.

I recommend making that product choice first; this draft does not establish either storage design as the smallest solution.

Both choices still owe a concrete solution for dispatch identity, loop authority and valid partial birth, including the unresolved shared-bucket retention boundary. Neither authorizes silently extending the approval-only `continuation_unavailable` contract.

Preserve draft SHA `2a4bb985…` as provenance. These corrections withdraw its minimum-mechanism and bundled-approval claims; they do not promote runtime or spec changes.
