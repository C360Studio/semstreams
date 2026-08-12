<!-- markdownlint-disable MD041 -->

**Status:** The address cutover, exact post-Foundation-B control/current-census amendments, startup-atomic correction,
focused race gates, service census/full service race, independently rerun full repository race suite, and fresh
corrected-tree crud-tools/agentic E2Es are complete and green. Frozen TSV diffs are empty and strict OpenSpec is 42/42.
Final independent SemStreams correction review returned `APPROVE`; the change is complete, green, and merge-ready but
has not merged. Candidate selection has not begun; no product tag exists, and #827 has not executed. Issue #810
remains parked with no generic overlap implementation.

## Why

Tool discovery is request/reply traffic, but `agentic-tools` previously declared the logical `tool.list` input as
ordinary core NATS on the default subject `tool.list`. A JetStream stream covering `tool.>` could answer that request
with a publish acknowledgement before the discovery responder answered. The former broad shipped stream guidance
therefore turned a healthy catalog into a silent empty or malformed response.

Issue #842 reserved a breaking wave for moving the default discovery address. That narrow cutover is now approved.
It removes the shipped collision without reviving #810's broader guard program.

## What Changes

- Keep the logical input port name `tool.list` so component wiring and discovery semantics retain one stable identity.
- Change its port kind to `nats-request` and its default subject to `discovery.tool.list`.
- Resolve one effective subject from the port configuration and subscribe only to that subject.
- Allow an operator to replace the subject only with another `nats-request` configuration for the same logical port.
- Fail startup when an override supplies legacy kind `nats`; do not repair or reinterpret it.
- Add no responder, alias, or compatibility subscription at the former default `tool.list` subject.
- Narrow live AGENT stream guidance from `tool.>` to the explicit `tool.execute.>` and `tool.result.>` families.
- Publish a direct migration note for discovery clients and deployments with explicit port overrides.
- Require both crud-tools and agentic E2E proof before the breaking cutover is integrated.
- Amend Foundation-B target accounting by retiring the exact frozen `tool.list|NATSPort` Go identity and adding the
  exact current `tool.list|NATSRequestPort` identity without rewriting either frozen TSV.
- Amend only the mechanically current message-logger census kind partition for the same nine inherited inputs:
  `nats_inputs` `18→9` and `nats_request_inputs` `9→18`; preserve every version-2 authority field and every other
  computed field.
- Make discovery subscription and later input-consumer startup fail closed and atomic: return an observable transient
  discovery-subscribe error, roll back only local resources allocated by the failed attempt, leave `running=false`,
  preserve durable consumer state, and permit a clean subsequent start.
- Correct two stale live comments that still describe broad `tool.>` coverage as current guidance.
- Rerun focused/full race verification and fresh crud-tools plus agentic E2Es, then obtain independent review of the
  correction before restoring complete/green change status.

## Capabilities

### Modified Capabilities

- `agentic-tools`: Moves the default tool-discovery request address and makes its request/reply kind explicit.

## Impact

This is a deliberate breaking address and configuration-kind change. Clients that publish discovery requests to the
former default `tool.list` subject must move to `discovery.tool.list`. Deployments that explicitly configure the
logical `tool.list` port as kind `nats` must migrate it to kind `nats-request`. A same-kind custom subject remains
supported.

There is no compatibility window. The runtime serves only the resolved subject, so a default deployment does not also
answer `tool.list`. This keeps the migration observable and prevents the old collision from surviving as a hidden
second route.

Runtime, control, current-census, focused/full race, and fresh crud-tools/agentic E2E evidence are green on the
corrected tree, and final independent correction review returned `APPROVE`. The configured future merge may close
#842; this status does not claim that merge occurred. Issue #810 remains parked as the generic operator-defined overlap
problem. This change adds no overlap guard, request-subject registry, publish-ack decoder, or exported subject
inventory.

No candidate has been selected. This change makes no product-tag or #827 claim.

## Non-goals

- No legacy responder or alias at `tool.list`.
- No automatic conversion from `nats` to `nats-request`.
- No generic detection of an operator-defined request subject captured by a stream.
- No provisioning guard, declared-subject registry, publish-ack decoder, or subject export from the parked #810 work.
- No rewrite of historical ADRs, remap inventories, archived OpenSpec, or the frozen pre-v1 program.
- No candidate selection, release authorization, product tag, or #827 execution.
