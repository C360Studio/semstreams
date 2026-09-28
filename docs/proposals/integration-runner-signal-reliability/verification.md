# Final local verification

Date: 2026-09-28. Worktree: `codex/gh1397-runner-synchronization`, claim HEAD `234200f3` plus the reviewed diff.
Test source MD5: `87186d432ecdb43e0462b87b9974aea7`.
Production runner MD5: `ef0dde707bec1ab6a39d5ad7293896fe`, with no source diff.

```bash
GOCACHE=/private/tmp/semstreams-gh1397-gocache task check:push
```

**PASS, exit 0.** The final run completed build, lint, integration-tagged vet, live-LLM-tagged vet, schema generation
and drift checks, contract checks, repository-wide unit race tests and the canonical integration runner.

- Unit race: 160 successful package records, zero failures. `test/testinfra`: 29.919s.
- Integration race: 160 successful package records, zero failures. `test/testinfra`: 27.780s.
- The integration runner emitted `tests complete`; its host lock was absent after completion.
- No schema or production runner diff remained.
- Log: `/private/tmp/semstreams-gh1397-check-push-3.log`.

These package counts exclude packages with no test files. The integration invocation is additive and includes ordinary
and integration-tagged tests; it is not a Docker E2E-tier run. This test-only change introduces no breaking runtime
contract. Hosted results for the committed candidate belong in PR #1408 and are not claimed by this local record.

Earlier attempts are explicitly superseded: attempt 1 failed lint before long tests; attempt 2 was interrupted for the
probe-containment correction after unit race passed. Their results remain in the review and convergence records.
The focused behavioral checks, mutation outcomes, restoration checksums and accepted deferral remain in the
[implementation evidence](evidence/implementation-evidence.md).
