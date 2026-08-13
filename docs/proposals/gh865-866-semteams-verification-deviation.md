# GitHub #865/#866 Semteams Verification Deviation

The accepted terminal-event design remains immutable at its recorded body
SHA-256. Its requested semteams behavioral verification cannot be performed
against the current semteams beta.159 tree because that adopter has not yet
completed its beta.160 migration.

The blocking incompatibilities precede this change: semteams imports the
removed `semstreams/pkg/ownership` package and its main wiring still calls the
retired six-argument AgentRun constructor. A full semteams build therefore
fails before it can exercise terminal callbacks.

This slice provides a durable representative adopter fixture and reproducible
harness at:

- `test/compat/semteams/agentrun_terminal_compat_test.go`;
- `scripts/verify-semteams-agentrun-compat.sh`.

That harness proves the retained product callback surface accepts production
success, failure, and cancellation envelopes. It is not evidence that the
unmigrated semteams application currently builds. Actual semteams wiring and
behavioral verification remain deferred to its beta.160 migration.
