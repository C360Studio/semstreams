# Exact execution logs

The original `.txt` diagnostic logs, historical owner-load log and incident excerpt referenced by the reports are
members of `execution-logs.zip`, under the same filenames. `execution-logs-manifest.json` records their original
checksums. Every member was read back and verified before raw copies were removed. No output bytes were normalized;
raw Go stack indentation and Docker output whitespace are preserved exactly.

The readable evidence reports distinguish measured assertions, historical/predecessor runs, final focused runs,
mutation failures and causal limits. The full original incident/issue snapshots remain in their separate bundle.
