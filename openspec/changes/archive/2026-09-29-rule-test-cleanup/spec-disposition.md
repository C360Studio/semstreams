# Rule cleanup specification disposition

No normative capability delta is proposed for this test-only adoption.

`openspec/specs/test-cleanup-policy/spec.md`, requirement “Lexical ownership of lifecycle test fixtures”, already
requires provisional ownership before fallible setup, lexical finalization after transfer, controlled Start/Stop
ordering, fresh finite checked terminal authority, explicit phase fences and suppression of implicit retry after a
concrete terminal attempt. Its existing scenarios cover each planned behavior:

| Planned adoption | Existing scenario/requirement |
|---|---|
| Protect the four helper roots before setup can fail | Setup assertion before transfer |
| Finalize the 37 callers before testing-context cancellation/substrate callbacks | Caller assertion after transfer |
| Stop cron/hardening owners once; retain operation authority for following phase | Explicit terminal phase fence |
| Preserve contextless/native limitations and actual completion observations | Deadline supply versus completion |
| Remove exact repaired debt only, retaining all other entries/resolutions | Exact reviewed debt; Baseline freshness |

The existing requirement explicitly distinguishes tracked legacy fixtures from certified compliant fixtures. This
batch repairs 24 of those legacy entries; it does not change what the policy requires of another fixture. Duplicating
that requirement as “Rule fixture ownership” or asserting new production shutdown guarantees would create redundant
spec truth. `component-lifecycle`, `runtime-context-ownership` and production rule/graph specifications remain unchanged.

Materialize this disposition alongside the design; do not create an empty or synthetic `specs/.../spec.md` delta.
Archive the change with its source/proof/baseline reconciliation and record that current capability specs required no
normative update. If independent design review identifies an actual contract gap, resolve that concrete gap before
implementation rather than pretending this disposition already covers it.
