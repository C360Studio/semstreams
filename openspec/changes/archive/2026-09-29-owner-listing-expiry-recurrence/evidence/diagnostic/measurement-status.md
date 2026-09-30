# #1421 measurement diagnostic: focused evidence and one native pass

Source revision: `586d6e6a4c35006efc5e2d6e3e0453192d43189b`; branch `codex/gh1421-listing-expiry-recurrence`.
Final temporary source SHA-256: `f50543bbd69effa1cb29f0e45b0e3c2c59a1bd401aee05598eec794ef8d16d71`.
Archived source SHA-256: `cbfd896bf49834ab5278c7c6a8c8fee50eb8fa6a2e301a975c906d7c134c7523`.
Go toolchain: `go1.26.4 darwin/arm64`; CI recurrence used Go 1.26.8. SDK: `nats.go v1.52.0`.
The fixture is configured for `2.14.4-alpine@sha256:f2123f533c2b0cada0a5c5ec434fb2b8cfe1cf220215ef9d7517e1372917ad66`.
The pinned local NATS image was inspected after the native pass: image ID and RepoDigest both
`sha256:f2123f533c2b0cada0a5c5ec434fb2b8cfe1cf220215ef9d7517e1372917ad66`.

The approved measurement changes only the private opt-in diagnostic. The retained source and exact diff are adjacent.
The recorder keeps 128 current-goroutine request/response stack snapshots, fails on overflow or stack truncation,
and classifies deletion origin only from source-specific frames. Unmatched deletion remains `unknown`.
The test still requires the exact 1,024-key set for control and `errors.Is(context.DeadlineExceeded)` with nil keys
for the retained deadline schedule; it checks delegated Stop invocation and owned cleanup separately.

## Focused runs

All integration-tagged executions used `scripts/run-integration-tests.sh` (host lock, `-race`, `-p 2`) with a narrow
`-run`, explicit `-timeout=180s`, `-v`, and `./natsclient`. The runner's own fixed `-timeout=20m` appears first;
the explicit later flag is the effective test alarm. No opt-in native case was enabled.

1. Baseline before review fixes: command
   `scripts/run-integration-tests.sh -timeout=180s -run '^TestKVFilterDiagnostic(EarlyFinalizer|KillJoinBound|ExpiredGate|TraceBoundedAndUnknown)$' -v ./natsclient`.
   Log `focused-baseline.log`, file birth 2026-09-29 23:38:51 UTC, modified 23:38:56 UTC. PASS, package 1.510s.
2. Controlled mutant: backed up source with `cp`; both copies SHA-256
   `a51fb72d6803c0b6b84e7706c768f5e2f1ce0b158d0182ba3449a831fe3e92fb`.
   The one-line valid mutant changed the origin classifier's default `unknown` to `framework_delegated_stop`;
   test code and runner were unchanged. Command
   `scripts/run-integration-tests.sh -timeout=180s -run '^TestKVFilterDiagnosticTraceBoundedAndUnknown$' -v ./natsclient`.
   Log `mutation-false-origin.log`, birth 23:39:43 UTC, modified 23:39:46 UTC. Intended assertion failed:
   a deletion request with no owner frame was falsely attributed to framework Stop. Package reached test and failed.
3. Restored baseline checksum matched the backup byte-for-byte before review fixes. After fixes, command
   `scripts/run-integration-tests.sh -timeout=180s -run '^TestKVFilterDiagnostic(EarlyFinalizer|KillJoinBound|ExpiredGate|TraceBoundedAndUnknown|CompletionJoinBound)$' -v ./natsclient`.
   Log `focused-restored.log`, birth 23:41:03 UTC, modified 23:41:07 UTC. PASS, package 1.528s.

The mutation demonstrates sensitivity only to false positive origin attribution at the tested source snapshot.
The focused checks do not establish native lifecycle behavior. Named schedules are sufficient; no randomized PBT
is used because this diagnostic compares two deterministic schedules and an exact seeded set.

## Native admission, execution and observed limits

At 23:42:08 UTC, the team initially applied a conservative continuous wall-clock cutoff of 23:43:35 UTC.
Only 87 seconds remained against the accepted 130-second setup/child/teardown envelope, so admission was refused.
Independent review then confirmed that the design's five-minute cap counts aggregate local command execution,
including preflight, compilation, proof and native runtime; editing and review waiting are excluded. The three
focused runner invocations' tool results (6.0s, 3.7s, and 6.2s plus a 0.76s completion poll) support a conservative
30-second debit, leaving at least 270 seconds. The earlier refusal is preserved; this was not a budget reset.

