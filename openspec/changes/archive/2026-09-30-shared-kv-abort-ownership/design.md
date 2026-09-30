# Shared KV abort ownership — bounded experiment

base: e479605a55095027aee4b1ce87e5b70af7cf6c18

Status: proposed for independent design review under #1421.
Accepted inventory: /tmp/gh1421-predicate-smoke-inventory.md
SHA-256: cc9be085be8b4295e2ef57b61418b43db0425b6c6ce4b3c7e914629c3d1b4973
Author: semstreams-architect gh1433_inventory_design; root materialization.

## Question and options

Hosted smoke failure establishes a filtered-listing deadline followed by Client.Close drain timeout. Construction,
collection and terminal ownership remain unattributed. Both harnesses reach NewKVStore/KeysByFilter/native KeyLister.

| Option | Benefit | Cost or limitation |
|---|---|---|
| Preserve evidence only | No execution or source cost | Required CI failure and ownership question remain unresolved. |
| Observe actual smoke workload | Could locate a future failure phase | Another healthy run may answer nothing; expands per-harness instrumentation. |
| Tighten smoke cleanup or reconcile #1286 | Repairs local harness defects | Failure precedes churn and is a KV error; no attribution for listing or drain. |
| Deterministic shared-KV abort experiment | Tests production abort without previous facade rescue drain | Artificial schedule can establish an independent defect, not historical cause. |

Recommend last option once before choosing production repair.

## Scope and scheduling seam

One developer owns one private opt-in integration diagnostic in natsclient and focused checks. Reuse accepted
child-process containment and bounded stack-witness mechanics; remove forwarding facade, native-channel drain and
request-origin tracing. No production, SDK, existing smoke/owner-load harness, deadline, assertion, cleanup-baseline
or runner edits. Preserve archived observer design; use a new scoped evidence record under #1421.

One canonical pinned file-backed fixture has 5,000 matching diag.* keys. Simple grammar isolates shared ownership;
it does not reproduce the composite join. Exactly two fresh child processes: transparent control, then gated expiry.
Parent owns container; each child owns a separate NATS connection and listing task.

Both call real Client.NewKVStore(...).KeysByFilter with unchanged default five-second timeout. Private KeyValue
decorator forwards exact context/filters to native ListKeysFiltered. On success retain native handle and observe
Stop synchronously. KeyLister embeds native handle without overriding Keys; production receives native channel.
Control returns handle immediately. Gated case holds only successful constructor return, with no key receive,
forwarding, independent drain, additional Stop or transport fault. Native delivery can fill buffers before collection.

While actual framework child is live, require complete affirmative stack witnesses and goroutine identities for:
- Native forwarding at pinned jetstream/kv.go:1451.
- Native watcher callback delivery at pinned jetstream/kv.go:1290.

Reuse bounded witness helper; reject truncated evidence. Occupancy cannot replace both stack witnesses. Constructor
failure or no pair before expiry is GATE_NOT_REACHED; stop, no retry/substitute schedule. After both witnesses, wait
for the same child's natural DeadlineExceeded and release successful native constructor return. Production collection,
context checks and deferred Stop remain unchanged.

## Required observations

Record timestamps/errors for construction, release, production return, delegated Stop and Client.Close.
Control must return exact seeded set, invoke delegated Stop once and close successfully; failure stops next case.
Gated case must return nil keys and errors.Is(context.DeadlineExceeded), with one framework-delegated native Stop.
Record Stop error separately without changing production handling.

Capture complete snapshots with both senders blocked before expiry, after production return before Close, and after
Close. For each witnessed goroutine report later presence, frame and state. Absence is not explicit join; unknown,
truncated or unmatched evidence remains unknown.

Invoke Client.Close once with existing15s terminal allowance, never draining native Keys before/during/after. Record
error and connection state independently. Deadline plus sender still blocked after Close is a detected ownership
failure, not clean completion merely because the child exits. A15s drain failure reproduces the secondary class;
promptClose plus surviving work is a narrower finding. Neither attributes the initial hosted stall.

## Ownership and containment

Install lexical finalization before listing task launch. Release gate once on every exit; cancel test-owned operation
authority and observe task completion within finite work allowance; Close once; preserve primary finding and separate
cleanup errors. No native Keys rescue or repeated Stop. On cooperative failure retain last observation and use child
containment. Reuse one parent-owned Cmd.Wait goroutine, bounded output, immediate kill-plus-Wait finalization.
Child exit/Wait prove containment only.

Child test alarm40s, parentkill45s, Waitreserve5s. Fixture setup20s; canonical cleanup existing separate finitebudgets.
Parent native command alarm180s. No later case after controlfailure, missingwitness, unresolved ownership or integrityfailure.

## Preparation and sensitivity

Source review before execution. Focused checks without NATS verify:
- Exact context/filter/native-channel delegation and oneStop through realNewKVStore.
- Gate release and listing-task finalization on earlyexits.
- Typed expiry and nilpartialresults afterrelease under expiredchild.
- Evidence distinguishes surviving blocked sender, absence, unknown and truncation.
- Bounded parentkill/Waitownership.

Explicit channels and existing bounded witness polling; no stress/randomizedmatrix. One sensitivity mutation makes
classifier falsely accept supplied post-close blocked-sender witness; independent assertion must fail. Retain cpbackup,
original/mutated/restoredhashes; restore before nativeexecution. Claim only classification sensitivity.

## Execution bound and disposition

At most300s aggregate local command execution for focusedchecks, sensitivity, build/runnerwait, one nativecommand and
cleanup. Require210sremaining before nativeadmission. Canonical integrationrunner+hostlock+-race; no parallelheavywork.
Two-case diagnostic executes once. No repeat for witness/green. Retain source/diffhashes,command,toolchain,pins,
all admittedcase events,findings,Close observations,childWait andfixturecleanup.

| Result | Supported disposition |
|---|---|
| Blocked native work survives productionreturn andClose | Concrete abort-ownership defect; smallest repair against reproducer for separate review. |
| Controlled15sdrainfailure | Secondaryclass reproduced; capturedstate bounds repairquestion. |
| PromptClose/no surviving witness | Schedule did not reproduce; no nativejoin/historicalrepair claim. |
| Missingwitness/truncation/controlfailure/unresolvedtestownership | Inconclusive or diagnosticdefect; stop without anothernative run. |

Experiment does not repairrequiredCI or authorize#1435merge. #1286budget/unit reconciliation distinct; aggregatejoin
qualifies universaldead-budgetpremise. Waiver remains#1432only.

Task: Prepare/review one transparent abortexperiment; proveownership/evidencechecks; executeonce within allowance;
use observation to select concrete repair or record unresolved question. No productionchange in this unit.
