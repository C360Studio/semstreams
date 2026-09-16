# R8 shared publisher review — 2026-09-15

## Scope and verdict

Independent SemStreams reviewer: **APPROVE — bounded R8 publisher slice only; no findings**.
Reviewed parent HEAD: `c347eff487f50b93bc338d764f43ef5b5ea5e133` plus the three uncommitted files below.
Existing R6/R7 work is preserved. No action, payload, admission, lifecycle, configuration or schema changes.

The architect-reviewed slice implements only the covered-output classification/PubAck portion of
`specs/rule-agent-publishing/spec.md:38`. The existing private classifier now calls canonical
`flowgraph.SubjectCovers(declaredFilter, concreteSubject)` for each resolved JetStream output subject.
It keeps searching past core ports. The existing synchronous publication, counters and uncovered core path remain.

The owner approved bounded current-contract/publisher-evidence reading for this slice:
https://github.com/C360Studio/semstreams/issues/1146#issuecomment-5676789878.
This exception preserves historical records and does not waive other gates.

## Exact reviewed sources

```text
7be4cec158ac7e1228901c81173007bacccb5a3d9b39e65413de3bdeed263410  processor/rule/publisher.go
ba0603287b6fdaef485d27053f64332ec7689bf6be73e24b086bb04525220934  processor/rule/publisher_test.go
84e38bb56050c25e33c5cad5ecdc4b93dc7546c96035ee8269bf1056743e4856  processor/rule/publisher_integration_test.go
```

## Conformance and proof

| Accepted obligation | Implementation or evidence |
| --- | --- |
| Reuse canonical directional coverage, no new matcher/API | `publisher.go:74` |
| Resolved JetStream facts; every declared subject; core cannot mask JS | `publisher.go:69`, `publisher.go:73` |
| Existing synchronous PubAck and success-only counters | unchanged `publisher.go:47`, `:53`, `:58`; `natsclient/client.go:1005` |
| Six shipped configurations, both port names, declaration-only surfaces | `publisher_test.go:84` |
| Exact bytes/subject; unavailable PubAck is an error, not core fallback | `publisher_integration_test.go:45`, `:56`, `:63` |
| Core-only output still works without a stream | `publisher_integration_test.go:69` |
| Graph and optional notification contracts preserved | `publisher_test.go:143`; unchanged existing caller controls |

Independent structural references confirm the three production callers: action publication, prepared graph events,
and optional rule notifications. No extra owner, knob, payload codec or acknowledgement wrapper was introduced.

New unit proof comprises 20 classifier rows, six shipped configuration rows and one graph caller case.
Focused GREEN also ran 15 unchanged controls (42 leaf cases across ten top-level tests).
Native proof uses the actual actionPublisher and one isolated canonical NATS container per run.

## Commands and outcomes

All commands use `GOCACHE=/private/tmp/semstreams-r7-test-cache GOPROXY=off GOSUMDB=off GOFLAGS=-mod=readonly`.

- RED classifier/configuration race test: seven wildcard/core-order/multiple-subject rows and all six configuration
  rows failed at the intended classification assertion. Exact, core and negative controls passed.
  Exit 1; package 0.594s; wall 5.63s.
- Native RED: exact stored bytes/subject succeeded, but after observed stream deletion the same covered publication
  returned nil instead of `jetstream.ErrNoStreamResponse`. This proves the accidental core fallback.
  Exit 1; test 2.63s; package 3.257s; wall 7.07s.
- Graph caller RED: zero stream publications instead of one. Exit 1; package 0.628s; wall 3.45s.
- Focused GREEN: rule 1.569s; flowgraph 1.883s; wall 4.76s; no skips.
- Native GREEN: actual PubAck, exact stored bytes/subject, missing-PubAck error with unchanged success counters,
  and core-only delivery without a stream. Test 1.00s; package 2.555s; wall 5.94s.
- `go test -race ./processor/rule ./component/flowgraph -count=1`: PASS, 5.416s / 1.594s; wall 7.81s.
- `go vet -tags=integration ./processor/rule ./component/flowgraph`: PASS; wall 4.45s.
- `go tool revive -config revive.toml -formatter friendly ./processor/rule/... ./component/flowgraph/...`:
  PASS, no warnings; wall 1.20s.
- `git diff --check` and focused formatting: PASS.

Native command:
`scripts/run-integration-tests.sh ./processor/rule -run '^TestIntegration_ActionPublisherUsesDeclaredTransport$' -v`.
The canonical runner owns race/failfast/fresh-count flags and the host lock. No sleeps or guessed ports.
A preceding sandbox socket denial is recorded separately and is not behavioral RED.

Exact commands, regexes, log hashes and case counts are in the checksummed developer evidence:
`handoff.md`, SHA-256 `43d333003a2b757cf4fd7537b273cc5704923e6cf0dbc8e2fbd60eefceeb9b2f`.
Durable backup root:
`/Users/coby/Code/c360/semstreams-wt/gh1146-rescue-checkpoint.y3bWGc/r8-publisher.CIFDXo/`.
Its `evidence/` directory holds the handoff and all RED/GREEN logs; its root holds verified copies of the three files.

All owned test sessions completed. Exact container inspection found no owned NATS/Reaper containers remaining;
the integration-lock owner was absent at closeout. No historical source was restored or discarded.

## Remaining gates

R8 stays unchecked. Action-specific uncovered/malformed/missing-publisher refusals, observed AGENT admission,
refusal/backpressure/allocation proof, four producer-to-loop paths, registered non-Graphable TaskMessage and
malformed/unregistered envelope proof, and loop-authority admission/typed creation remain required.

Generic `executePublish` wire/correlation is unchanged. No #1311 runtime work or restack is included.
No full combined push gate, E2E, archive, merge or issue closure is claimed. The earlier c347eff4-only push exception
does not authorize a new implementation push. This local reviewed slice is not the required complete #1159
prerequisite checkpoint.