Immediately before launch, the host lock was absent, `docker ps` showed no running container and the diagnostic
source remained SHA-256 `f50543bbd69effa1cb29f0e45b0e3c2c59a1bd401aee05598eec794ef8d16d71`.
One native pass ran with:

```sh
SEMSTREAMS_KV_LIFECYCLE_DIAGNOSTIC=1 scripts/run-integration-tests.sh -timeout=180s -run '^TestIntegration_KVFilterLifecycleDiagnostic$' -v ./natsclient
```

Runner session `73050`; `/tmp/gh1421-native.log` was born 2026-09-29 23:45:25 UTC and last written
23:45:37 UTC. Exit status 0. The race-enabled runner reported `natsclient` 11.280s and the two-case parent test
9.82s. The exact 38,697-byte log is retained as `native-pass-log.zip`, ZIP SHA-256
`bf1b503d76e265511f970213d788a543fba258ce40695ac512144860d33f9c1a`, with extracted member SHA-256 in `native-pass-log.sha256`.
Host lock was absent and privileged `docker ps` showed no running containers after runner exit.
The full native command's exec call yielded a live session after 5.2s; the next concrete progress read showed the
parent test and substrate teardown complete, and the completion poll returned exit 0. Its log birth/last-write
markers span 23:45:25–23:45:37 UTC. Conservatively debit **60 seconds** for the entire native command, including
runner preflight, any compilation, both cases, teardown and the exit poll; this is deliberately above the package's
11.280s runtime and the 12-second log-write span. With the prior **30-second** focused-command debit, the aggregate
local execution is bounded by **90 seconds of the approved 300 seconds**. Editing and review waiting are excluded
under the independently reviewed budget reading; the initial wall-clock admission refusal remains recorded above.

| Observation | Transparent control | Retained default-deadline schedule |
|---|---|---|
| Native construction | 1.409167ms | 1.810583ms |
| Collection gate | Unmodified delivery | First key consumed; native forwarding goroutine 83 and watcher goroutine 27 witnessed blocked on sends at 29.680083ms while framework context remained live |
| Framework return | 16.638042ms, exact 1,024 seeded keys, nil error | 5.006616416s, nil keys, `errors.Is(err, context.DeadlineExceeded)` |
| Delegated Stop | Enter 16.5865ms, exit 16.609ms, `nats: invalid subscription` | Enter 5.005757083s, exit 5.006296666s, `nats: invalid subscription` |
| SDK deletion request origin seen | Native forwarding goroutine's deferred Stop; request and successful response | Context-completion unsubscribe; request entry only, no successful response callback |
| Client close | nil, CLOSED at 29.656ms | nil, CLOSED at 5.019572958s |
| Native keys/stack | Exact set returned; watcher callback join not exposed | Native Keys closed after drain at 5.020111541s; forwarding goroutine absent but watcher goroutine 27 still present in immediate 5.021334041s snapshot |
| Owned test work | Child exit and parent Wait complete | Facade forwarding joined 5.02027075s; child exit and parent Wait complete |

The framework Stop invocation oracle was checked once per case. The context-completion observer joined within the
shared terminal allowance. No trace buffer overflow or stack truncation occurred. The trace records no request from
framework-delegated Stop in these windows, and no asynchronous ordered-recovery deletion. Absence of a successful
response callback is not a request error outcome; SDK tracing does not expose every error return. The observed
context-completion deletion is asynchronous relative to the framework return and cannot establish Stop delay.
The public KeyLister offers no initial watcher marker or watcher join handle: native callback completion remains
**unproven**. In the deadline case its goroutine was still present in the immediate post-close snapshot.
Child containment, Keys closure and stack disappearance are distinct observations, not a callback join.

This 1,024-key in-memory fixture differs from the incident's 5,000-key file-backed workload. Construction was
prompt, collection reached the configured five-second deadline, and delegated Stop returned promptly in this
retained schedule. These observations neither explain the incident's extra interval nor establish a historical
cause or production repair. There was no second matrix or production edit.

The three raw logs are also retained byte-for-byte as members of `focused-logs.zip` (stored, no
normalization). Verify each extracted member with `focused-logs.sha256`; the ZIP itself is SHA-256
`e29767bf5a3f4dd7d297d2305b95b67389594c3acac4ff32f72fe0067b7c32df`. The standalone `.log` copies are ignored by Git and are not the durable artifact.
