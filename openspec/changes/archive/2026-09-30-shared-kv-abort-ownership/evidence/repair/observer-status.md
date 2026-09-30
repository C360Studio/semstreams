# Observer implementation evidence

The final ordinary race run passed on the exact source identities in observer-preparation-manifest.json.
Its log records command, start time, exit status, 11.712 seconds wall time, and identical before/after source hashes.
The watcher-plus-constructor-error correction is included in that run. No native/tagged test executed.

The archive retains the final three graph-index source files and exact logs. Two attempts failed before test
execution because the sandbox denied warm Go cache access. A cold alternate-cache compile was interrupted without
test evidence; the final run used the normal warm cache with approved permissions. Earlier unsaved race/guard
outputs are reported by the worker but are not used as artifact-backed final proof.

The baseline refresh changes only dependency fingerprints in the existing reportCleanup/publish resolutions.
Their site identities, classifications and rationale remain unchanged; counts remain 273 legacy entries and
96 resolutions. The refresh and source still require independent implementation review and the actual guard gate.
