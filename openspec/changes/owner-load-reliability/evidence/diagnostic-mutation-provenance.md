# Stop delegation mutation provenance

Historical first implementation mutation record. The reviewer-corrected source and its mutation are recorded in
`diagnostic-review-correction.md`.

The final-source baseline file was copied to
`/private/tmp/gh1421-kv-diagnostic-final-baseline.go` before mutation. That backup and the restored in-tree source
both have SHA-256 `da21c07c5b404d62f3a00a3899604fbe1c4e4d1f7926e49e2153eb3bc44484c9`.
The backup is scratch state; the exact one-line mutation is retained in `diagnostic-final-mutant.diff`.

The mutation command asserted one exact source match, then replaced only the facade's delegation:

```python
from pathlib import Path

p = Path("natsclient/kv_filter_lifecycle_diagnostic_integration_test.go")
s = p.read_text()
old = "\terr := f.observer.Stop()\n"
assert s.count(old) == 1, s.count(old)
p.write_text(s.replace(old, "\terr := error(nil) // MUTANT: bypass independent Stop observer\n"))
```

The shell installed `trap 'cp /private/tmp/gh1421-kv-diagnostic-final-baseline.go
natsclient/kv_filter_lifecycle_diagnostic_integration_test.go' EXIT` before replacement. It ran the exact
canonical command in `diagnostic-report.md` once with the mutant and emitted `mutation_status=1`, then the trap
restored the file. The SHA-256 of the restored source and backup matched the baseline hash above. The same
canonical command passed again after restoration (`diagnostic-final-restored.txt`).

The independent invocation observer is at diagnostic source lines 251–266 and its count method at 261–265.
The facade Stop is at line 306; the required count assertion is at lines 458–471. The diff leaves the observer
and assertion unchanged. The mutant compiled and reached the count assertion in the cancel child at 29 ms;
`diagnostic-final-mutant-run.txt` records `delegated Stop invocation count=0 want=1` and parent exit 1.
The final restored run passed all three cases with exit 0.

Evidence identities:

- `diagnostic-final-baseline.txt`: `a52418a78f36a898cb29e6485c572f192c03e76c31a1fbb2b7eba33b4bef85de`
- `diagnostic-final-mutant.diff`: `59603b237675bbf4125373223a52a5c4029c7e90a3dc392f7557c8dbbf2cc743`
- `diagnostic-final-mutant-run.txt`: `c1f371c5fc09ade93839c288486a614cb6e274914c4c27a461c6b006745f9b0a`
- `diagnostic-final-restored.txt`: `4e475460636e08e379d6febad5a93a188ba20940b6ac43b47521f0b0b79aa24c`
