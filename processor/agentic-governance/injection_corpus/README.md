# injection_corpus

Labeled prompt-injection examples consumed by the agentic-governance
embedding classifier. Phase 2 of [ADR-043](../../../docs/adr/043-prompt-injection-defense-detonation-corpus.md).

## File format

JSONL. One record per line. Lines starting with `#` are treated as
comments and skipped. Schema:

```json
{
  "id": "stable-record-id",
  "text": "the labeled input",
  "signal": "instruction-override",
  "source": "internal-seed-v0/instruction_override"
}
```

| Field | Required | Notes |
|---|---|---|
| `id` | yes | Stable identifier — used as `governance.injection.top_match_id` when this record is the nearest neighbor. Phase 3 detonator writes hex sha256 of `text`. |
| `text` | yes | The labeled input (injection attempt or benign counter-example). |
| `signal` | yes | One of the buckets enumerated in ADR-043 line 206. Becomes `governance.injection.signal` on match. |
| `source` | no | Provenance string. Persisted for audit; not used in classification. |

## Signal buckets (ADR-043)

| Signal | Meaning |
|---|---|
| `instruction-override` | Attempt to redirect, replace, or invalidate the prior instruction context. Covers the eight regex categories in the legacy `injection_patterns.go`. |
| `network-egress` | Markdown-URL exfil, instructions to call outbound network APIs. |
| `data-access` | Instructions to enumerate or summarize data the agent has access to (sources, queries, profiles). |
| `code-exec` | Instructions targeting code execution / shell tooling. |
| `filesystem-read` | Instructions targeting filesystem reads. |
| `exfil-email` | Instructions to send email or messages out of band. |
| `secret-access` | Instructions targeting credentials, env vars, API keys. |
| `cred-enum` | Instructions to enumerate users, identities, accounts. |
| `benign` | Legitimate text that uses adjacent vocabulary; teaches the boundary. |

## Bootstrap source: `internal_seed_v0.jsonl`

A small (~35 records) hand-curated seed, sourced from:

1. The `Examples []string` fields of `DefaultInjectionPatterns` in
   [`injection_patterns.go`](../injection_patterns.go) — the existing
   regex placeholder. These are direct-injection examples.
2. Hand-authored indirect-injection examples covering the OSINT
   threat shapes ADR-043 centers on: page-embedded directives,
   markdown-URL exfil, tool-shadowing.
3. Hand-authored benign counter-examples: legitimate text that uses
   words like `system`, `instructions`, `override`, `rules`,
   `base64`, etc. without being an injection.

This seed exists to prove the loader contract end-to-end and to
provide a non-trivial smoke-test corpus. **It is not the production
corpus.** The Phase 2 measurement protocol will demonstrate what this
seed can and cannot detect, and the Phase 3 detonator will broaden
distribution against real OSINT scrape input.

## Adding additional sources

Future PRs:

- `deepset_v1.jsonl` — vendored subset of
  [deepset/prompt-injections](https://huggingface.co/datasets/deepset/prompt-injections)
  (CC-BY-4.0, attribution required).
- `greshake_v1.jsonl` — derived from Greshake `scenarios/`
  ([greshake/llm-security](https://github.com/greshake/llm-security),
  MIT). Indirect-injection gold.
- `detonator-{tenant}.jsonl` — written by the Phase 3 detonator;
  per-tenant in production.

Each additional source gets its own `Source` entry in the
loader configuration; the classifier aggregates across all of them.
License attribution lives in
`docs/operations/NN-injection-corpus-attribution.md` per the
ADR-043 §"Bootstrap corpus" section.
