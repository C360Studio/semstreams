# Post-R1c foundation remap: roadmap review

**Review type:** independent SemStreams pre-owner design review.
**Repository baseline:** `c38e3e82d5a0b1deec598ad1bf8bb21a6bf0b3fa`.
**Accepted inventory:** `docs/proposals/post-r1c-foundation-remap-inventory.md`, 447 lines, 25,852 bytes,
SHA-256 `d347b99935e9d9a8f3ddf1e97b6e3595d187e51087829ea96e06aa25321de953`.
**Reviewed roadmap:** `docs/proposals/post-r1c-foundation-remap-roadmap.md`, 626 lines, 35,379 bytes,
SHA-256 `9183f1e85e3249f362bb63b81ed5e31fdfd624be96fd4e6c26a7ef9bd99a4075`.
**Verdict:** `DESIGN REVIEW PASS`.

## Review method

The reviewer verified every submitted artifact identity and independently checked the roadmap's load-bearing premises
against production factory order, registry ownership, message-logger construction, component configuration, port
types, readiness, OpenSpec contracts, and E2E coverage. The review tested implementability and adopter defaults rather
than agreement with the recommendation.

## First review

The first roadmap identity was 459 lines, 24,581 bytes, SHA-256
`6a991cbd7d222e7189022b3e554df0edcbbd1a504728562440d242e3a7d2b16f`.

Verdict: `DESIGN CHANGES REQUESTED`.

Three findings were returned:

1. The effective-port snapshot named no implementable owner, merge contract, or lifecycle. Components are constructed
   before their ports can be inspected, message-logger is created in nondeterministic service-map order, and the draft
   did not define config restart or removal.
2. The canonical grammar lacked an exact resolver signature, required/allowed field contract, direction rules,
   precedence, binding JSON shape, first consumer for `KVReadPort`, and matching OpenSpec requirements.
3. The atomic component migration omitted `task e2e:research-graph`, despite migrating that distinct component chain.

The roadmap responded by defining one typed declaration envelope, a binding kind matrix, an unexported resolver,
first production exact-read declarations in Foundation B, complete-replacement merge semantics, Registry ownership of
one immutable snapshot per instance generation, atomic restart/removal, contract text, and research-graph E2E.

## Second review

The second roadmap identity was 601 lines, 33,195 bytes, SHA-256
`e50b6e18303d82a1515a966f553930005af40e444c892360eb9fa4013ce7138b`.

Verdict: `DESIGN CHANGES REQUESTED`.

The original findings were closed, but two message-logger corrections were required:

1. Converting default `"*"` auto-discovery into an actual NATS `>` subscription would silently expand the
   default-enabled raw-payload recorder to every permitted account subject, including undeclared product and reply
   traffic. That broadened the adopter's security and traffic cost without opt-in.
2. Foundation B could not claim message-logger consumed the unexported resolver or close #859 before Foundation C's
   registry snapshot existed.

The roadmap now records message-logger as the sole temporary raw interpreter at the Foundation B boundary. Foundation
C supplies a replaying Registry snapshot observer with full-current initial delivery, latest-complete generation
updates, defensive cloning, non-blocking registry mutation, restart/removal reconciliation, and cancellation.
Message-logger auto mode remains limited to declared normalized NATS/JetStream subjects plus explicitly configured
subjects. It never becomes account-wide. #859 cannot close until that final interpreter is removed.

## Final review

The reviewer verified the final identities above and reported:

- no closure regressions;
- truthful Foundation B sequencing;
- a complete Registry observer lifecycle;
- default exclusion of undeclared and reply traffic; and
- closure of all previous findings.

Final verdict:

> DESIGN REVIEW PASS

This verdict makes the exact roadmap eligible for owner review. It does not approve the roadmap or authorize any
runtime, specification, issue, or downstream change.

A formatting-only closure removed eight trailing Markdown hard-break spaces and updated the roadmap's inventory
identity reference. The reviewer verified the final exact identities above and returned `DESIGN REVIEW PASS`; no
design text changed.
