---
name: e2e-doctor
description: Diagnose Docker disk pressure, testcontainer cleanup, and port conflicts before SemStreams integration or E2E runs. Use when infrastructure evidence is needed to explain a test failure.
argument-hint: "[optional tier name]"
---

# Diagnose E2E infrastructure

Read the [testing policy](../../../docs/contributing/01-testing.md) and
[shared protocol](../../../.agents/protocol.md). The canonical integration runner owns Docker preflight,
the shared host lock and Reaper policy. Use that runner for both full and focused integration tests.
Serialize heavy runs on the shared host; this helper does not bypass the runner's lock.

A mapped-port timeout can reflect infrastructure pressure, but the message alone does not prove that cause.
Preserve the failing command, timestamps and logs before changing the environment. Compare actual Docker
state with the failure; do not classify every timeout as infrastructure or every isolated rerun as a fix.

## Inspect before changing resources

```bash
docker system df
docker ps -a --format '{{.ID}}\t{{.Names}}\t{{.Status}}\t{{.Image}}'
task e2e:check-ports
```

For the selected tier, inspect its Compose project, labels, port mappings and any active session/run owner.
An old NATS or Ryuk container may still belong to another session. Age and name patterns are not ownership
evidence. An occupied port is a reason to identify its owner, not permission to stop it.

## Reclaim only the identified run

Prefer the selected run's existing teardown command with its exact Compose project and profiles. A targeted
`docker rm -f <confirmed-abandoned-container-id>` is appropriate only after ownership and abandonment are
established and cleanup is within the session's authorization. Preserve another session's resources; report
unresolved ownership rather than treating those resources as leaks.

Host-wide builder/image pruning is not a routine preflight step. On a shared host, caches and large images
can belong to sister projects or active builds. Do not use `docker image prune -a` as a cleanup shortcut;
removing another project's image requires its owner's explicit authorization.

## Verify cleanup and the original failure

Confirm that the selected run's containers, volumes and listeners were released. The host's total container
count need not be zero. Retain cleanup errors and unresolved resources with the run evidence.

Re-run the appropriate check only when the measured change justifies it. A clean rerun does not establish
that a known flake is fixed. Follow the protocol's required-job flake rule, and report what the evidence proves
about the original failure and what remains unknown.
