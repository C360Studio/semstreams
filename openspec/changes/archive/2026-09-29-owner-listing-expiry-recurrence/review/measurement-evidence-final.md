# Measurement evidence review: final

Independent reviewer: **APPROVE source and observed results; PASS final evidence accounting**.

Verified identities:

- Diagnostic source SHA-256 `f50543bbd69effa1cb29f0e45b0e3c2c59a1bd401aee05598eec794ef8d16d71`.
- Native log SHA-256 `525330cfbdb73c19f7cc67e0adde2977ce0c8ae68d34a3cc05d7f73080b3f0c1`.
- Final report SHA-256 `909a9719dbe5482563d77ac357250da607f305d020666a774686df0e78f3e1c6`.
- Retained ZIP contents and member manifests match their original logs exactly.

The control returned all 1,024 expected keys; construction took 1.41ms and delegated Stop 22.5 microseconds. The
controlled collection blockage returned nil keys with a typed deadline error after approximately five seconds;
construction took 1.81ms and delegated Stop 0.54ms. Both delegated Stops returned `nats: invalid subscription`.
Affirmative request stacks attribute control deletion to native forwarding cleanup and the deadline case's deletion
to context-completion unsubscribe. No deadline DELETE response was recorded; this does not establish request timeout.

Checked test-owned cleanup and child containment completed. Native watcher callback completion remains unproven;
its goroutine was present in the immediate snapshot. The false-origin mutation detects the claimed attribution error,
not complete lifecycle coverage.

The remaining MEDIUM accounting correction was resolved in the final report: full-command completion observations
support a conservative 60-second native debit and aggregate at most 90 of the allowed 300 seconds. No remaining
packaging finding. No additional native execution was needed.

The ten-second CI return remains unexplained. Useful next evidence is construction/collection/Stop attribution from
the actual repeated predicate-forward workload, especially its failing attempt. This reviewed pass authorizes neither
a production repair nor closing #1421.
