# Actual-workload observation design review

Independent reviewer: **DESIGN REVIEW PASS** for SHA-256
`77a7dbc61d54a536ab5056f0ee9f0a01a03957746dde37c23f47a4db09cde81e`.

The persistent observer is proportionate: it measures the actual five-call workload, preserves the native channel and
synchronous Stop, and retains evidence before fatal assertions. Snapshot classifications distinguish observed markers
from actual runtime blockage. Callback joining, timing exclusions and one-pass limits are explicit.

Implementation verification must include exact activation: attempts 0–4 only for the CI predicate-forward loop; unarmed
and other workload paths remain delegated. Exercise early exit through the shared reporting scope using `require` with
testify's `assert.CollectT`, whose `FailNow` calls `runtime.Goexit`. A bounded, joined proof goroutine suffices; no
subprocess framework is needed. Independently release and join deliberately held callbacks even when assertions fail.

Source seam spot-checks agree with the design. Root mechanically extracted its eleven source pins into
`workload-seam-pins.md`; `task inventory:verify -- openspec/changes/owner-listing-expiry-recurrence/workload-seam-pins.md`
verified 11/11 with no moved, drifted, malformed or unparsed pins. The verifier does not accept the original design's
prose/table format; that earlier invocation performed no source comparison and supplies no source-verification result.

Root accepts this private test-observation slice under the user's instruction to continue. This approval supplies no
production-repair authorization, historical-cause conclusion, issue closure or new merge waiver.
