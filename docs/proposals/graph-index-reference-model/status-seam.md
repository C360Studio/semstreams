# Graph-index status-seam correction evidence

base: 18bc72037d383a403d631026472ef88ac32c2d33

Narrow architect inspection during implementation. No tests, writes, Docker or protected smoke inspection.
The planned successful in-memory status reader is unsupported; use the already inventoried production projection
seam and narrow the claim. No runtime/helper or SDK-private-layout change is proposed.

## Broker-owned target acquisition

- `natsclient/kv.go:115` — `bucketStatus, ok := status.(*jetstream.KeyValueBucketStatus)`
- `natsclient/kv.go:119` — `info := bucketStatus.StreamInfo()`
- `natsclient/kv.go:123` — `return info.State.LastSeq, nil`
- `processor/graph-index/watermark.go:77` — `target, err := natsclient.BucketLastSeq(ctx, statusBucket)`
- `processor/graph-index/watermark.go:104` — `status = applyKnownIncompleteOverrides(status, target,`
- `processor/graph-index/watermark.go:106` — `return c.latchBootstrap(status)`

The pinned SDK's jetstream/kv.go lines 808-810 declare private info/bucket fields. StreamInfo returns that private
pointer; successful Status construction follows kv.stream.Info(ctx) at lines 1555-1561. Zero value cannot supply head.
The existing TestBucketLastSeqAcceptsStatusOnlyReader proves error propagation only, not a successful fake.
No unsafe/private-layout fixture is admitted.

## Existing unit projection seam

- `processor/graph-index/watermark_test.go:253` — `base := graph.ComputeIndexStatus(graph.IndexStatusInputs{Indexed: tc.indexed, Target: tc.target})`
- `processor/graph-index/watermark_test.go:254` — `status := c.latchBootstrap(applyKnownIncompleteOverrides(base, tc.target, tc.enumComplete, failed))`

ComputeIndexStatus consumes the subject's real watermark and the fixture's committed head. The existing override
and latch functions consume actual subject enumeration/failure state; EvaluateReadinessGate observes the result.
These are subject observations, never the expected-result oracle. Independent history-derived expectations stay
separate. This unit seam proves the projections and latch; it does not execute enclosing computeIndexStatus,
server target acquisition, separate SDK handles or status publication.

Cold hydration checks incomplete bootstrap/canonical-gate refusal before completion and after one owner. Exact
handler results are compared after the production latch admits the completed build. A cold handler refusal caused
by unavailable status would observe the wrong reason and must not be counted as bootstrap proof.

After bootstrap, real query handlers check actual failedCount before the latch (query.go lines 213 and 217).
Injected persistent failures therefore require classified index_not_ready from those handlers and a degraded
production projection. Production repair must restore healthy projection and exact handler results.

The readiness mutation becomes premature latch completion, such as accepting enumeration completion alone.
The two-pending-owner witness must detect bootstrap/canonical-gate admission before initial work completes.
Required-write-failure mutation and real handler-refusal evidence remain separate.

## Remaining broker evidence

Existing replacement_reconcile_integration_test.go lines 571 and 586 observe public watermark status and require
published ready status plus both revisions reaching the boundary. Coordinated real-NATS confirmation remains
required. No new container or production seam is needed.

## Inspection ledger

`git grep -n 'BucketLastSeq' -- natsclient processor/graph-index ':!processor/graph-index/predicate_layout_smoke_integration_test.go'`
located the helper, error-only unit test and existing integrations.

`git grep -n 'KeyValueBucketStatus' -- natsclient processor/graph-index ':!processor/graph-index/predicate_layout_smoke_integration_test.go'`
returned four matches, concrete assertions/error text in natsclient/kv.go; no constructor fixture.

Read kv.go lines 95-129, complete kv_capability_test.go, the relevant SDK declaration/accessor/constructor ranges,
watermark_test.go lines 235-271 and production readiness ranges. No runtime or contract change is recommended.
