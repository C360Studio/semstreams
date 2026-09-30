# Shared KV abort diagnostic preparation

Source is frozen for independent review. No native diagnostic case has run.

## Source identity

- Ordinary private source: `ordinary-source.go.txt`, SHA-256 `68847d513fbfdd038cccd4070ade7162c0211341d6fe743e252f7d0f41e3494f`.
- Tagged private source: `tagged-source.go.txt`, SHA-256 `1571f26742d68739dca4425e10f16e339674afab1f4e3c2a6b0f7909b93a9b57`. Tagged source has not been compiled or executed; the one admitted canonical native command will supply that check.
- The first classifier fault used pre-proof-fix source SHA-256 `648fa0f1879a8c2b0f0b738cfd4f84752dceb9f3bfdf7ebf81be39be4faf5bfe` and mutant SHA-256 `43ffe97a518cd33c612482e2fbe64931345b5de52dda02b1b8c45efda299efae`. A `cp` backup was made before mutation; restoration matched `648fa0...` byte-for-byte. The later receive-only channel assertion fix produced current `68847d...`.

## Commands and outcomes, in order

1. `go test -race -count=1 -run '^TestAbortDiagnosticEvidenceAndContainment$' -v ./natsclient` — exit 1 in 3.217 s. On the intentional false classifier, expected `surviving_blocked_sender`, got `absent_from_snapshot` (`gh1421-abort-classifier-red.log`). This is a behavioral TDD RED. It is **not yet a complete current-source sensitivity experiment** because no passing baseline subgroup preceded that first mutation.
2. `go test -race -count=1 -run '^TestAbortDiagnostic' -v ./natsclient` — exit 1 in 7.597 s after byte-exact restoration. The classifier subgroup passed; both channel-identity checks failed only because the assertions compared bidirectional `chan string` with receive-only `<-chan string` (`gh1421-abort-focused-green.log`). This is a proof-type mistake, not a native result.
3. The assertions were corrected to compare receive-only channel identities. `go test -race -count=1 -run '^TestAbortDiagnostic' -v ./natsclient` — exit 0 in 8.567 s (`gh1421-abort-focused-green-v2.log`). Transparent/default-child, expired constructor gate, classifier ambiguity/truncation and bounded stream/Wait checks passed. This ordinary lane starts no real NATS fixture.

Tool-reported command execution totals 19.381 s. Conservative preparation debit is 30/300 s, leaving at least 270 s; native admission requires 210 s. No tagged runner/build/native wait or fixture cleanup is included yet. Editing and reviewer idle time are excluded by the accepted aggregate-execution design.

The named deterministic examples cover finite gate, witness, classifier and containment states; no generated property is needed for this test-only schedule. A current-source classifier sensitivity mutation with passing baseline, intentional failure, and restored passing proof remains required after source review. Native review/admission remains pending. This source does not change production behavior or establish the hosted failure cause.
