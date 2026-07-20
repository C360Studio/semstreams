# Tasks: read-path-readiness-retry-contract

## 1. Fix + document

- [x] 1.1 `pkg/fusion/engine_lens.go`: classify internal-read errors in `Fuse`.
      When `Resolve` / `Entities` (and the neighbor-expansion path) return the
      classified `ErrorCodeIndexNotReady` transient, return the empty-honest
      envelope (`Response{Index: <current status>, Ready=false, ...}`) — the same
      degrade as the top `!Ready` gate — instead of `return Response{}, err`.
      Non-transient errors still propagate. Keep the top-gate behavior unchanged.
- [x] 1.2 Test: reproduce the semsource shape deterministically — top `Ready`
      gate passes, then the resolver returns a classified `ErrorCodeIndexNotReady`
      → assert `Fuse` returns an empty `Ready=false` envelope, `nil` error (not a
      propagated error); and a non-transient resolver error still propagates.
      No sleeps — inject the transient via a fake graph/resolver.
- [x] 1.3 Spec delta (this change) documents the read-path retry contract +
      the fusion degrade-consistency requirement (DONE — validates strict).

## 2. Gates + close-out

- [x] 2.1 `-race` unit (`./pkg/fusion/...` + any fusion integration), `go build
      ./...`, `go vet -tags=integration ./...`, `task lint`; semstreams-reviewer
      pass (touches the fusion contract).
- [x] 2.2 Close #592 with the decision comment: CLOSE (retry is the contract),
      the Path-A/Path-B reframe, the reopen trigger (continuous-write deployment
      also serving exact point queries — does not exist today), and the red
      herrings (`lifecycle/manager_query.go`, `rule/entity_watcher.go`, and the
      temporal/spatial/embedding "watcher unavailable" sites reuse
      `ErrorCodeIndexNotReady` for responder-up, NOT catch-up — do not widen a
      tolerance into them).
- [x] 2.3 Merge + archive this change; then the joint tag covers #591 + this fix.
