# Design — discovery under stream shapes (gh#810, gh#822)

**Status: DRAFT, awaiting Fable shape review.** Written by an execution session against the
six binding constraints recorded in `tasks.md`. Constraint 6 requires a recorded Fable review
of new exported `graph`/`natsclient` surface *before* invention — this document is the thing to
review, so that the review happens against a grounded proposal rather than against code.

Every claim below is cited to `file:line` at `c05a11fb` (main) or to the pinned dependency
source. **Three of the six constraints do not survive contact with the code as written** — that is
the substance of this document, and each is flagged with what to rule on instead.

---

## 1. Verified ground truth

| Claim | Verified at | Result |
|---|---|---|
| Stream provisioning seams | `natsclient/client.go:877`, `natsclient/stream.go:138`, `natsclient/stream.go:488`, `config/streams.go:700` | **FOUR, not three** — see §2.1 |
| Framework provisions streams before components start | `cmd/semstreams/main.go:121` (step 5) vs component build at step 8+ | **Confirmed** — see §2.2 |
| A second pub-ack detector already exists | `gateway/graph-gateway/component.go:1475`, used at `:1699` | Confirmed; closed-set + 256-byte cap |
| `jetstream.PubAck` carries a `val` key the hand-list omits | `nats.go@v1.52.0/jetstream/publish.go:125-141` | Confirmed — 5 json keys |
| …and the **server** emits more than the client type declares | `nats-server/v2@v2.12.4/server/stream.go:257-265` | **7 keys + `error`** — see §2.3 |
| `DecodeQueryReply` has zero production callers | `git diff main origin/feat/gh810-…-impl` | Confirmed |
| A production in-repo `tool.list` requester exists | grep across all non-test packages | **No such caller exists** — see §2.4 |
| `RequestClassified` is the shared request seam | `natsclient/errors.go:257` | **69 production call sites / 31 files** |
| Core-NATS vs JetStream ports are distinguishable | `component/port.go:20`, `processor/agentic-tools/config.go:146` | Yes, but insufficient — see §2.5 |

---

## 2. The five findings that change the shape

### 2.1 There are four provisioning seams, and two in-code comments each say "three"

`config/streams.go:357` enumerates them as "this provisioner, `Client.EnsureStream`, or
`Client.CreateStream`". `natsclient/stream.go:567` says of consumer auto-create: *"THE SAME TWO
GUARDS AS EVERY OTHER PROVISIONING SEAM. This is a third one."* Neither list is the union. The
union is four:

1. `config.StreamsManager.EnsureStreams` → `js.CreateStream` (`config/streams.go:700`) — the boot provisioner
2. `natsclient.Client.CreateStream` (`natsclient/client.go:877`) — unconditional create
3. `natsclient.Client.EnsureStream` (`natsclient/stream.go:138`) — get-or-create
4. `natsclient.Client.ensureStreamForConsumer` (`natsclient/stream.go:488`) — consumer auto-create

**This is good news, not a complication:** all four already call the same two fail-closed guards
(`CheckOrdinaryStreamName` + `CheckStreamBounds`, returning `errs.WrapFatal`). The capture check is
the *third guard in an established pattern*, not a new mechanism. **Ruling wanted:** confirm "all
three" in constraint 1 means the union of four, and that the fix includes correcting both stale
comments.

One asymmetry to rule on explicitly. At seam 3 the bounds check deliberately sits *inside* the
not-found branch, because "refusing the second would make a non-owner unable to read an existing
stream, which is not its call" (`natsclient/stream.go:179-182`). **Subject capture does not share that
logic** — a stream that captures a declared request/reply subject is harmful whether this caller
created it or merely bound it. Recommendation: the capture check runs on the *bind* path too,
i.e. outside the not-found branch, unlike bounds.

### 2.2 Constraint 1's guard, as literally written, cannot fire in gh#810's own deployment

This is the important one.

