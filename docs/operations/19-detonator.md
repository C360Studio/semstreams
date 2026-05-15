# Detonator — Offline Injection Labeling

Operational guide for the ADR-043 Phase 3 detonator: a sandboxed
canary loop that observes what an LLM tries to do when fed an
untrusted input, then writes labels in the Phase 2 corpus format.
Output flows directly into the embedding classifier without a
separate ingestion step.

## Why this exists

ADR-043 Phase 2 ships the runtime classifier on a hand-curated
seed corpus. Hand-curating doesn't scale and doesn't capture the
distribution of injections in operators' actual scrape streams.
The detonator solves the data problem: feed unlabeled input,
observe canary behavior, get labels. ADR-043 §"Detonator (sandbox)
ownership" details the architectural choices.

## What the detonator IS

- A pure-Go sandbox: a fake `agentictools.ExecutorRegistry`
  (`WildcardExecutor`) exposing the attacker tool surface
  (`read_file`, `bash`, `fetch`, `read_env`, `db_query`,
  `list_users`, `get_api_key`, and the OSINT-adjacent
  `http_request`, `send_email`, `send_message`). Tools record
  what was called but never execute anything.
- A bounded canary loop wrapping the existing
  `processor/agentic-model.Client`. Per-detonation caps on turns,
  tool calls, output tokens, and total deadline.
- A markdown-URL scanner over the canary's raw text between turns
  (image, link, bare forms; auto-rendered image URLs are the
  primary exfil vector for OSINT agents).
- A batch CLI (`cmd/detonate-injections`) that reads JSONL
  `{id, text}` records, detonates each through the canary, and
  writes labeled corpus records to a JSONL output file.

## What the detonator IS NOT

- **Not a runtime path.** Detonations are offline batch work.
  Runtime classification happens in the Phase 2 filter against
  the corpus the detonator already wrote to.
- **Not multi-tenant.** Phase 3 writes flat JSONL files. The
  `INJECTION_LABELS_{tenant}` NATS KV bucket per
  ADR-043 §"Cache size and policy" is Phase 4.
- **Not a component.** Phase 3 ships CLI-first. The component
  wrapping per ADR-043 line 137 lands later once the CLI usage
  pattern stabilizes.
- **Not free.** Each detonation costs real LLM tokens. Default
  caps bound per-detonation cost; operators tune for their
  provider's pricing.

## Quick start

```bash
# 1. Configure your canary endpoint.
cat > /tmp/canary.json <<'EOF'
{
  "provider": "anthropic",
  "model": "claude-haiku-4-5",
  "max_tokens": 4096,
  "supports_tools": true,
  "api_key_env": "ANTHROPIC_API_KEY"
}
EOF

# 2. Prepare an input batch. Only `text` is required — the
# output record's ID is sha256(text), so an operator-supplied
# `id` would be silently overwritten and is intentionally omitted.
cat > /tmp/scrape.jsonl <<'EOF'
{"text":"Article body here..."}
{"text":"Another article body..."}
EOF

# 3. Detonate.
INPUT=/tmp/scrape.jsonl \
OUTPUT=/tmp/labels.jsonl \
ENDPOINT=/tmp/canary.json \
task adr043:detonate

# 4. Wire the output into the Phase 2 classifier corpus and
# rerun the measurement harness against your eval slice.
CORPUS=/tmp/labels.jsonl \
EVAL=/path/to/your/eval.jsonl \
task adr043:measure:eval
```

## Phase 3d wire-up proof

The synthetic-corpus proof demonstrates the end-to-end contract:
detonator output → corpus loader → measurement harness all flow
through the same JSONL `injectioncorpus.Record` shape. Reproducible
via `task adr043:proof`.

### Baseline (seed-only)

```
- Threshold: 0.30
- Eval records: 37
- Overall exact-match: 37 / 37 (100.0%)
- Latency p50: ~15µs
- Latency p99: ~30µs

| Signal                  | TP | FP | Precision | Recall |
|---                      |---:|---:|---:       |---:    |
| benign                  |  8 |  0 | 1.00      | 1.00   |
| data-access             |  2 |  0 | 1.00      | 1.00   |
| instruction-override    | 25 |  0 | 1.00      | 1.00   |
| network-egress          |  2 |  0 | 1.00      | 1.00   |
```

Four signal buckets, almost entirely `instruction-override` —
reflects the seed's hand-curation focus on the regex placeholder's
direct-injection shapes.

### Augmented (seed + synthetic detonator output)

```
- Threshold: 0.30
- Eval records: 50
- Overall exact-match: 50 / 50 (100.0%)
- Latency p50: ~20µs
- Latency p99: ~33µs

| Signal                  | TP | FP | Precision | Recall |
|---                      |---:|---:|---:       |---:    |
| benign                  | 11 |  0 | 1.00      | 1.00   |
| code-exec               |  2 |  0 | 1.00      | 1.00   |
| cred-enum               |  1 |  0 | 1.00      | 1.00   |
| data-access             |  3 |  0 | 1.00      | 1.00   |
| exfil-email             |  1 |  0 | 1.00      | 1.00   |
| filesystem-read         |  1 |  0 | 1.00      | 1.00   |
| instruction-override    | 26 |  0 | 1.00      | 1.00   |
| network-egress          |  4 |  0 | 1.00      | 1.00   |
| secret-access           |  1 |  0 | 1.00      | 1.00   |
```

