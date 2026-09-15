# Gated-DAG semantic-settlement migration

## Scope and evidence

This is a SemStreams-owned migration record for the two measured gated-DAG adopters. It records their observed source
seams so each repository owner can perform its own migration. SemStreams does not edit, build, or validate either
sister repository.

The accepted inventory used these reproducible checkpoints:

- SemSpec HEAD `5a9496eecc453747f4bc557b95444db6304c1420`, branch
  `hardfork/semstreams-lifecycle`, with tracked-diff SHA-256
  `4d264d7a61e8259ee4c3c629abfbeaf889b73f263fd4588d07f8a97eb01b6816`, staged-diff SHA-256
  `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855`, untracked-content SHA-256
  `cedfa283583e3d6672cf76a3b8212ef6bcf604b083b36248120a102931d04918`, and porcelain-state SHA-256
  `5707ccb2c2a6b2eb652110b4d1b9ce0bd9973d3944dab14a38cc708c181d6b10`.
- SemDragon HEAD `07f4de9b65887801ff18a7273d14233023049321`.

These identities make the read-only observations reproducible. They do not make uncommitted plans current product
truth and they do not claim either checkpoint still represents a sister repository when this guide is read later.

## The split contract

SemStreams separates two responsibilities:

- `gated-dag-dispatch` owns the adopter's domain definition of durable done and replay; and
- `jetstream-consumer-policy` owns typed settlement, heartbeat, lease validation, and exact native consume-handle
  mechanics.

The permanent typed API and removal of the old heartbeat helper reach `main` together in the final #759 cutover.
The non-default integration branch is staging, not a compatibility release. Nil/error is not a portable definition
of done, and a fast consumer receives no raw-message or exported no-heartbeat workaround.

The dispatch producer uses the logical unit ID as `Nats-Msg-Id`. Server deduplication applies only inside the stream's
configured `Duplicates` window. `Duplicates >= BackstopInterval` covers ordinary backstop-driven redispatch; it is
not an unbounded exactly-once guarantee. After that horizon, each adopter's durable already-complete or idempotent
replay check is load-bearing.

Each adopter therefore needs its own reviewed matrix for ACK, Retry, Terminate, and Quarantine. A generic
`func(context.Context, []byte) error` mapping cannot safely infer those decisions.

## SemSpec execution bridge

At the recorded checkpoint, SemSpec's execution bridge is registered in `cmd/semspec/main.go` and enabled in the
default and E2E configurations alongside gated-DAG. Its handler decodes the unit reference and polls `ENTITY_STATES`.
It returns success only after completed, failed, or recovery-exhausted graph evidence becomes durable. Translation,
prepare, and recovery paths write those states; ordered recovery can remove terminal and claim markers before a new
attempt.

Its current durable definition of done is therefore terminal `ENTITY_STATES` evidence, not callback completion by
itself. A replay must check that evidence before repeating work. The owner must decide how completed, failed,
recovery-exhausted, immutable poison, transient graph-read failure, and commit-ambiguous paths map to the four typed
settlement decisions.

The recorded `Start` retains only a cancel function. `Stop` cancels but does not retain, drain, or join the exact
native `jetstream.ConsumeContext`. If the execution bridge remains, its migration must:

1. retain the exact canonical consume handle returned at acquisition;
2. validate `HeartbeatDeliveryPolicy` from the same final consumer configuration used for acquisition;
3. implement `DeliveryWork` around the terminal-state authority and reviewed decision matrix;
4. inspect every `DeliveryResult` and stop the exact handle outside the callback on `OwnerStopRequired`; and
5. cancel, drain, and join that exact handle through the component's existing lifecycle owner.

The checkpoint also contained an uncommitted plan to remove gated-DAG and the execution bridge. That plan is a
collision to reconcile, not evidence that the shipped registered and enabled path was already absent.

## SemDragon staged questdag

At the recorded checkpoint, SemDragon ships and enables `questdagexec`. That component is not the gated-DAG dispatch
consumer described here. A separate `questdag` package contains a factory and generated registration descriptor, but
no product registry invokes `questdag.Register` and no shipped product configuration enables it. The path is staged,
not active.

The staged `questdag` handler decodes a dispatch, resolves graph, party, and membership state, reserves the member,
and calls `ClaimAndStartForParty`. Its current successful return means the reservation plus
`ClaimAndStartForParty` consequence committed. It short-circuits replay when the quest has advanced beyond posted.
That is a different definition of done from SemSpec's terminal completed/failed evidence.

The staged implementation also creates a consumer root with `context.Background`, retains cancellation, and does not
join an exact native consume handle. Before registration or enablement, the SemDragon owner must:

1. preserve a lifecycle context derived from the component owner rather than inventing a root;
2. retain and join the exact canonical consume handle;
3. define replay authority for reservation and `ClaimAndStartForParty`, including delivery after `Duplicates`;
4. decide typed settlement for malformed/inapplicable input, already-advanced work, transient graph or membership
   failure, reservation ambiguity, and partial `ClaimAndStartForParty` effects; and
5. prove the selected matrix across process replacement before enabling the component.

The existence of completion-writer code does not change this measured done contract: no non-test caller of that writer
was found at the checkpoint, and no production failed-marker writer was found for staged `questdag`.

## Typed transport composition

After the adopter's domain matrix is accepted, compose it with the permanent transport API:

```go
policy, err := natsclient.ValidateHeartbeatDeliveryPolicy(
	ctx,
	cfg,
	heartbeat,
	natsclient.ImmediateDeliveryRetry(),
	func(
		workCtx context.Context,
		attempt natsclient.DeliveryAttempt,
		data []byte,
	) (natsclient.DeliveryDecision, error) {
		return handleDispatch(workCtx, attempt, data)
	},
)
if err != nil {
	return err
}

handle, err := client.ConsumeStreamWithConfig(ctx, owner, cfg,
	func(msgCtx context.Context, msg jetstream.Msg) {
		result := natsclient.ConsumeDeliveryWithHeartbeat(msgCtx, msg, policy)
		recordAndReact(result)
	},
)
```

`handleDispatch` and `recordAndReact` are adopter-private placeholders. The adopter retains `handle`, closes admission
on owner-fatal results, stops that exact handle outside the callback, and joins it during Stop. Native message and
settlement authority do not escape into domain work.

## Verification owed by each adopter

Before enabling or retaining its migration, each sister owner should prove:

- the production registry and configuration select the intended component;
- the same final consumer configuration drives validation and acquisition;
- positive settlement follows the adopter's durable consequence;
- redelivery short-circuits through durable already-complete or idempotent evidence both inside and beyond
  `Duplicates`;
- poison, transient, ambiguous, and partial effects follow the reviewed decision matrix;
- process replacement does not duplicate the domain effect; and
- Stop closes admission and joins the exact native consume handle.

SemStreams records these obligations only. Completion evidence and repository mutation remain with the respective
SemSpec and SemDragon owners.
