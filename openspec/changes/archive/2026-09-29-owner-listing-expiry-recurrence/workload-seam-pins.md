# Actual-workload seam pins

base: f5603895b1f225735d6138000b276c505adfaa08

Mechanical extraction of the architect's eleven source pins from `workload-observation-design.md` for the
inventory verifier. This is not a new census or a replacement for the accepted inventory. The original
design table is not the verifier's bullet grammar; its ordinary prose bullets are intentionally not copied here.

## Source pins

- `processor/graph-index/owner_filter_load_integration_test.go:56` — `name: "ci", entities: 5_000, nameContext: 5_000, spread: 20,`
- `processor/graph-index/owner_filter_load_integration_test.go:195` — `raw, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: bucketName, Storage: jetstream.FileStorage})`
- `processor/graph-index/owner_filter_load_integration_test.go:197` — `stores[bucketName] = nc.NewKVStore(raw)`
- `processor/graph-index/owner_filter_load_integration_test.go:474` — `keys, err := store.KeysByFilter(ctx, filter)`
- `processor/graph-index/owner_filter_load_integration_test.go:475` — `duration := time.Since(started)`
- `processor/graph-index/owner_filter_load_integration_test.go:484` — `require.NoError(t, err, label)`
- `natsclient/kv.go:541` — `lister, err := kv.bucket.ListKeysFiltered(ctx, pattern)`
- `natsclient/kv.go:550` — `keys, err := collectFilteredKeys(ctx, lister)`
- `natsclient/kv.go:583` — `defer func() { _ = lister.Stop() }()`
- `natsclient/kv.go:590` — `case key, ok := <-lister.Keys():`
- `natsclient/kv.go:589` — `return nil, ctx.Err()`
