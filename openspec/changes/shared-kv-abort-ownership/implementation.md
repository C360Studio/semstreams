# Repair implementation and verification map

The reviewed repair replaces only full KVStore filtered listing's native KeyLister forwarding layer with direct
WatchFiltered collection and terminal delivery ownership. Minimal-reader and unfiltered paths retain their existing
contracts. Cancellation is checked before selection for prompt refusal; the regression's terminal oracle does not
depend on that branch, as the supplemental mutation demonstrates.

| Accepted obligation | Current implementation or evidence |
| --- | --- |
| Existing filter semantics, full bucket capability | natsclient/kv.go:550; unchanged IgnoreDeletes and MetaOnly options |
| No-watcher context and exact no-match precedence | natsclient/kv.go:551 |
| Prompt cancellation and complete snapshot requirement | natsclient/kv.go:571; marker/closure handling at 579 |
| Once-only synchronous Stop and separate finite drain | natsclient/kv.go:599; bounded terminal context at 600 |
| Typed primary plus visible cleanup failures | natsclient/kv.go:614 |
| Minimal-reader helper retained | natsclient/kv.go:624 |
| Public terminal-ownership proof independent of scheduling | final-source snapshots and both final mutant logs |
| Virtual-time five-second failure histories | natsclient/kv_watcher_ownership_test.go, final focused log |
| Passive observer and reached test adapters | observer-preparation.zip; graph-ingest source/fail/fix evidence |
| Existing cleanup classifications only | cleanup fingerprint record; all 273 entries unchanged, 96 resolutions |
| Actual native delivery closure after five-second expiry | evidence/repair/verification/raw-logs.zip and native-result.md |

The active task ledger owns remaining gates. The ordinary RED, preparation corrections, survived mutation, final
mutants, exact restorations, source reviews and native result are retained under evidence/repair and review.
Earlier source packets are historical checkpoints; the final-source packet identifies final Go bytes.

The historical CI stall and fifteen-second client-drain failure remain unattributed. #1421 stays open and #1435 stays
draft while remaining verification proceeds. The #1432 waiver does not transfer. No new issue is filed by this unit.
