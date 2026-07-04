# Tasks — Graph Retention Guardrail

## 1. Remove the query-client retention landmine

- [x] 1.1 `graph/query/client.go` `DefaultConfig()`: set `ENTITY_STATES`,
      `SPATIAL_INDEX`, `INCOMING_INDEX` TTL to `0`
- [x] 1.2 Unit test: `DefaultConfig()` yields TTL `0` on every shared graph bucket

## 2. Boot-time guardrail

- [x] 2.1 `natsclient`: `BucketRetention(ctx, bucket) (maxAge time.Duration, maxBytes int64, err error)`
      reading the backing stream config (mirrors `BucketLastSeq`)
- [x] 2.2 Pure check `errIfLifecycleRetention(name, maxAge, maxBytes) error` + unit test
      (0/0 → ok; non-zero TTL → error; binding MaxBytes → error)
- [x] 2.3 `graph-ingest` Start: after ensuring `ENTITY_STATES`, assert no lifecycle
      retention; return a fatal error (fail-closed) if violated
- [x] 2.4 Integration test: a bucket created with a TTL trips the assertion; a clean
      bucket passes

## 3. Spec + close

- [x] 3.1 `openspec validate` the change
- [x] 3.2 Gates green (`go test -race`, `task lint`), then archive → promotes
      `graph-retention` into `openspec/specs/`
