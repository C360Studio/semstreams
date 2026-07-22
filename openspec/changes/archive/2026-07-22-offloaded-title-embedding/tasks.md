# Tasks — offloaded-title-embedding

## 1. Thread identity text onto the offloaded pending record (D2)

- [x] 1.1 Add a `sourceText string` parameter to `Storage.SavePendingWithStorageRef` (`graph/embedding/storage.go:191`) and set `Record.SourceText` from it (record already carries the field; the offloaded lane leaves it empty today).
- [x] 1.2 In `queueEmbeddingWithStorageRef` (`processor/graph-embedding/component.go:1488`), compute the identity text with the existing `extractTextForEmbedding(state)` and pass it into `SavePendingWithStorageRef`. For an offloaded entity this returns exactly the inline text-suffix triples (the body is not inline).
- [x] 1.3 Update all other `SavePendingWithStorageRef` callers/tests for the new param.

## 2. Concatenate identity-first at hop 2 (D1, D3, D4)

- [x] 2.1 Re-branch `getSourceText` (`graph/embedding/worker.go:663`) on `StorageRef` primary: if `StorageRef != nil` fetch the body, then prepend `SourceText` + separator when `SourceText != ""` (identity-first); else (inline lane) use `SourceText` unchanged. This replaces today's mutually-exclusive `if SourceText / else if StorageRef` so an offloaded record with identity text no longer drops the body.
- [x] 2.2 Define the identity↔body separator as a single frozen constant (`"\n\n"`); it is part of the embedded bytes and the dedup key.
- [x] 2.3 Confirm the existing `truncateAtWord(combined, maxSourceTextLen)` applies to the combined text (identity survives, body trims) and that `fetchTextFromStorage`'s stream clamp + this truncate do not double-count `text_truncated_total` (#602). (Combined re-truncate is uncounted in `getSourceText`'s offloaded branch; `fetchTextFromStorage` remains the single offloaded-lane truncation-count site.)
- [x] 2.4 Confirm the hop-2 dedup key (`DedupKey(embedderIdentity, sourceText)`, `worker.go:453`) now derives over the combined text with no code change — it already keys over `getSourceText`'s output.

## 3. Observability (D5)

- [x] 3.1 Add a metric recording whether an offloaded entity embedded inline identity text alongside its body (paired included/absent counter), following the `graph-embedding` metrics precedent (`metrics.go`, e.g. `text_truncated_total`). Increment at the offloaded-lane text-production site.

## 4. Tests

- [x] 4.1 Unit-test `getSourceText`: offloaded + identity → identity-first combined; offloaded + no identity → body-only; inline lane (no StorageRef) → `SourceText` unchanged.
- [x] 4.2 Unit-test the cap: combined text over cap → identity retained, body trimmed from the end; truncation counted once, not double.
- [x] 4.3 Unit-test the dedup key changes when identity OR body changes, and matches across lanes for byte-identical combined content (#627 stays moot).
- [x] 4.4 Integration-test (testcontainers, real NATS/ObjectStore) an offloaded entity: a text-suffix predicate present on the entity ends up in the embedded/queryable text; a query naming the title/identity retrieves the entity.
- [x] 4.5 Test the observability metric increments on identity-included and identity-absent offloaded entities.

## 5. Coordination

- [x] 5.1 Adopter heads-up written: `docs/operations/offloaded-identity-embedding-change.md` (semsource: `text_suffixes` now takes effect on offloaded entities; one-time re-embed of identity-carrying offloaded entities; recall shift; observability counters; embed-both deferred).

## 6. Gate before push (CI-green-before-merge; owner policy)

- [x] 6.1 `revive` clean, `go test -race ./...` (133 ok / 0 FAIL), tagged vet `integration`+`live_llm` (exit 0), contract ok, `gofmt -l` clean.
- [x] 6.2 `task schema:generate` + `git status schemas/ specs/` — no drift (the `sourceText` param is internal; metric names aren't operator config; `Config` untouched).
- [x] 6.3 Integration on the touched framework packages green under `-tags=integration` (`graph/embedding`, `processor/graph-embedding` incl. the offloaded-identity end-to-end test on real NATS/ObjectStore).
- [x] 6.4 `task e2e:semantic` GREEN — scenario completed, `validation_errors:0`, `embedding_failed_total:0`, `data_loss:0`, `known_answer 7/7`, `search 8/8`. (`community_ground_truth 0/3` and `nl_*_intent 0/N` are non-gating LLM-driven soft probes; change is inline-lane byte-invariant.)
- [ ] 6.5 After the PR is up, **wait for the main CI workflow to go green** (`gh pr checks <pr>` all pass + `mergeStateStatus == CLEAN`) BEFORE merging — do not merge on local gate alone.
