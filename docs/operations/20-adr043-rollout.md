# ADR-043 Rollout Playbook

End-to-end operator guide for deploying the prompt-injection
classifier from the moment the [Phase 2](18-injection-classifier-measurements.md)
and [Phase 3](19-detonator.md) PRs merge to a production enforce-mode
classifier soaked against your scrape distribution.

This document is the orchestration layer. Each stage links to the
detailed reference doc for that stage's mechanics.

## Reading order

If you're new to ADR-043 read first:

1. [ADR-043](../adr/043-prompt-injection-defense-detonation-corpus.md) — the architecture and why.
2. This playbook — the operator timeline and decision points.
3. [18-injection-classifier-measurements.md](18-injection-classifier-measurements.md) — measurement mechanics.
4. [19-detonator.md](19-detonator.md) — detonator mechanics.

## Where the safety lives

The classifier filter ships **default off**. When enabled, it
defaults to **shadow mode** — verdicts emit to metrics + violation
events but never block traffic. The flip from shadow to enforce
is a deliberate operator decision gated by per-deployment
measurements, NOT a config toggle that flips on first install.

Two ADR-043 risks drive this discipline:

- **Line 278: bootstrap corpus contamination.** Public corpora
  may share phrasing with legitimate OSINT content. The benign
  false-positive rate cannot be known without measuring against
  the operator's own benign-input distribution.
- **Line 276: frontier-floor calculus.** Sufficient LLM-as-judge
  capability could render the embedding tier cost amortization
  rather than defense. The decision is data-driven, not a hunch.

Treat the rollout as a calibration project, not a flag flip.

## Stages

### Stage 1: Bootstrap deployment

**Goal**: deploy the classifier code path with the filter off; the
regex placeholder keeps doing its existing job.

**Actions**:

1. Merge PR #84 (Phase 2) and #85 (Phase 3).
2. No config change needed — the classifier filter is opt-in via
   `FilterConfig.injection_classifier`. Without that entry, only
   the existing regex `injection_detection` filter runs.
3. Verify regex filter unchanged in dashboards
   (`semstreams_governance_filter_total{filter="injection_detection"}`).
4. Verify no new metric cardinality
   (`semstreams_governance_injection_classifier_decisions_total`
   should not appear yet).

**Exit criteria**: existing governance posture unchanged.

### Stage 2: Generate a labeled corpus

**Goal**: produce a per-deployment corpus from real scrape input
so subsequent measurements reflect production reality.

**Actions**:

1. Collect a representative slice of your OSINT scrape: 100–500
   raw text inputs. Mix definitely-benign with samples flagged by
   adjacent systems (URL filters, content filters) as potentially
   adversarial. **No labels yet** — just the text.

