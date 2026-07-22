# Tasks — offloaded-title-embedding

## 1. Thread identity text onto the offloaded pending record (D2)

- [ ] 1.1 Add a `sourceText string` parameter to `Storage.SavePendingWithStorageRef` (`graph/embedding/storage.go:191`) and set `Record.SourceText` from it (record already carries the field; the offloaded lane leaves it empty today).
- [ ] 1.2 In `queueEmbeddingWithStorageRef` (`processor/graph-embedding/component.go:1488`), compute the identity text with the existing `extractTextForEmbedding(state)` and pass it into `SavePendingWithStorageRef`. For an offloaded entity this returns exactly the inline text-suffix triples (the body is not inline).
- [ ] 1.3 Update all other `SavePendingWithStorageRef` callers/tests for the new param.

## 2. Concatenate identity-first at hop 2 (D1, D3, D4)

- [ ] 2.1 Re-branch `getSourceText` (`graph/embedding/worker.go:663`) on `StorageRef` primary: if `StorageRef != nil` fetch the body, then prepend `SourceText` + separator when `SourceText != ""` (identity-first); else (inline lane) use `SourceText` unchanged. This replaces today's mutually-exclusive `if SourceText / else if StorageRef` so an offloaded record with identity text no longer drops the body.
- [ ] 2.2 Define the identity↔body separator as a single frozen constant (`"\n\n"`); it is part of the embedded bytes and the dedup key.
- [ ] 2.3 Confirm the existing `truncateAtWord(combined, maxSourceTextLen)` applies to the combined text (identity survives, body trims) and that `fetchTextFromStorage`'s stream clamp + this truncate do not double-count `text_truncated_total` (#602).
- [ ] 2.4 Confirm the hop-2 dedup key (`DedupKey(embedderIdentity, sourceText)`, `worker.go:453`) now derives over the combined text with no code change — it already keys over `getSourceText`'s output.

## 3. Observability (D5)

- [ ] 3.1 Add a metric recording whether an offloaded entity embedded inline identity text alongside its body (paired included/absent counter), following the `graph-embedding` metrics precedent (`metrics.go`, e.g. `text_truncated_total`). Increment at the offloaded-lane text-production site.

## 4. Tests

- [ ] 4.1 Unit-test `getSourceText`: offloaded + identity → identity-first combined; offloaded + no identity → body-only; inline lane (no StorageRef) → `SourceText` unchanged.
- [ ] 4.2 Unit-test the cap: combined text over cap → identity retained, body trimmed from the end; truncation counted once, not double.
- [ ] 4.3 Unit-test the dedup key changes when identity OR body changes, and matches across lanes for byte-identical combined content (#627 stays moot).
- [ ] 4.4 Integration-test (testcontainers, real NATS/ObjectStore) an offloaded entity: a text-suffix predicate present on the entity ends up in the embedded/queryable text; a query naming the title/identity retrieves the entity.
- [ ] 4.5 Test the observability metric increments on identity-included and identity-absent offloaded entities.

## 5. Coordination

- [ ] 5.1 Draft a `semstreams-asks` / `docs/operations` note for semsource: offloaded entities now embed their identity triples (`.signature`/`.comment`/title) alongside the body, so their vectors change — a one-time re-embed and a recall shift are expected; `text_suffixes` now takes effect on offloaded entities.

## 6. Gate before push (CI-green-before-merge; owner policy)

- [ ] 6.1 `task lint` (revive clean), `go test -race ./...`, tagged vet (`integration`, `live_llm`), contract tests, `gofmt -l`.
- [ ] 6.2 `task schema:generate` + `git status schemas/ specs/` — no drift (the `sourceText` param is internal; no operator-config surface change expected — confirm).
- [ ] 6.3 Framework-package sweep: `go test -race -tags=integration ./...` on `graph/embedding/`, `processor/graph-embedding/` and consumers.
- [ ] 6.4 Run the fusion/embedding e2e tier (semantic — exercises offloaded evidence bodies + search) green before merge.
- [ ] 6.5 After the PR is up, **wait for the main CI workflow to go green** (`gh pr checks <pr>` all pass + `mergeStateStatus == CLEAN`) BEFORE merging — do not merge on local gate alone.
