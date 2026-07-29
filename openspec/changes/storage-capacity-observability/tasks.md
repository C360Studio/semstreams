## 1. Provisioner guard (do this first — it is the data-loss boundary)

- [x] 1.1 Add a name guard in the stream provisioner that refuses any stream whose name carries the
      `KV_` or `OBJ_` prefix, failing closed and naming the resource plus the owner that legitimately
      provisions it. Guard at `createStream` (`config/streams.go:358`) and at declaration validation,
      so neither the operator map (`:254`) nor a port-derived name (`:289`) can reach it
- [x] 1.2 Add a POSITIVE guard test: declaring `KV_ENTITY_STATES` and an `OBJ_*` name in `cfg.Streams`
      and as a port stream name must each fail, loudly. A test that merely proves they are exempt from
      the bounds requirement does NOT cover this — the hazard is the reconciler writing to them
- [x] 1.3 Add a test proving the refusal is by prefix and NOT by catalog membership, using a `KV_*`
      name outside the descriptor catalog

## 2. Inventory

- [x] 2.1 Add an account-scoped inventory enumerating JetStream via the paged listing that returns full
      stream info (config + state together), not a names listing plus a describe per resource
- [x] 2.2 Attribute `KV_*` resources via `graph.OwnerOf(bucket)` (`graph/kvcatalog.go:213`) after
      stripping exactly ONE leading `KV_`; report non-catalog resources as unattributed
- [x] 2.3 Model capacity as three distinct states — bounded, unbounded, unknown — and prove in tests
      that no two collapse
- [x] 2.4 Bound collection: interval-driven with a timeout, never on the component-start or health path,
      degrading to last-good-with-timestamp, naming the producing process in the report
- [x] 2.4b Expose the collection interval as operator configuration with a schema entry and a JSON
      round-trip test (a Go struct field reachable only from a composition root is not operator
      configuration) — may move to section 4 with the rest of the operator surface
- [x] 2.5 Add unit tests for attribution, the doubled-prefix case (`KV_KV_FOO` → `KV_FOO`, unattributed),
      catalog-removal reporting unattributed, and unknown capacity
- [x] 2.6 Add a real-NATS integration test proving a resource this process never created or touched is
      still enumerated

- [x] 2.7 Reconcile the info listing against the name listing and publish names-minus-infos as
      real-named unknown-tier/unknown-capacity rows. The server excludes offline streams from the info
      listing (`Missing`/`Offline`, dropped by nats.go) but NOT from the name listing, so without this
      the inventory silently omits exactly the resources nobody can read. Dedupe resources by name
      before sorting: nats.go advances its page offset by `len(resp.Streams)` while the server's cursor
      also passed the excluded entries, so >256 streams plus one offline stream yields overlapping
      pages and duplicate rows

- [ ] 2.8 Prove the undescribable-resource path against a genuinely offline stream. Unit tests cover the
      reconciliation against a fake and integration pins the assumption it rests on (name listing ⊇ info
      listing on a healthy server), but no test produces a real offline stream — that needs a two-image
      run: write state under a newer `nats:` tag, restart on an older one so a persisted config requires
      a higher API level than the running binary. Feasible but its own piece of work; do not fold it in
      silently, and do not claim the offline path is end-to-end proven until this exists

## 3. Growth and pressure

- [x] 3.0 Declare the report bucket in the descriptor catalog (`graph/kvcatalog.go`) — operational class,
      owner-only writes, no-lifecycle retention, bounded History following the `GRAPH_STATUS` precedent —
      and publish one key per resource each collection, deleting the key for a resource that disappeared.
      MOVED FORWARD from section 4: the per-key history IS the growth series 3.1 needs, so building a
      separate sample store first would be building a mechanism to immediately replace