2. Configure a canary LLM endpoint. Any model.Registry-shaped
   endpoint works (`anthropic`, `openai`, `openrouter`, `ollama`).
   Cheaper-and-faster wins here — Claude Haiku or Gemini 2.5 Flash
   are good defaults. See [`19-detonator.md`](19-detonator.md#quick-start)
   for the JSON shape.

3. Run the detonator:

   ```bash
   INPUT=corpora/scrape-sample.jsonl \
   OUTPUT=corpora/detonated-v1.jsonl \
   ENDPOINT=configs/canary.json \
   task adr043:detonate
   ```

   Expected wallclock: ~30 minutes for 500 inputs at concurrency=2
   on Claude Haiku. Expected token cost: \$1–3.

4. Inspect the output. Read 10–20 records by eye. Do the signal
   labels match what you'd expect a human analyst to call the
   inputs? Common surprises:
   - Heavy `instruction-override` skew → canary persona is too
     deferential; tune `--system-prompt-file`.
   - Everything is `benign` → canary persona is resistant; loosen
     the prompt.
   - Random distribution → canary is hallucinating tool calls;
     pick a different model.

**Exit criteria**: a JSONL corpus that smells right when read by
hand. Don't proceed to measurement until the labels look defensible.

### Stage 3: Measure baseline + benign-FP rate

**Goal**: get the two numbers that determine whether you can move
to shadow mode.

**Actions**:

1. **Held-out evaluation** — split your detonated corpus 80/20.
   Build the classifier from 80%; measure against 20%:

   ```bash
   CORPUS=corpora/detonated-v1-train.jsonl \
   EVAL=corpora/detonated-v1-eval.jsonl \
   THRESHOLD=0.70 \
   task adr043:measure:eval
   ```

   Record the per-signal precision/recall and overall latency.

2. **Benign-OSINT calibration** (the decisive step) — collect a
   separate slice of inputs you KNOW are benign (legitimate
   articles, social posts, internal documents you've reviewed).
   Label them all `benign` and run them through the same
   classifier:

   ```bash
   CORPUS=corpora/detonated-v1.jsonl \
   EVAL=corpora/benign-osint.jsonl \
   THRESHOLD=0.70 \
   task adr043:measure:eval
   ```

   Every record's truth is `benign`. The harness output tells you
   what fraction got classified as something else — your
   **false-positive rate**.

3. Decision point — is the FP rate acceptable?

   | FP rate | Verdict |
   |---|---|
   | < 1% | Safe to ship to shadow mode. |
   | 1–5% | Tune threshold (Stage 4) or add benign counter-examples to corpus. |
   | > 5% | Corpus needs more benign coverage. Run more detonations against known-benign input. |

**Exit criteria**: FP rate < 5% and held-out recall ≥ 80% on the
attack buckets you care about.

### Stage 4: Tune threshold

**Goal**: find the threshold that maximizes recall without
crossing your FP tolerance.

**Actions**:

1. Sweep thresholds via the harness:

   ```bash
   for t in 0.50 0.55 0.60 0.65 0.70 0.75 0.80; do
     echo "=== threshold=$t ==="
     CORPUS=... EVAL=... THRESHOLD=$t task adr043:measure:eval
   done
   ```

2. Plot FP rate (from Stage 3 benign eval) vs recall (from
   Stage 3 held-out) at each threshold. Pick the threshold at
   the elbow — the highest one where recall is still acceptable
   and FP is below your tolerance.

3. Common operating points (your numbers will differ):

   | Posture | Threshold | Trade-off |
   |---|---|---|
   | Conservative | 0.75–0.80 | Lower recall, very low FP. Misses subtle injections; catches the obvious 80%. |
   | Balanced | 0.65–0.70 | Default-ish. Catches most attacks; tolerable FP. |
   | Aggressive | 0.55–0.60 | High recall; FP requires more benign corpus tuning to make production-safe. |

**Exit criteria**: a specific threshold value with FP + recall
numbers committed to your deployment notebook.

### Stage 5: Flip shadow mode

**Goal**: classifier verdicts emit to metrics + violation events
in production without blocking traffic.

**Actions**:

1. Add the classifier filter to your `agentic-governance`
   `FilterChain.Filters` config:

   ```yaml
   filters:
     - name: injection_classifier
       enabled: true
       classifier_config:
         threshold: 0.70  # from Stage 4
         shadow_mode: true  # IMPORTANT — verdicts only, no blocking
         corpus_sources:
           - domain: detonated-v1
             version: v1
             path: /etc/semstreams/corpora/detonated-v1.jsonl
   ```

2. Roll out via your usual deploy mechanism. Confirm the new
   metrics appear:
   - `semstreams_governance_injection_classifier_decisions_total{verdict,signal}`
   - `semstreams_governance_injection_classifier_score`
   - `semstreams_governance_filter_latency_seconds{filter="injection_classifier"}`

3. Watch for `verdict="shadow"` increments. These are the
   verdicts that WOULD block in enforce mode.

**Exit criteria**: dashboards show classifier decisions; no
production traffic blocked.

### Stage 6: Soak shadow mode

**Goal**: validate the classifier's decisions against real
production traffic without paying the cost of a wrong call.

**Actions**:

1. Run shadow mode for at least **7 days** of representative
   production traffic. Longer if your traffic mix is bursty or
   if you have monthly cycles.

2. Sample 50–100 `verdict="shadow"` violations from the
   `governance.violation.*` JetStream subject. For each:
   - Read the original `Message.Content.Text`.
   - Read the verdict (`signal`, `score`, `top_match_id`).
   - Decide: was this a true positive (genuine injection) or
     false positive (legitimate content)?

3. Compute the production FP rate. Compare to Stage 3's
   measurement.

4. Decision point:

   | Production FP vs measured | Action |
   |---|---|
   | Within 2× | Shadow data confirms the offline measurement. Proceed to Stage 7. |
   | 2–5× higher | Your detonation corpus didn't capture the production benign distribution well enough. Loop back to Stage 2 with more diverse benign inputs. |
   | More than 5× | Something is structurally wrong — re-examine corpus quality, threshold choice, or the canary model itself. |

**Exit criteria**: production FP rate is within 2× the offline
measurement and operator confidence the classifier is calling
things correctly.

### Stage 7: Flip to enforce mode

**Goal**: classifier verdicts block traffic. The defense layer
becomes load-bearing.

**Actions**:

1. Flip the config:

   ```yaml
   filters:
     - name: injection_classifier
       enabled: true
       classifier_config:
         threshold: 0.70
         shadow_mode: false  # was true
         corpus_sources:
           - ...
   ```

2. Roll out staged — canary deploy if your infra supports it.
   Watch:
   - `semstreams_governance_injection_classifier_decisions_total{verdict="block"}` — should match the shadow-mode rate from Stage 6.
   - User-error notifications. Some legitimate users will see "your message was blocked" — be ready to triage.
   - p99 latency on the message-validation path.

3. Hold your hand near the rollback switch (Stage 8) for the
   first 24 hours.

**Exit criteria**: classifier blocking in production with verdict
rates matching Stage 6 shadow-mode rates.

### Stage 8: Rollback procedure

**Goal**: when enforce mode starts blocking legitimate content,
revert safely and re-tune.

**Actions**:

1. **First**, flip `shadow_mode: true` and redeploy. Verdicts
   continue to emit but traffic flows. This is reversible in
   minutes; do not wait until you've diagnosed.

2. **Then**, investigate:
   - Pull `governance.violation.*` events with `Action=blocked`
     in the window of operator concern.
   - Read 20–30 by hand. Classify each as true/false positive.
   - Cluster the false positives by `top_match_id` (which seed
     record triggered the misclassification). One bad seed
     record typically drives most of the FP volume.

3. **Fix**:
   - If clustered: edit the offending corpus record (delete it,
     or add benign counter-examples that the classifier will
     match instead).
   - If diffuse: the threshold is too low. Raise by 0.05 increments
     and re-run Stage 3 measurement before flipping back to enforce.

4. Loop through Stages 3–6 with the new corpus or threshold,
   then re-enter Stage 7.

### Stage 9: Quarterly recalibration

**Goal**: stay ahead of injection landscape drift.

**Actions**:

Every 90 days OR when you observe an attack class the classifier
misses:

1. Re-collect a representative scrape sample (Stage 2).
2. Re-detonate against the **same canary model** for comparability.
3. Re-measure (Stage 3). Has the FP or recall shifted?
4. If material drift: flip back to shadow (Stage 5), then loop
   through 6→7.

The Phase 2 measurement doc has the trigger matrix at
[18-injection-classifier-measurements.md § When to re-run](18-injection-classifier-measurements.md#when-to-re-run).

## Decision cheat sheet

| Decision | Threshold to clear |
|---|---|
| OK to enable classifier in shadow mode | Offline FP < 5%, held-out recall ≥ 80% |
| OK to flip enforce mode | 7-day shadow soak; production FP within 2× of offline measurement |
| Rollback to shadow | Operator triage shows ≥ 10 FP per hour OR a user-impact incident |
| Refresh corpus | Quarterly OR new attack class observed in shadow |
| Refresh threshold | Material drift in production FP rate (> 2× change month-over-month) |

## Frequently asked questions

**Q: Why not ship default-on?**
Public corpora and even hand-curated seeds don't reliably represent
your deployment's benign-input distribution. Default-on without
per-deployment calibration is the FP-incident path. See ADR-043
line 278.

**Q: How much does the detonator cost to run?**
~\$1–3 per 1000 inputs on Claude Haiku at default caps. Costs are
upfront (corpus building) and quarterly (recalibration), not
per-request. Runtime classification is free — cosine over
in-memory vectors at sub-millisecond p99.

**Q: Can I skip the detonator and use only public corpora?**
You can, but you'll either over-block (default-tuned public corpora
on your benign distribution) or under-catch (your scrape stream
has injection shapes the public corpora don't). The detonator
generates labels distributed exactly like your real input — that's
the leverage.

**Q: What if my LLM provider rate-limits the detonator?**
Lower `--concurrency`. The detonator is batch work; spreading the
~30 minutes over a few hours is fine.

**Q: Does this work for non-OSINT agents?**
Yes — override `SYSTEM_PROMPT` to a persona matching your agent
shape (code-review, customer-support, data-analyst, etc.). The
infrastructure is agent-shape-agnostic; only the canary prompt is
domain-specific.

**Q: Can rules match on classifier verdicts?**
Two of the four predicates (`signal`, `tier`) are rule-visible by
default. Two (`score`, `top_match_id`) are rule-opaque to prevent
adversaries gaming the score and to insulate rules from corpus
revisions. Operators can opt rules into matching on the opaque
predicates per deployment via the vocabulary registry.

## Related

- [ADR-043](../adr/043-prompt-injection-defense-detonation-corpus.md) — design.
- [18-injection-classifier-measurements.md](18-injection-classifier-measurements.md) — measurement mechanics.
- [19-detonator.md](19-detonator.md) — detonator mechanics.
- [17-tool-call-governance.md](17-tool-call-governance.md) — adjacent governance layer (tool-call filter).