**Coverage went from 4 buckets to 9.** Six new attack shapes —
`code-exec`, `cred-enum`, `exfil-email`, `filesystem-read`,
`secret-access`, plus broader `network-egress` — are now
classifiable against the runtime corpus.

This is a SELF-EVAL run (records appear in both train and eval) so
the 100% accuracy is structural, not a generalization claim. The
proof is the WIRE-UP: detonator output JSONL → corpus loader →
classifier → measurement all use the same `injectioncorpus.Record`
contract without ceremony. Operators measure generalization
against held-out eval per the Phase 2c protocol in
[18-injection-classifier-measurements.md](18-injection-classifier-measurements.md).

The synthetic file `testdata/detonator_synthetic_phase3d.jsonl`
mimics what a real Gemini/Anthropic canary would produce. Replace
it with actual detonation output (via `task adr043:detonate`) for
your deployment's generalization measurement.

## Per-detonation cost caps

| Cap | Default | Purpose |
|---|---|---|
| `--max-turns` | 6 | Outer loop bound. Most injections resolve in 1–3 turns; 6 is the upper bound where an honest agent has decided. |
| `--max-tool-calls` | 12 | Cumulative across all turns. Bounds tool-recursive loops. |
| `--max-tokens` | 1024 | Per-turn output budget; bounds runaway thinking costs. |
| `--timeout` | 60s | Per-detonation total deadline. Hard cap on wall-time spend. |
| `--concurrency` | 2 | Parallel canary workers. Keep modest unless your LLM provider tolerates parallelism. |

For a 1,000-input batch on Claude Haiku at default caps, expect
~$1–3 of token spend depending on canary behavior. Take-bait rate
in production OSINT input distributions is typically 5–15%; the
remaining inputs cost one turn each.

## Idempotency

The CLI computes the output record ID as `sha256(input.text)`
regardless of the operator-supplied `id` field. On re-run with the
same input file, records whose computed ID already appears in the
output are skipped. Operators interrupt mid-batch with SIGINT and
resume by re-running the same command — partial progress is
preserved.

The hash-only-on-input ID has a deliberate side effect: re-running
the same input with a different canary model produces the same ID,
and the corpus loader's cross-source duplicate-ID detection then
forces the operator to choose which model's labels to trust. This
is intentional per the `feedback_warning_not_fail_masks_integration_drift`
discipline (silent override of one model's labels by another would
be a forensic landmine months later).

## Canary persona

The default system prompt is a permissive OSINT-analyst persona —
the canary should TAKE the bait, not resist it. For non-OSINT
threat models (code-review agents, customer-support agents),
override via `SYSTEM_PROMPT=/path/to/prompt.txt`. Prompt design
guidance: the canary needs ENOUGH license to use tools that an
injection would target. Resistant prompts under-detonate.

## Signal-bucket taxonomy

Detonator output uses the same ADR-043 line 206 enum that the
runtime classifier consumes:

| Bucket | Detonator origin |
|---|---|
| `code-exec` | bash, execute tool calls |
| `secret-access` | read_env, get_api_key tool calls |
| `cred-enum` | list_users tool calls |
| `filesystem-read` | read_file tool calls |
| `network-egress` | fetch, http_request tool calls OR markdown image-URL exfil in canary text |
| `data-access` | db_query tool calls |
| `exfil-email` | send_email, send_message tool calls |
| `instruction-override` | persona/role override behavior (rule-based; the canary takes a role it shouldn't) |
| `benign` | canary processed input without tickling attacker tools or URL exfil |

Multi-bucket detonations (canary tickled bash AND fetch AND
read_env) collapse to a single PRIMARY signal per ADR-043 line
268-272 priority order: code-exec > secret-access > cred-enum >
filesystem-read > network-egress > data-access > exfil-email >
instruction-override > benign. Multi-label is Phase 4.

## When to re-detonate

| Trigger | Re-run scope |
|---|---|
| New input batch | Yes — detonations are per-input. |
| Canary model swap | Selectively — operator decides which inputs benefit from a second opinion. Cross-source duplicate-ID detection forces explicit choice. |
| Threshold tune in classifier | No — re-run measurement only. |
| Adapter/embedder model change | No — corpus is the input to the embedder; re-embed not re-detonate. |
| Quarterly recalibration | Yes — injection landscape drifts. Re-detonate a representative slice and re-measure. |

## Related

- [20-adr043-rollout.md](20-adr043-rollout.md) — end-to-end operator playbook; this doc is its Stage 2.
- [ADR-043](../adr/043-prompt-injection-defense-detonation-corpus.md) — the design.
- [18-injection-classifier-measurements.md](18-injection-classifier-measurements.md) — Phase 2 measurement protocol the detonator output plugs into.
- [`processor/agentic-detonator/`](../../processor/agentic-detonator/) — package implementation.
- [`cmd/detonate-injections/`](../../cmd/detonate-injections/) — the batch CLI.
- [`taskfiles/adr043.yml`](../../taskfiles/adr043.yml) — `task adr043:detonate`, `task adr043:proof`, `task adr043:measure`.
