# Exact source diffs

All `.diff` and `.patch` artifacts referenced by the evidence and reviews are retained byte-for-byte as members of
`source-diffs.zip`, under their original filenames. `source-diffs-manifest.json` records every original checksum.
The archive was read back and every member checksum verified before the raw copies were removed.

Unified diffs contain significant leading context spaces before Go tabs and blank context lines. Keeping these
raw bytes in an archive preserves exact mutation/reconstruction evidence without treating patch syntax as new
source whitespace. Extract the named member to inspect or apply it; no source or test behavior changed in packaging.
