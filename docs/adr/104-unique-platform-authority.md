# ADR-104: The Platform Authority Is Unique by Default

## Status

**Proposed (2026-08-30)** — pending owner acceptance on #1168. Amends ADR-102 (decision 7's "pre-v1 or never" now
applies to a value the *framework* mints, not only one the operator wrote) by reference. Supersedes nothing.
Mechanics live in the `entity-id-contract` and `component-runtime-config` capability specs.

Scope note: the run-identity half of the reviewed draft (a framework family whose instance digests its full origin)
left this ADR with the owner scope cut of 2026-08-30 and belongs to **#1192**; the import-lane admission fact
belongs to **#1194**. Neither is decided here.

## Context

`platform.org` / `platform.id` are positions 1–2 of every identity a deployment mints (ADR-102). They were validated
for shape and byte budget only, so two deployments provisioned from one configuration template mint under the same
authority — and ADR-102 d7 forbids ever rewriting a minted authority, which makes the collision permanent rather
than repairable. The only detector was `local_authority_claimed`, which fires after two deployments have already
collided inside one graph.

A first-boot persist-and-compare seam already existed (`config.Manager.Start` against the `semstreams_config`
bucket), but it persisted a value the file held and never minted one. Inventory:
`docs/proposals/gh1168-federation-identity-inventory.md`. Owner ruling 2026-08-30: greenfield — no deprecation, no
alias, no parallel path; sisters handle their own migration.

## Decision

1. **`platform.id` is unique by default.** On a deployment's genuine first boot the framework mints a six-hex-byte
   entropy suffix from `crypto/rand`, records it once with an atomic `Create`, and adopts it on every later boot and
   in every co-process sharing that configuration bucket. Nothing in the configuration document disables the mint.

2. **The durable record is keyed on the stem and is not configuration.** `semstreams_config/platform_identity`
   carries exactly `{"org": …, "stem": …, "id": …}` — the declared authority, the declared identifier, and the
   effective identifier. It is created once and never rewritten, never pushed by configuration synchronization,
   never applied back into memory by it, and never watched. The KV `platform` **config** key becomes a read-only
   mirror: still published for the UI, never applied back over the running authority.

3. **Identity is established before arbitration, from one read, in three branches.** The record present → adopt it
   (refusing unless the record's `org` matches and the file's `platform.id` equals the record's `stem` or its `id`).
   The record absent and the bucket otherwise empty → mint and `Create`. The record absent and other keys present →
   **refuse Start naming that cause, minting nothing and creating nothing**: such a bucket predates identity minting,
   and minting into it would durably record an authority the deployment's own guard then rejects for the wrong
   reason.

4. **The opt-out is the record, not a configuration key.** An operator who owns global uniqueness pre-creates
   `platform_identity` with `id == stem`; the adopt branch takes it and validates it exactly as it validates a
   configuration value. A boolean in the configuration document was rejected: the document is the very artifact the
   threat model says gets cloned, so a cloned opt-out recreates the footgun the mint exists to close (owner ruling,
   2026-08-30).

5. **The authority-pair budget reserves the suffix.** Configuration load bounds `len(org) + len(id) + 7` against the
   family-table budget, so a pair that fits only unsuffixed cannot be durably minted and then rejected forever.

## Consequences

- BREAKING, in the beta.163 wave: every deployment's `platform.id` gains a suffix on its next boot against fresh
  storage, so every entity it mints moves. Fresh storage, no migration (ADR-102 d7).
- **A bucket carried over from before this change refuses Start, loudly, and creates nothing** — the refusal names
  the pre-identity bucket and instructs fresh storage. It is repeatable, not a wedge.
- Adopter fixtures and e2e stop predicting the pair from a configuration file and read
  `semstreams_config/platform_identity` instead — the framework observes; nobody computes.
- The declarable authority pair loses 7 bytes of headroom (163 rather than 170), uniformly.
- #1188 namespaces this bucket by the pre-mint `(org, stem)` and retires the gh#459 guard; the mechanism above is
  correct without that guard, because the adopt branch performs its own comparison.

## Alternatives rejected

- **A configuration knob** (`platform.unique: true`, `platform.mint_suffix: false`) — cloned N times it recreates the
  shared authority; and an unknown-key typo is silently dropped by `encoding/json`, so it would also require a strict
  `platform`-block key check whose only motivating hazard is the knob itself.
- **An environment override** — forks identity between co-processes sharing one bucket.
- **Minting at config load and rewriting the file** — read-only mounts fail; two hosts sharing a config diverge.
- **Deriving from a host fact such as the hostname** — clones with equal hostnames still collide; not entropy.
- **Refusing an entropy-less identifier at load** — "entropy-less" is undecidable by grammar, and it moves the mint
  into the operator's hands.

## Cross-repo contract

A sister conforms when: its composition root passes `deps.Platform` unchanged; its configuration files accept the
minted suffix, or it pre-creates `platform_identity` for a deployment that must stay unsuffixed; and its fixtures,
e2e and tooling read the effective pair from `semstreams_config/platform_identity` rather than predicting it from a
configuration file. The record's shape is normative in the `component-runtime-config` capability spec: exactly the
three fields `org`, `stem`, `id`.