`cmd/semstreams/main.go:121` provisions streams at **step 5**. Components are constructed at step 8
and started later. So at the moment the boot provisioner runs, **no component has subscribed to
anything**. A registry populated by subscription — which is what #847 built, hooking
`SubscribeForRequests` (`natsclient/request.go:347` **on the #847 branch**) — is *empty* at the seam constraint 1 names.

#847's own comment reasons its way to the opposite conclusion from the same fact:

> *"Streams are typically provisioned BEFORE components subscribe, so a check that only ran when a
> stream is created would see no declared subjects yet and catch nothing."*

That reasoning is **correct about the ordering and wrong about the remedy**. It concluded "check at
subscribe time instead"; the actual remedy is that the registry must not be subscription-populated.
If declared subjects are resolved from configuration *before* step 5 — and they can be, because
`*config.Config` is fully loaded by then — the provisioning seam sees the complete set and the
check fires exactly where constraint 1 wants it.

**Both directions still need covering,** because streams are also created at runtime after
components are up (`processor/gated-dag/component.go:134` calls `EnsureStream` during component
start). So:

- **Provisioning seams** (all four): check the stream being created/bound against the *declared*
  registry. Synchronous, fail-closed. Catches "stream provisioned after the subject was declared".
- **`SubscribeForRequests`**: check the subject against *existing* streams. Synchronous,
  returns an error. Catches "subject served on a deployment whose streams already capture it".

Neither alone closes the class; together they are order-independent. **Ruling wanted:** confirm
the second check point is in scope — it is not in constraint 1, and #847 was drafted partly *for*
putting the guard there, which risks the rework over-correcting into removing it.

### 2.3 Constraint 3 fixes `val` and still misses two live ack shapes

Constraint 3 says derive the ack key set by reflection over the pinned `jetstream.PubAck`. That
does fix the `val` gap it names. It does **not** produce a correct closed set, because the client
type is not the wire contract:

| Source | JSON keys |
|---|---|
| `jetstream.PubAck` (client, v1.52.0) | `stream`, `seq`, `duplicate`, `domain`, `val` |
| `server.PubAck` (server, v2.12.4) | + `batch`, `count` |
| `server.JSPubAckResponse` (the actual reply envelope, `server/stream.go:241`) | + `error` |

The deployed server is **`nats:2.14.4-alpine`** (#791) — newer than the 2.12.4 source above, so the
field set can only have grown further. A batch-publish ack
(`{"stream":"X","seq":1,"batch":"…","count":3}`) fails closure under a key set derived from the
client type, is therefore classified "not an ack", and decodes to an empty catalog. **That is
gh#810 again in a new spelling** — which is exactly the failure mode the derivation was meant to
retire.

Three options, and this is a genuine judgment call:

- **(A) Derived set ∪ documented server-only keys** (`error`, `batch`, `count`), with a test that
  fails if reflection over `jetstream.PubAck` yields a key outside the union. Honours constraint 3's
  intent (drift is caught by a test, not by silence); residual risk is future server-only fields.
- **(B) Positive discriminator, open set**: `stream` non-empty string + `seq` number, no closure.
  Immune to every future field. Cost: a legitimate reply carrying exactly those two top-level keys
  is rejected.
- **(C) Closure over the *reply* contract instead of the ack**: `stream` + `seq` present and no
  envelope key (`data`/`request_id`/`timestamp`). Future-proof on the ack side; leans on the
  envelope contract staying closed.

**Recommendation: (A)**, because the drift becomes a red test rather than a silent miss, and
because at the request seam (§2.4) the false-positive cost of (B) lands on 69 call sites. Fable's
call.

### 2.4 Constraint 5's "motivating caller" does not exist, and the right seam is `RequestClassified`

Constraint 5 requires `DecodeQueryReply` to ship with its motivating `tool.list` caller wired.
**There is no in-repo production requester of `tool.list`.** The framework *serves* it
(`processor/agentic-tools/component.go:169`); the only in-repo requester is the e2e scenario at
`test/e2e/scenarios/crud-tools/scenario.go:436`. The real callers are sister repos.

So constraint 5 cannot be satisfied as phrased — which is a signal about the seam, not about the
constraint's intent. The intent is "no zero-caller surface", and the seam that satisfies it is
`natsclient.Client.RequestClassified` (`natsclient/errors.go:257`): **69 production call sites
across 31 files**, including every graph-query proxy, both agentic-tools graph executors,
`pkg/projection/mutation_client.go`, the gateway, and the e2e client the crud-tools scenario uses.

Rejecting the ack there:

- closes the class for every requester at once, including callers that never touch the `graph`
  package — and `tool.list` is agentic, not graph, so a `graph`-package decoder was never on its
  path in the first place;
- gives the gateway's private detector somewhere to retire *into*, satisfying constraint 2 by
  consolidation rather than by moving a duplicate one package over;
- makes the sister repos' `tool.list` calls correct for free, since they call through `natsclient`;
- removes the need for `graph.DecodeQueryReply` to exist at all in this change.

**Ruling wanted:** constraint 2 says retire the gateway's detector "*into* the canonical decoder",
naming `graph.UnwrapQueryResponse`'s neighbourhood as the home. §2.4 argues the home is
`natsclient`, one layer down, and that the `graph` decoder then needs no ack branch. These are
incompatible; one of them should be withdrawn explicitly rather than both being half-built again.

A behavioural note that belongs in the ruling: `RequestClassified` returning a typed error where it
previously returned bytes **changes the error surface of 69 call sites**. That is the correct
direction (an ack was never a valid reply), but it is a framework-wide behaviour change and
therefore lockstep-relevant, which is precisely the sort of thing constraint 6 exists to catch.

### 2.5 The registry needs a request/reply marker that ports do not currently carry

For the provisioning-time check to work (§2.2) the framework must know, from configuration, which
subjects are **request/reply**. `component.PortDefinition` distinguishes `Type: "nats"` from
`Type: "jetstream"` (`processor/agentic-tools/config.go:141-150`), but that is not the needed
distinction: a core-NATS *pub/sub* subject covered by a stream is harmless — the subscriber still
receives the message and the stream also stores it. The breakage is specific to request/reply,
where JetStream's ack wins the race and the responder's reply is discarded.

So the declared set is "subjects passed to `SubscribeForRequests`" — accurate but runtime, which is
the ordering problem again. Resolving it needs one of:

- a new explicit attribute on `PortDefinition` (e.g. `RequestReply bool`, additive, schema
  regeneration required), resolved from config at boot; or
- a framework-level declaration API components register into at construction, before step 5.

Either way, the drift guard is the same shape that already works in `graph-query`: **`SubscribeForRequests`
asserts its subject is in the declared registry**, so a subject served without being declared is a
startup error and the two sides cannot drift. This is new framework surface and is the main thing
constraint 6 is pointing at.

Also worth ruling on while here, adjacent and one line away:
`processor/agentic-tools/component.go:171` treats a `tool.list` **subscribe failure as a `Warn`** and
continues serving with no discovery at all — the same "log line where a boot error belongs" class
constraint 1 is fixing. In scope or its own issue?

---

## 3. Proposed design

Three parts. Parts A and B are independent; C is small and unblocks SemSource (gh#822).

**A. Declared-subject registry + capture guard**
One registry, resolved from configuration before the boot provisioner runs, populated from
request/reply port declarations (§2.5). Consulted synchronously at all four provisioning seams
(§2.1) as the third fail-closed guard beside `CheckOrdinaryStreamName` and `CheckStreamBounds`,
returning `errs.WrapFatal`. Consulted again, synchronously and as a returned error, in
`SubscribeForRequests` for the reverse ordering. `SubscribeForRequests` additionally asserts its
subject is declared, so the registry cannot drift from what is served.
Reuse #847's `SubjectFilterCaptures` / `FindSubjectCaptures` and the `SubjectCapture.Error()`
message — the primitives and the three-fact operator message are sound and mutation-checked
(`natsclient/subject_capture.go`). Extend matching to wildcards on **both** sides (constraint 4);
today it handles the filter side only.

**B. One pub-ack detector, at the request seam**
Retire `gateway/graph-gateway/component.go:1475` into a single detector in `natsclient`, called
from `RequestClassified` (§2.4). Key set per the option Fable picks in §2.3. Test asserts against
fixture ack **bytes**, not against the same reflection that built the set (constraint 3), and
includes a batch-ack fixture and an `error`-bearing fixture. `graph.DecodeQueryReply` is **not**
built.

**C. Exported request-subject list (gh#822)**
#847's `QuerySubjects()` plus the both-directions handler cross-check
(`processor/graph-query/query.go`) is the right shape and should be kept. Its **test** is the
defect: it compares the exported copy against its own backing slice. The replacement drives
`setupQueryHandlers` against a recording client and compares *actually subscribed* subjects against
`QuerySubjects()` in both directions. Once A lands, this list is one input to the registry rather
than a parallel declaration.

---

## 4. What survives from PR #847

**Keep:** `SubjectFilterCaptures`, `FindSubjectCaptures`, `SubjectCapture` + its `Error()` message,
the `QuerySubjects()` export and the runtime handler cross-check, and the token-semantics
documentation (the "why this cannot be a prefix test" rationale is correct and hard-won).

**Discard:** `ReportSubjectCaptures`'s advisory-log policy and its detached-goroutine call site;
`graph.DecodeQueryReply` / `IsPublishAck` / `ErrPublishAck` as a `graph`-package surface; the
hand-listed ack key set; the tautological subject-export test.

**Correct:** `SubjectFilterCaptures` handles wildcards on the filter side only.

---

## 5. Questions for Fable, consolidated

1. **§2.1** — "all three seams" = the union of four? And correct the two stale in-code enumerations?
2. **§2.1** — capture check on `EnsureStream`'s *bind* path too, unlike bounds?
3. **§2.2** — is the synchronous `SubscribeForRequests` check in scope, alongside the provisioning
   seams? (Not in constraint 1; the rework risks deleting the only check that fires today.)
4. **§2.3** — ack key set: (A) derived ∪ documented server keys + drift test, (B) positive/open, or
   (C) closure over the reply contract? Recommendation: (A).
5. **§2.4** — detector home: `natsclient.RequestClassified` (69 callers) rather than a `graph`
   decoder — which withdraws constraint 2's stated home and constraint 5 with it. Accepting also
   accepts a framework-wide error-surface change at 69 call sites.
6. **§2.5** — request/reply declaration: new `PortDefinition` attribute, or a registration API?
   This is the new framework surface constraint 6 governs.
7. **§2.5** — is `agentic-tools`' `Warn`-on-subscribe-failure in scope, or its own issue?

**gh#842's conditional deferral rides on question 3 and 6 together:** the deferral is valid only if
a synchronous fail-at-boot guard ships in .160. A guard that is synchronous at seams no deployment
reaches before its components start (§2.2) would satisfy the letter and reproduce the exact
false-premise that drafted #847.
