# Injection Classifier Measurements

Operational protocol for the ADR-043 Phase 2 embedding classifier
filter. Operators run the measurement harness to tune the classifier
threshold for their corpus and to validate that the embedding tier
actually adds value over the existing T0 regex filter and the
candidate T3 LLM-as-judge tier.

## Why this exists

ADR-043 §"Risks and open questions" calls out three concerns the
measurement harness has to answer before flipping the classifier
filter from default-off to enforce mode in production:

1. **Bootstrap corpus contamination**: public corpora may share
   phrasing with legitimate OSINT content. Without per-deployment
   benign-slice calibration, the classifier ships a false-positive
   footgun.
2. **Frontier-floor calculus**: at Gemini 3.x Pro / Sonnet 4.6
   capability, T3 LLM-as-judge may be sufficient alone. If so, the
   embedding tier is cost amortization rather than a defense layer.
3. **Adversarial robustness**: out-of-distribution attacks miss the
   embedding tier; the chain must fall through to T3 on low-confidence
   matches rather than fail open.

The Phase 2 harness produces the numbers for items 1 and 2. Item 3
is a Phase 3+ chain-orchestration concern.

## Harness contract

`cmd/measure-injection-classifier` (invoked via
[`task adr043:measure`](../../Taskfile.yml) for smoke runs and
[`task adr043:measure:eval`](../../taskfiles/adr043.yml) for real
runs). Loads a classifier corpus, classifies an eval corpus against
it, emits a markdown table of precision/recall per signal bucket
plus latency percentiles.

Two modes:

- **`--self-eval`**: classify the corpus against itself. Smoke mode.
  Produces the noise floor for the vendored seed. Useful as a CI
  regression target — if loader or classifier change breaks the
  shape, the self-eval numbers diverge from 100/100.
- **`--eval-corpus FILE`**: classify FILE against the configured
  `--corpus`. Production mode operators use with their own holdout
  eval set and benign-OSINT slice.

The harness does **not** call any LLM. The T2-vs-T3-only comparison
column is the operator's job — bring your own LLM endpoint config
and use it on the same eval set.

## Smoke run output (Phase 2 vendored seed)

Reproducible via `task adr043:measure`. Output below is the
regression target; if loader or classifier changes degrade it,
something broke.

```
# Injection Classifier Measurement

- Threshold: 0.30
- Eval records: 37
- Overall exact-match: 37 / 37 (100.0%)
- Latency p50: ~20µs
- Latency p99: ~50µs

## Per-signal precision / recall

| Signal                  | TP | FP | FN | Precision | Recall |
|---                      |---:|---:|---:|---:       |---:    |
| `benign`                |  8 |  0 |  0 | 1.00      | 1.00   |
| `data-access`           |  2 |  0 |  0 | 1.00      | 1.00   |
| `instruction-override`  | 25 |  0 |  0 | 1.00      | 1.00   |
| `network-egress`        |  2 |  0 |  0 | 1.00      | 1.00   |
```

Self-eval being 100/100 across all buckets only proves the loader
and classifier are wired correctly — the corpus appears in the
training set, so the classifier finds an exact lexical match for
every record. **This is not a generalization claim.** It is the
floor we expect; anything below means something is broken in the
pipeline.

## Production measurement protocol

Before enabling the classifier filter in enforce mode, run the
following four measurements on your deployment's actual input
distribution.

### 1. Held-out precision/recall

Split your corpus into train (~80%) and eval (~20%). Build the
classifier from the train slice; measure against the eval slice.

```bash
CORPUS=corpora/train.jsonl \
EVAL=corpora/eval.jsonl \
THRESHOLD=0.70 \
task adr043:measure:eval
```

Pass criteria depend on your deployment, but as guardrails:

- `instruction-override` recall ≥ 0.80 (we want to catch most
  obvious injections).
- `benign` precision ≥ 0.95 (we very much want NOT to misclassify
  benign content as an attack).

If either guardrail fails, adjust the threshold or grow the corpus
before flipping to enforce mode.

### 2. Benign-OSINT false-positive rate

Collect a slice of your **actual scrape stream** known to be benign
(legitimate articles, social posts, PDFs — whatever your OSINT
pipeline ingests). Classify it.

```bash
CORPUS=corpora/train.jsonl \
EVAL=corpora/benign_scrape_2026q2.jsonl \
THRESHOLD=0.70 \
task adr043:measure:eval
```

Every record's true signal is `benign`, so this measurement reduces
to: what fraction got classified as anything other than `benign` or
`no_match`? That's your per-deployment false-positive rate. **This
is the ADR-043 line 278 calibration step**. The classifier filter
cannot safely flip to default-on without this number being below
operator tolerance.

### 3. T2-vs-T3-only comparison

If you have a T3 LLM endpoint configured (Gemini 3.x Pro, Sonnet
4.6, GPT-5), run your eval slice through BOTH the embedding
classifier AND a single-call LLM-as-judge prompt. Compare:

- Accuracy (per-signal precision/recall).
- p50/p99 latency.
- Per-classification token cost (T3 only).

If T3 alone hits the accuracy bar with acceptable latency and cost,
the embedding tier becomes a cost optimization rather than a
defense layer. Per ADR-043 §"Alternatives considered" / "Skip the
embedding tier", this is the explicit Phase 2 decision point.

This step is operator-driven; the Phase 2 harness does not invoke
LLMs (no endpoint config, no cost discipline at this stage). The
Phase 4 chain orchestration will fold the comparison into a proper
T2→T3 fallback once the per-deployment numbers exist.

### 4. Latency budget

Read the `Latency p50` / `p99` lines from the eval run. For an
embedding classifier with a ~20k-record corpus, both should be
sub-millisecond on modern hardware. If they aren't, either the
corpus has grown beyond the per-bucket TTL or the BM25 vocabulary
needs trimming — investigate before shipping enforce mode.

## When to re-run

| Trigger | Re-run |
|---|---|
| Corpus revision (new records, retired records, label changes) | Yes, all four. |
| Threshold tune | Yes, items 1 and 2. |
| Adapter / embedder model change | Yes, all four. |
| Operator role / persona change | Yes, item 2 (benign slice may shift). |
| Quarterly recalibration | Yes, all four; treats injection-landscape drift. |

## Versioning

This document tracks the **harness contract**, not the per-deployment
numbers. The smoke run output above moves only when the vendored
seed corpus changes or when loader/classifier behavior shifts.
Per-deployment measurements live in operator notebooks or runbooks,
not in this doc.

## Related

- [ADR-043](../adr/043-prompt-injection-defense-detonation-corpus.md) — the design.
- [`cmd/measure-injection-classifier/main.go`](../../cmd/measure-injection-classifier/main.go) — the harness.
- [`processor/agentic-governance/injection_corpus/`](../../processor/agentic-governance/injection_corpus/) — corpus format and vendored seed.
- [`processor/agentic-governance/injection_classifier.go`](../../processor/agentic-governance/injection_classifier.go) — the runtime filter.
- [`taskfiles/adr043.yml`](../../taskfiles/adr043.yml) — task target definitions.