- [x] 3.1 Derive growth rate from SUCCESSIVE published observations (Δbytes over Δt across the report
      bucket's per-key revisions), never from a single snapshot's `FirstTime`/`LastTime` span. A snapshot
      cannot separate sustained growth from churn at stable size: a KV bucket under `History` 1 compacts
      old revisions, so it holds roughly constant bytes while its timestamps span a long window, and
      bytes-over-span would report growth — and project exhaustion — for a bucket that never grows.
      Report unknown rate until at least two observations exist, rather than extrapolating from one
- [x] 3.2 Project headroom and time-to-threshold; suppress both for unknown-capacity resources
- [x] 3.3 Derive pressure as the worse of a proportional-headroom band and a time-to-threshold band,
      reporting which input raised it
- [x] 3.4 Resolve thresholds through a supplier seam rather than a value frozen at construction. NOTE
      (revised): a RESTART is the supported way to apply a threshold change — a stale threshold only
      evaluates old numbers visibly and destroys nothing, unlike a stale value driving a durable
      resource. The seam is retained because it already exists and costs one function type (an operator
      can retune mid-incident without restarting their monitoring), but nothing depends on it and no
      further work should extend it
- [x] 3.5 Add a JSON round-trip test for the operator-facing threshold configuration
- [x] 3.6 Add unit tests for pressure transitions, the rate-raised-before-headroom case, the
      no-pressure-for-unknown-capacity case, and restart-survival of the projection

## 4. Operator surface

- [x] 4.1 Publish Prometheus metrics for usage, headroom, growth rate, time-to-threshold, and pressure
      state, labelled by resource and owner
- [x] 4.2 Ship an example alert rule (or health-status surface) alongside the metrics so the pressure
      gauge has a consumer at merge time and does not become a phantom signal
- [x] 4.3 Expose pressure in component health STATUS without degrading readiness, and add a test that
      `critical` pressure fails no readiness or health gate
- [x] 4.4 Implement the operator surfaces as CONSUMERS of that bucket — an HTTP route reading it, and
      the alert rule from 4.2 driven by `Watch` — so there is one produced truth and no surface can
      disagree with another. Add a test that two surfaces cannot diverge because neither recomputes
- [x] 4.5 Report per-tier declared-versus-account-limit comparison via `js.AccountInfo`
      (`config/streams.go:192`), honoring the `-1`-means-unlimited sentinel (`:227`); never sum memory
      and file tiers together
- [x] 4.6 Report an unbounded account limit as unbounded and its over-commitment comparison as
      not-applicable — note testcontainers reports unlimited by default (`config/streams.go:220-223`),
      so this is the default integration-test path
- [x] 4.7 Name unbounded resources explicitly; never represent them as having headroom. Couple this with
      rendering not-evaluated rows VISIBLY: an unbounded resource carries no pressure state (neither band
      has an input), so any surface that filters on `state != normal` would make exactly the unbounded
      resources invisible — the opposite of what 4.7 exists to do
- [x] 4.8 Do NOT key any alert on a row disappearing from the report bucket. Reclamation is eventually
      consistent under concurrent producers, so a row may transiently vanish and return; alert on the
      row's contents (pressure, staleness) instead
- [x] 4.9 Publish a report-collected timestamp gauge so 4.8's STALENESS axis is actually alertable.
      Without it, a collector that silently stops is indistinguishable from a calm account through the
      metrics: Prometheus stamps SCRAPE time, not data time, so `timestamp()` cannot substitute and the
      series keeps looking fresh forever. The gauge's VALUE must be the collection time, making
      `time() - gauge > horizon` the alert. A monitoring surface that cannot report that it stopped
      monitoring is this capability's own phantom-signal class. Note `metric.MetricsRegistrar` takes a
      `prometheus.Gauge`, so a `GaugeFunc` does not fit the existing registrar — set it on the
      collection path instead. The rule file currently documents the gap and forbids approximating it
      with an absence expression; delete that caveat when this lands

## 5. Ordinary stream bounds

- [x] 5.1 Require explicitly declared finite `MaxAge`, finite `MaxBytes`, and discard policy on ordinary
      streams; fail readiness naming the stream, its declaration source, and the missing field. A silent
      framework default (today `MaxAge` 7d at `config/streams.go:387,390`) must NOT satisfy the
      requirement
- [x] 5.2 Name the owning component in the diagnostic where the declaration source records one; the
      framework-constant (`:242-249`) and operator-map (`:254`) paths carry no component attribution, so
      either plumb an owner through or report the declaration source instead of a guessed owner
- [x] 5.3 Make discard policy an explicit declaration field instead of the hardcoded
      `Discard: DiscardOld` (`config/streams.go:430`), and state in the declaration diagnostic what
      `DiscardNew` does at the ceiling (producer-side `503 err_code=10077`)
- [x] 5.4 Add the expiring migration override: names resource, owner, and expiry; readiness reports every
      active override; readiness fails once an expiry passes; an override without an expiry is rejected
      at validation. Without this the bounds requirement is a flag day for every component-derived
      stream (`config/streams.go:303-306`) and every sister-repo config
- [x] 5.5 Extend the drift reconciler (`config/streams.go:435-471`, today subjects + duplicate window
      only) to reconcile `MaxAge`, `MaxBytes`, and discard drift, touching no ungoverned field
- [x] 5.6 Fail readiness on non-editable drift (storage tier, retention policy) reporting observed and
      declared, instead of the current silent ignore
- [x] 5.7 Add real-NATS integration tests for drift repair, non-editable-drift readiness failure,
      declared-discard-policy creation, and override expiry behavior
- [x] 5.8 Add the ARCHIVAL classification (#729): permanent by declaration, naming stream + owner +
      why permanence is the contract; rejected if owner or reason is absent. Readiness reports it as a
      named permanent exception STRUCTURALLY distinct from a time-limited override — an archive whose
      override can only be renewed forever trains operators to renew without reading, which is what
      makes genuinely time-limited overrides invisible. SemMachina's `CAMPAIGN_LEDGER` is the live
      consumer and its declaration was offered as a test fixture
- [ ] 5.9 Evaluate an archival stream's pressure against the ACCOUNT TIER ceiling (#729). It has no
      limit of its own, so the account limit is its only ceiling; reporting it unevaluable would mean
      declaring a stream archival silently removes it from the surface that would warn about it —
      capacity matters MORE for a stream that can never evict, since it is the only lever left
- [ ] 5.10 Follow the bounds requirement to `natsclient.Client.EnsureStream` at CREATION (#729/#730):
      section 1 already took the prefix refusal to that seam, and if bounds do not follow, a direct
      caller becomes the supported route around the requirement. LIVE IN-REPO INSTANCE, confirmed on a
      running stack: `component/registry.go:914-926` creates `COMPONENT_CAPABILITIES` through
      `EnsureStream` with `MaxAge: time.Hour`, no `MaxBytes`, and no `Discard` — an ordinary stream,
      created by framework code, bypassing the contract section 5 just established. The live server
      reports `max_bytes=-1 discard=old`, neither of which anyone chose. It carries
      `MaxMsgsPerSubject: 1` so it is bounded per-subject in practice, but not by bytes and not by
      declaration. Fix this one as part of the slice — a framework that exempts itself from its own
      contract cannot ask sister repos to honor it
- [ ] 5.11 Report declared-versus-observed divergence when `EnsureStream` binds an EXISTING stream
      (#730). Today `natsclient/stream.go:141-145` returns the existing stream and discards the
      caller's `cfg` in silence, so a stream two components declare has its limits set permanently by
      boot order with no diagnostic on either side. Report only — do NOT restamp, since a non-owner
      silently rewriting another owner's config is worse than the drift
- [ ] 5.12 State who owns a shared stream's limits (#730). If the answer is "the declaring component,
      consumers use GetStream", document that as the contract rather than leaving it inferred from the
      single `agentic/agentrun/agentrun.go:697-718` precedent a sister repo had to reverse-engineer.
      This is now load-bearing rather than documentation hygiene: 5.5 made the provisioner reconcile
      retention, so two processes declaring the same stream differently no longer merely resolve by
      first-boot — they FLAP, each repairing the other's value on every boot. A provisioner cannot
      detect this locally (it sees its own declaration and the live config, never the other
      declaration), so the ownership statement is the only thing that prevents it; the repeated-repair
      log from 5.5 is the only thing that reveals it after the fact

## 6. Documentation and gates

- [ ] 6.1 Write the storage pressure runbook: reading the report, correcting capacity ahead of the
      projection, what each pressure state does and does not mean, and the `DiscardNew` ceiling behavior
- [ ] 6.2 Record that pressure is report-only and that admission control is deferred behind a checkable
      gate: this change merged; the rejection path proven to classify transient and NAK; projection
      verified against observed outcome on at least three real resources with no `critical` that
      resolved without operator action
- [ ] 6.3 Seed or correct the capability home — `openspec/specs/nats-streaming/spec.md` is a publish-path
      capability whose Purpose is still a `TBD` stub; stream provisioning is a separate capability and
      must not be filed under it
- [ ] 6.4 Confirm no EXISTING catalog row's declared retention policy changed, no retention Kind was
      added, and `graph-retention`, the acquisition seam, and ADR-068/073 are untouched. Adding the 4.4b
      report-bucket row is in scope and expected; changing how any existing bucket is governed is not
- [ ] 6.5 Run lint, `go test -race ./...`, tagged integration on touched packages, contract tests, and
      `task schema:generate` with no uncommitted drift
- [ ] 6.6 Run a relevant e2e tier before merge. This is REQUIRED, not conditional: tasks 1.1 and 5.1
      change stream provisioning and can fail boot, which is the breaking-change class the house rule
      covers regardless of pressure staying report-only
