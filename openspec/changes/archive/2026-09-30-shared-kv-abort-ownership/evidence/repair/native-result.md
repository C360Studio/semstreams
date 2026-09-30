# Focused native repair validation

One canonical command passed on the independently reviewed source, without a rerun:

```bash
scripts/run-integration-tests.sh -timeout=90s -run '^TestIntegration_KVStoreFilteredNativeDeliveryClosure$' -v ./natsclient
```

UTC execution: 2026-09-30 04:41:02.813 through 04:41:15.034. Exit 0; total wall time 12.220 seconds.
The test took 6.54 seconds and package output reported 7.976 seconds. Native log SHA-256:
`5d77b38e9f4cc580bf766d6f4b3b34cef3e0e668ad242f9ffbf8980caae63aee`.
The exact log is retained as verification/raw-logs.zip/gh1421-repair-native.log.
The recorded eight source/baseline hashes were unchanged across execution; exact command metadata is native.json.

The fixture reported 5,000 file-backed keys, server 2.14.4, SDK v1.52.0 and image/image ID at the normative
f2123f533c2b0cada0a5c5ec434fb2b8cfe1cf220215ef9d7517e1372917ad66 digest. The explicit capacity witness was
len=256, cap=256 while the actual child context remained live. The reviewed test then required that child to expire,
nil keys with typed deadline error, and the exact native Updates channel closed and drained at public return.
No observer or test drain supplied that completion.

Fixture 12e444488806 was stopped and terminated in the log. After command completion Docker had no containers,
and the integration lock was absent. The runner's default image preflight is separate from this pinned fixture.

This validates the exercised terminal-delivery boundary after repair. It does not establish native goroutine joins,
consumer deletion, historical CI causation or elimination of every intermittent listing failure. The new test is a
permanent default integration regression; successful execution here does not replace required full pre-push gates.
