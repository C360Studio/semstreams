# Change: Correct agentic-tools wire terminal scope

## Status

Implementation and focused evidence are complete. Independent implementation review remains pending. No production
behavior change is authorized.

## Why

The #1063 wording added to current agentic-tools truth said every admitted call receives a terminal outcome. That
contradicts the existing `approval_required` contract and production behavior: the initial approval response is a
correlated nonterminal pause, creates no COMPLETED outcome, and leaves the same CallID eligible for approved
re-dispatch. The accepted #1063 work also lacked a fresh active OpenSpec ledger for its causal test, policy guard,
forced omissions, Docker evidence, and owner rulings.

## Accepted authority

- Accepted parent inventory SHA-256 (`INVENTORY PASS`):
  `d91a49caa42d027df482c0c8adc4ebe4f290459e5b161a81bf8d11a372662d7a`.
- Accepted parent #1063 design SHA-256:
  `56fa9dc95a4dbf6f3f7d121912972e036f7c1c2d55a8834e991eeafa8b37ae7a`.
- Independently reviewed and owner-accepted terminal-scope correction pre-acceptance SHA-256:
  `22375a461578b6100a96d838d6726c2d4f2f10bedcfe80b483fc7914e9117332`.
- Owner acceptance date: 2026-08-23.

## What changes

- Scope durable terminal correlation to logical calls reaching execution, terminal policy rejection, or COMPLETED
  replay.
- Preserve `approval_required` as correlated nonterminal coordination with its phase-distinct message ID and
  same-CallID approved re-dispatch.
- Keep `MaxAckPending=3` acknowledgement-admission-only and promise neither serialized execution nor overlap.
- Align current capability truth, README, package GoDoc, concepts, tuning wording, and durable evidence.

## Non-goals

No production component, approval behavior, CallID, message ID, allowlist, executor, outcome persistence, ACK/NAK,
MaxAckPending, subject, timeout, lifecycle, payload, schema, or configuration changes.
