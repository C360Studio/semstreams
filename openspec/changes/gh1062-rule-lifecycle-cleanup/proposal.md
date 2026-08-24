# Change: Distinguish controlled Rule teardown from accepted-parent abort

Status: compact lifecycle-lane correction implemented; replacement focused verification passed and independent
implementation review remains pending.

Accepted evidence:

- Compact inventory: `docs/proposals/gh1062-compact-lifecycle-lane-inventory.md`, independently reviewed
  `INVENTORY PASS` at SHA-256 `f33782076205b58504124ddfe2fb391cc70073a92f6c2b17f6c99bebac5820ed`.
- Compact design: this change's `design.md`, independently reviewed and owner accepted at SHA-256
  `10fae45ccada66c38092b02dafe6f72b6081b54e4d6c18b290c8ee3d3e21a809`.

## Why

Two Rule readiness integration tests accidentally canceled the accepted Start parent and destroyed NATS before Stop.
They were intended to prove controlled cleanup, so they must keep Start authority and NATS live until bounded Stop
returns nil. A separate proof is needed for the materially different abort lane where Start authority has already
ended and orderly native drain is no longer guaranteed.

## What changes

- Keep controlled readiness cleanup in exact LIFO order: bounded Stop, Start-parent cancellation, NATS termination.
- Define controlled Stop as drain, join, finalize, and nil while Start authority remains live.
- Define accepted-parent abort Stop as synchronous bounded best effort under the exact Stop context. Accurate native or
  deadline errors are permitted and preserved.
- Keep the isolated real-NATS abort proof observational: NATS remains live, Stop must not panic or outlive its bound,
  and whenever the exact Stop context ends, the returned error must preserve that exact context error.
- Remove #1062 production behavior whose only purpose was to manufacture nil from abort outcomes.
- Clarify the portable lifecycle GoDoc, current specification, and runtime-context composition truth.

## Non-goals

- No lifecycle production order, signature, timeout, retry, detached cleanup, retained context, exported surface, or
  generic lifecycle wrapper change.
- No watcher-sentinel normalization or runtime-command-fence reinterpretation.
- No promise of complete join, leak freedom, replacement authority, or a second rejoin after abort Stop's bound wins.
- No config, schema, wire, subject, bucket, stream, payload, query, persistence, ADR-095, or E2E change.
