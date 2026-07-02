# ADR-063: Store-as-substrate & the `StorageInstance` resolver

## Status

**Proposed — 2026-07-02. Design-reviewed (adversarial, 5-lens) — findings resolved in
this revision.** Scopes gh#415 (shared `{StorageInstance → storage.Store}` resolver —
converge fusion + embedding content fetch). Follow-on to ADR-062 (deterministic graph
fusion) increment-6 convergence; parent gh#376, tracker gh#411. Companion to the
already-shipped loud-fix for the silent case (gh#414, #416).

**Review outcome (2026-07-02).** A pre-Accept adversarial review verified every factual
claim against code and returned one BLOCKING + two HIGH findings, all resolved here:

- **B1 (blocking) — reconfig vs. exclusive ports.** `cm.unregisterPorts` is called ONLY
  from `RemoveComponent` (`component_manager.go:719`); the reconfig
  (`restartComponentWithNewConfig`) and reconcile-stop (`stopAndRemoveComponent`) paths
  delete the component + `registry.UnregisterInstance` but never clear `cm.resources`. So an
  **exclusive** `store-provide` port would make a restarted owner fail its own
  `checkPortConflicts` and silently fall back to old config. **Resolution:** `store-provide`
  is **non-exclusive**; duplicate-ownership detection moves to registry-population time (we
  own those hooks). The pre-existing `unregisterPorts` asymmetry (latent for `NetworkPort`
  too) is filed as separate framework work (gh#417), not a dependency of this ADR.
- **H1 — port token vs. registry key.** The registry keys on `store.InstanceName()` (what
  refs carry); the `store-provide` token is declared by the component from its OWN internal
  instance name (the same value), never the ComponentManager map-key. Token, registry key,
  and ref value are one value by construction.
- **H2 — behavior change.** This is additive in WIRING but **behavior-changing by design**:
  offloaded bodies currently excluded (configs that wire no `store-read`) will now resolve
  and embed. See Consequences.

**Phase 1 validated on `e2e:semantic` (2026-07-02).** `Scenario completed successfully`,
`validation_errors:0`, `embedding_failed_total:0`. The registry path was exercised live:
`content_resolved_total:189` (offloaded bodies fetched via the registry — previously excluded
under `configs/semantic.json`, which wires no `store-read`), `content_resolve_error_total:0`,
`content_unresolved_total:0`. The H2 inclusion is demonstrated, not just non-regressive.

## Decision

Treat a **Store as a data-plane substrate that lives *below* the flow layer**, and give
the framework a single authoritative way to resolve the federation name a `StorageRef`
carries — `StorageInstance` — to a live store handle. Three complementary layers:

1. **Substrate** — `storage.Store` / `storage.StreamableStore` stay pure byte-storage
   interfaces. A store is not a flow participant; it has no ports, no flowgraph
   presence, no lifecycle stages. It is the moral equivalent of a DB connection that a
   component holds and calls in-process.

2. **Declaration (ports)** — add a **`store-provide:<instance>`** (non-exclusive) port so a
   storage component declares *"I own store instance X"*, and evolve **`store-read`** so
   a consumer declares read access — either to a specific instance (static edge) or to
   the store federation (capability edge, for federated consumers that read whatever
   instance a ref names). Ports remain **declarations**: flowgraph visibility, reconfig
   triggers, and — new here — the **population source for the registry**. Duplicate-ownership
   detection lives at registry-population time (not on the exclusive-port path, which is
   reconfig-fragile — see B1).

3. **Resolution (registry)** — a small concurrent-safe **`StorageInstance →
   storage.StreamableStore`** registry, injected through `component.Dependencies`,
   **populated by the ComponentManager from `store-provide` ports at component Start and
   torn down at Stop**, and **resolved lazily per-fetch** by consumers (graph-embedding,
   fusion). The registry is the live handle-lookup the data plane calls; it is *fed by*
   the flow model's own declarations, so reconfig-correctness falls out of the manager's
   existing lifecycle ownership.

The registry **interface is the deployment seam**: in-process it holds local
`objectstore.Store` handles (fast, streaming); a standalone/remote consumer populates the
*same* interface with a NATS-backed remote store. We do not pick a single transport — we
make the handle's backend pluggable behind `StorageInstance → StreamableStore` and keep
the data plane in-process wherever it can be.

`pkg/fusion` stays a leaf (imports only `message` + `storage`); its `StoreResolver`
interface is unchanged and the populated registry satisfies it.

## Context

### The problem: `StorageInstance` is a name, not an address

A `message.StorageReference` carries `{StorageInstance, Key, …}`. gh#400 canonicalized
`StorageInstance` to the **producing component's instance name** (`objectstore.Store.
InstanceName()`, `store.go:73`) — *deliberately decoupled from the bucket*. So a consumer
holding `ref.StorageInstance` **cannot derive a handle on its own**: it doesn't know the
bucket, the backend (ObjectStore today; a filestore/S3 tomorrow), or the connection. Only
the owning component knows that, and it can change at runtime (reconfig swaps the
instance; a future backend swap changes the physical binding entirely).

That is what rules out every registry-free alternative:

- *Each reader builds its own handle from the ref* → needs the bucket, which it can't get
  from the instance name → circular; and the handle wouldn't track the owner's reconfig
  (built once at the reader's Start).
- *Reconstruct-on-demand* → same "name isn't an address" wall, and provably wrong the
  moment the owner switches backend or bucket.

So a central authority is **mandatory, not convenient**: it is the one place the owner
publishes *"instance X = this live handle, right now,"* and readers resolve current truth
instead of guessing or caching a stale derivation. **Reconfig is the forcing function.**

### The live gap (gh#414 / gh#415)

Two content-fetch paths are each half-built:

- **fusion** (`pkg/fusion/hydrate.go`, ADR-062 #399) defines `StoreResolver`
  (`Store(instance) (storage.Store, bool)`) + `MapStoreResolver` + `BodyResolver`, but
  **nothing populates them in production** — `MapStoreResolver` appears only in hydrate.go
  + tests. Dormant seam.
- **graph-embedding** has a *working* fetch, but it is **single-bucket and
  instance-blind**: `createContentStore` (`component.go:759`) builds one
  `objectstore.Store` from the first `store-read` port, and `worker.fetchTextFromStorage`
  (`worker.go:397`) calls `contentStore.Open(ctx, ref.Key)` — **`ref.StorageInstance` is
  ignored**. A ref naming any *other* instance cannot be fetched, so its offloaded body is
  silently excluded from the embedding (and thus BM25/search).

gh#414 made the silent case **loud** (metric `graph_embedding_content_unresolved_total`
+ a one-shot warning, #416). This ADR is the actual convergence: one populated resolver
both paths consume.

### Stores are already a below-the-flow substrate (grounded)

This is not a new architectural stance — it is what the code already does, made explicit:

- **`store-read` is a declaration, not an edge.** `StoreReadPort` (`port_store.go`)
  carries only `{Bucket, Interface}`, `IsExclusive()==false`, `ResourceID
  "store-read:BUCKET"`. It is a config marker with a resource id for conflict tracking. It
  produces **no wired edge today** — precisely: `classifyInteractionPattern`
  (`flowgraph/flowgraph.go`) has no case for it, so it falls through to `default:
  PatternStream` with the junk connection id `"unknown_type_component.StoreReadPort"`, and
  `connectStreamPorts` never matches it because no output port carries that id. So the
  absence of an edge is a fallthrough side-effect, not a designed classification — which is
  why increment 5 (static/federation edges) is a real flowgraph-classification change, not
  free visibility (see Migration).
- **Components already build store handles directly and call them in-process.**
  `graph-embedding` (`component.go:775`) and `agentic-loop` (`component.go:563`) each call
  `objectstore.NewStoreWithConfig(ctx, c.natsClient, …)` and read the handle in-process.
  The `store-read` port just tells them *which bucket name* to pass.
- **This is how the whole data plane works** — not special to stores. graph-embedding gets
  its KV access the same way (`natsClient.CreateKeyValueBucket`, `js.KeyValue`,
  `bucket.WatchAll` — `component.go:474,822,841`). **Ports are declarations** (visibility,
  conflict detection, stream derivation, reconfig triggers); **actual data access is an
  in-process handle the component pulls from natsClient.** NATS is *encapsulated inside*
  the handle.

So a store registry introduces **no new NATS reach** — components already encapsulate NATS
inside these handles. It changes only *where the handle comes from*: shared-from-owner vs
privately-reconstructed, keyed by the federation identity instead of a static bucket name.

### Reconfig, precisely

Runtime config change is honored by `restartComponentWithNewConfig`
(`component_manager.go:1256`): Stop old → cancel ctx → unregister → create-with-new-config
→ start. A config change **replaces the instance**; on Stop, the objectstore Component
`Close()`s its store (`Close` defined `store.go:371`, called from objectstore `component.go:291` Stop). That is exactly the
point a registry must respect, and it dictates the registry's lifecycle contract (below).

## The three layers

### 1. Substrate — unchanged interfaces

`storage.Store` (Put/Get/List/Delete) and `storage.StreamableStore` (adds `Open`) stay as
they are (`storage/storage.go`). `objectstore.Store` implements both (`store.go:24-27`).
No store gains ports or flowgraph presence. The registry keys **`StreamableStore`** (the
superset — objectstore implements it), which reconciles the type mismatch: fusion's
`StoreResolver.Store()` returns `storage.Store` and is satisfied by auto-upcast; embedding
needs `Open` and gets it directly. One registry, both consumers.

### 2. Declaration — the `store-provide` / `store-read` port pair

**`store-provide:<instance>` (new, NON-exclusive).** A storage component declares the
instances it owns. The token is derived by the component from **its own internal instance
name** (the value it stamps into refs via `store.InstanceName()`) — never the
ComponentManager map-key — so the port token, the registry key, and the ref's
`StorageInstance` are the **same value by construction** (resolves H1). Two jobs:

- **Flowgraph visibility** of ownership — statically true, cleanly declarable.
- **Population source for the registry** — the ComponentManager, which already owns
  Start/Stop/reconcile, reads a started component's provide-ports, obtains the live handle
  via a narrow provider interface, and registers it. Register-on-Start / deregister-on-Stop
  becomes a **structural property of the manager**, not something each store component
  re-implements. This is the ports-are-declarations model applied to stores.

**Why NOT exclusive (B1).** An earlier draft made `store-provide` exclusive to catch
duplicate ownership at `checkPortConflicts`. Adversarial review found this unsafe: the
manager calls `unregisterPorts` **only** from `RemoveComponent` (`component_manager.go:719`);
the reconfig (`restartComponentWithNewConfig`) and reconcile-stop (`stopAndRemoveComponent`)
paths delete the component + `registry.UnregisterInstance` but leave `cm.resources` stale.
An exclusive `store-provide:X` would therefore make a *restarted* owner collide with its own
lingering entry and fail reconfig — the exact silent-fallback this ADR exists to prevent.
So **duplicate-ownership detection moves to registry-population time**: `Register(instance,
handle)` errors (loud, at Start) if `instance` is already held by a different live
component. This keys on the real `InstanceName()`, is owned by the hooks we add (so the
reconfig swap is correct by construction — deregister-on-Stop then register-on-Start), and
does not depend on the manager's `cm.resources` symmetry. The pre-existing `unregisterPorts`
asymmetry (which also makes any exclusive port — today only `NetworkPort` — reconfig-fragile)
is filed as **separate framework work (gh#417)**, not a prerequisite here.

**`store-read` (evolve).** Consumer read declaration, two flavors:

- **Specific instance** (static consumer, e.g. a fixed single content bucket) → a real
  static edge; a strict upgrade over today's bucket-keyed `store-read`, now keyed by
  instance so it lines up with what refs carry.
- **Federation** (federated consumer — embedding, fusion — reads *whatever instance a ref
  names*) → a **capability edge**: "reads from the store federation." The specific instance
  is chosen by the producer at runtime, so this is the honest declaration.

The resulting graph is a **bipartite federation**:

```
[objectstore-A] ─provide─┐
[filestore-B]   ─provide─┤→ (store federation / registry) →┌─read─ [graph-embedding]
[objectstore-C] ─provide─┘                                 └─read─ [fusion gateway]
```

Every provider→federation and federation→consumer edge is graphed. The **only** thing that
stays dynamic is the *exact* runtime pairing (embedding read instance-C for entity-42) —
inherent to federation, and not a gap: you need "who can provide" and "who can consume,"
which the ports give you; nobody needs the per-ref edge statically.

### 3. Resolution — the registry contract

A small type (proposed home: `storage/storeregistry`, keeping `storage` a leaf and
`pkg/fusion` a leaf):

```go
// StoreRegistry maps a StorageReference.StorageInstance to the live store that
// backs it. Concurrent-safe. Populated by the ComponentManager from store-provide
// ports; resolved lazily per-fetch by consumers.
type StoreRegistry interface {
    // Streamable returns the streaming store for instance, and whether one is
    // registered right now. Consumers MUST call per-fetch and MUST NOT cache the
    // handle (see the lifecycle contract).
    Streamable(instance string) (storage.StreamableStore, bool)
}
```

The concrete impl also satisfies `fusion.StoreResolver` (`Store(instance) (storage.Store,
bool)`) by upcast, so fusion consumes the same registry with no change to `pkg/fusion`.

**Lifecycle contract (the two rules that keep it flow-consistent):**

1. **Register on Start, deregister on Stop** — the entry is lifecycle-bound to the owning
   component and owned by the manager. A reconfig (Stop→Start of a fresh instance) *swaps*
   the entry automatically, via the same restart mechanism that already honors reconfig for
   components.
2. **Consumers resolve per-fetch (lazy), never cache the handle** — body fetch is already
   per-entity at runtime, so this is natural. Each fetch gets the *current* handle; a
   stale/closed handle is never held. A fetch racing a Close just errors → hydration
   degrades for that entity and retries — no worse than today.

**Ownership.** The registrant (owner) owns `Close`; **borrowers must not Close**. This is a
change from today, where each consumer builds and Closes its own handle — it must be
explicit in the borrowing consumers (graph-embedding stops Closing a borrowed store).

**Injection.** A new nil-able `component.Dependencies.StoreRegistry` field (PayloadRegistry
/ LifecycleManager precedent — a framework-owned leaf type, not a pluggable external
surface). It is **constructed and owned by the `ComponentManager`** (`storeregistry.New()` in
the constructor) and injected via the single `buildComponentDependencies`, so every binary
that runs components through the manager gets a populated registry with no per-`main.go`
wiring to forget — this deliberately sidesteps the half-migrated-`main` failure class (a
`cmd/` that gets the wiring while another doesn't). A standalone/remote consumer that does
NOT use the ComponentManager constructs and populates its own registry instance.

**In-process / remote seam.** In-process, the registry holds local `objectstore.Store`
handles (direct method call, streaming). A standalone consumer (standalone fusion service,
semsource ADR-0006) populates its *own* registry with a NATS-backed remote store that
implements `StreamableStore` by RPC to the owner's store port. The framework ships the type
+ in-process population; remote-handle wiring is the cross-process consumer's job.

### Read contract (raw body, not envelope)

Orthogonal to resolution but recorded so producers and readers agree: **the store a ref
points at holds the RAW body bytes at `Key`** (written via `Put` / the fusion `CONTENT`
bucket, gh#395), *not* a `StoreContent` JSON envelope (whose `map[string]string` fields
corrupt non-UTF-8 and would embed as JSON noise). Consumers read raw via `Get`/`Open`.
Embedding already warns when a read starts with `{` (`worker.go:421`) — that stays as the
guard. The resolver does not unwrap envelopes; the write-format contract is the producer's
responsibility.

**Observability — keep the two failure classes distinct (M1).** gh#414's
`graph_embedding_content_unresolved_total` means *"no store registered for this instance"*
(a wiring/config fault). A registered store that errors mid-fetch (network blip, bucket
deleted, closed-mid-fetch) is a **different class** — an infra fault — and must NOT fold
into the generic `markFailed` "text extraction failed" path (`worker.go:270`), or we
regress the diagnosability gh#414 just bought. Add a distinct observable —
`graph_embedding_content_resolve_error_total` (instance resolved, fetch errored) — separate
from `content_unresolved` (instance not registered). An operator must be able to tell a
missing registration from a failing backend.

## Consequences

### Positive

- **Federation actually works** — a ref naming any provided instance resolves; offloaded
  bodies stop being silently excluded from embeddings/search (closes the gh#414 class at the
  root, not just loudly).
- **One resolver, both consumers** — fusion's dormant hydration activates and embedding's
  instance-blind fetch is retired, via the same registry.
- **Reconfig-correctness is structural** — owned by the manager's existing lifecycle, not
  re-implemented per store component. *More* correct than today (today a `store-read` bucket
  change is only honored if the consumer itself restarts).
- **Ownership conflicts are loud** — registry-population-time detection turns duplicate
  ownership into a startup error at `Register` instead of a silent registry clobber (and
  without the reconfig-fragile exclusive-port path — B1).
- **Flowgraph keeps its story** — store edges become a visible provider→federation→consumer
  bipartite structure instead of invisible in-process reach.
- **Leaves stay leaves** — `pkg/fusion` and `storage` unchanged as leaf packages; the
  registry and manager wiring live where the coupling already is.
- **Additive in wiring** — nil-safe Dependencies field, additive population, and
  registry-primary-with-`store-read`-fallback in embedding (below). No config or API breaks.

### Negative / cost

- **Behavior-changing by design (H2)** — this is additive in wiring but NOT output-neutral.
  Configs that wire no `store-read` today (e.g. `configs/semantic.json`) currently EXCLUDE
  offloaded bodies from embeddings (loud since gh#414); once refs resolve through the
  registry, those bodies WILL embed — shifting BM25/neural embedding and search output for
  the semantic tier. That is the intended gh#414 root fix, not a regression, but golden /
  search assertions may move and the `e2e:semantic` gate is mandatory before tag. Make the
  newly-included content observable (the resolve metric above); this is the *inclusion*
  mirror of `feedback_gate_silent_exclusion_flips_with_cost_ledger` — same discipline: the
  output delta must be observable, not silent.
- **The exact runtime store edge is not statically graphable** — inherent to federation
  (producer chooses the instance). Mitigated to a capability edge by the ports, but the
  per-ref pairing is dynamic by nature.
- **Cross-component lifecycle coupling** — a borrowed handle dies when its owner stops. Lazy
  per-fetch lookup + graceful fallback bounds the blast radius (next fetch misses/retries),
  but the coupling is real and must be documented in borrowing consumers.
- **New surface** — a port type, a provider interface, a registry type, and manager wiring.
  More than "component calls `registry.Register` in Start" — bought deliberately for
  flowgraph visibility + manager-owned reconfig correctness + ownership-conflict detection.

### Risks

- **Ordering** — mitigated by lazy lookup: a ref cannot exist unless its producer already
  ran and registered. No eager-before-register trap (contrast the buckets case,
  `feedback_eager_resource_creation_before_consumer_register`).
- **Handle staleness** — mitigated by the no-cache rule for BORROWED handles: the worker
  resolves the registry per-fetch and never stores the resolved handle (the owned `store-read`
  fallback store, by contrast, may be retained — the component owns and closes it). Enforce
  with a test that a post-reconfig fetch resolves the NEW handle, and never Close a borrowed
  store.
- **Instance-name drift** — the objectstore factory currently hardcodes
  `instanceName := "objectstore"` (`component.go:135`) and does not receive the
  ComponentManager map-key; the registry keys on `store.InstanceName()` (the value actually
  stamped into refs), so it is robust to that drift. But `store-provide:<instance>` should
  derive from the *store's* instance name, not the map-key, to stay aligned. (Worth a
  follow-up to thread the map-key into the factory so multi-instance objectstore gets
  distinct names — out of scope here.)

## Migration path (increments)

Additive in wiring but output-changing where refs newly resolve (H2); `e2e:semantic`
(touches the embedding path) is **mandatory** before any tag, per the breaking-change
discipline — the tier covers the seam and will surface embedding/search output shifts.

1. **Registry type + Dependencies field** — `storage/storeregistry` + nil-able
   `Dependencies.StoreRegistry`, built in both `cmd/` mains. No consumer yet.
2. **`store-provide` port + manager population** — non-exclusive port type, narrow provider
   interface (`ProvidedStores() map[string]storage.StreamableStore`) on storage components,
   ComponentManager `syncStoreRegistry` helper wired into `startSingleComponent` (register)
   and `stopAndRemoveComponent` / `restartComponentWithNewConfig` (deregister). objectstore
   Component declares `store-provide:<instance>` (token from its own instance name) and
   exposes its handle. Duplicate-ownership → loud error at `Register`.
3. **graph-embedding consumption (the live gap)** — a WORKER CODE change (M2), not a doc
   note: inject a narrow `StoreResolver` into the `Worker` and resolve
   `resolver.Streamable(ref.StorageInstance)` **per-fetch** inside `fetchTextFromStorage`.
   The registry-resolved handle is **BORROWED** — resolved fresh each fetch, never stored on
   the worker, never Closed by the worker (the owning storage component Closes it). The
   worker's existing `contentStore` field is **repurposed as the OWNED fallback** (built from
   a `store-read` port, Closed by the component) — used only when the registry cannot resolve
   the ref's instance, preserving single-bucket BM25 deploys (strictly additive). The
   component gate (`shouldFetchViaStorageRef`) allows the StorageRef path when the registry
   resolves the instance OR the fallback store is wired; otherwise it reports the exclusion
   loudly (gh#414) and extracts inline text. Add the resolve-error metric (M1).
4. **fusion adapter-ready** — the registry satisfies `fusion.StoreResolver`; wiring can pass
   it to `NewBodyResolver`. The **live** fusion cmd/ consumer (gateway/tool building
   `Engine` + `BodyResolver`) stays semsource convergence #6 (gh#411) — mirrors the B2
   adapter-without-live-consumer pattern (`feedback_dont_ground_on_no_producer_in_framework_binaries`).
5. **`store-read` federation flavor** — a flowgraph-CLASSIFICATION change (M3), not free
   visibility: add `StoreProvidePort` / `StoreReadPort` cases to `classifyInteractionPattern`
   AND `extractConnectionID` (`flowgraph/flowgraph.go`), keying provider `store-provide:X`
   and consumer `store-read:X` to the SAME connection id so the edge actually forms (today
   store-read falls through to `default: PatternStream` with a junk id and forms none).
   Federation-capability flavor for embedding/fusion; specific-instance flavor keeps the
   static edge. Deferred to Phase 2.

## Open questions

- **Provider interface shape** — the registry is a **standalone injectable type** (the noun);
  **`ComponentManager` owns population** (the verb), hooked next to the existing
  `registerPorts`/`unregisterPorts` calls in `startSingleComponent` /
  `stopAndRemoveComponent` / `restartComponentWithNewConfig`. No separate "store manager" —
  that would be a parallel lifecycle observer with no lifecycle of its own. Open detail:
  `ProvidedStores() map[string]storage.StreamableStore` on the storage component, read by
  ComponentManager after Start (leaning this, both register and deregister manager-owned for
  reconfig symmetry), kept as a small unit-testable `syncStoreRegistry` helper.
- **Does `store-read` need to be *enforced*** (a consumer may only resolve instances it
  declared) or purely advisory for visibility? Enforcement would re-introduce static
  wireability at the cost of federation flexibility.
- **Multi-instance objectstore naming** — thread the map-key into the factory (retire the
  hardcoded `"objectstore"`), or leave single-instance-only for now?
- **Remote store impl** — does semstreams ship a `StreamableStore`-over-NATS reference impl,
  or leave it entirely to the cross-process consumer? (Out of scope for gh#415; note the
  seam.)

## Related decisions

- **ADR-062** (deterministic graph fusion) — parent; #399 defined the `StoreResolver` this
  ADR populates; #400 canonicalized `StorageInstance`.
- **ADR-047 / ADR-048** — substrate-convention precedent (Lifecycle harness, BoundedDispatcher):
  framework provides the primitive + lifecycle; apps/consumers own work logic.
- **ADR-055/056** — StorageRef lifting onto EntityState at the ingest seam (the producer of
  the refs this resolver consumes).

## References

- gh#415 (this), gh#414 / #416 (loud fix), gh#411 (convergence tracker), gh#376 (parent ask).
- `pkg/fusion/hydrate.go`, `graph/embedding/worker.go`, `processor/graph-embedding/component.go`,
  `storage/objectstore/{store,component}.go`, `component/port_store.go`,
  `service/component_manager.go`.
</content>
</invoke>
