# Golden: the edge triage agent, built blind

Version 1 — written 2026-09-14 at owner direction. Tracked by #1306 / #1315. Status: not yet run.
Proposed protocol P2 and NEAR supplement N1 (2026-09-24) await owner adoption; R1–R10 behavior is unchanged.

This is a measurement instrument, not a product. It is rerun by a fresh agent at every milestone tag and release
candidate. The metric is how far an outside developer gets on the agentic path from the documentation alone, and
what the framework does when the box loses its uplink. Requirements are stated as behavior so they survive API churn;
the friction log is the delta between runs.

## 1. When it runs

Preconditions for run 1, all three:

1. The restart work under #1146 (including #1362) and the composition work in #1301 have landed on main.
2. The four known docs defects have landed: `docs/basics/05` retired payload registration; `docs/basics/07`
   `publish_agent` example (missing `subject`, `{{.EntityID}}` templating); `docs/advanced/06` omits `publish_agent`;
   the approval flow unreachable from `docs/basics`. Running before they land re-records known friction.
3. A tag exists at or after both. **Every run pins to a tag, never HEAD.** Record the tag in the run record.

Reruns: at every milestone tag and every RC tag. Compare counts against the previous run at its tag.

## 2. Roles

- **Builder** — a fresh Opus developer session that has NOT read the v1 identity review, the issue tracker, or any
  prior run record. It receives only the brief in §9. Never the session that wrote the review or a prior run.
- **Scorer** — a separate reviewer session that verifies the friction log against the builder's transcript and
  files the run record. The scorer may read everything.
- **Owner** reads the run record. Nothing here is a ruling.

## 3. The blindfold

The builder may read:

- `README.md`, everything under `docs/` except `docs/contributing/golden-edge-agent-runs/`, and Go doc comments
  (`go doc`, pkg.go.dev, or reading a package's `.go` files for their comments and exported signatures).
- Any file reachable by following a link from `README.md` or `docs/`, including examples and configs the docs link.
- The framework's public HTTP and CLI surfaces at runtime (`semstreams catalog`, `validate`, OpenAPI, `/loops`).

The builder may NOT read, and must log every violation:

- `cmd/`, `examples/`, `configs/`, `test/`, `scripts/`, `taskfiles/`, `openspec/`, `.agents/`, `.claude/` except via a
  docs link.
- Any sister repository. Any prior run record. GitHub issues, pull requests, and discussions.
- Protocol P2: this document's §12 and `docs/proposals/near-governance-audit/` (planning and scoring material).

A violation is logged with the reason it was necessary and the run continues. **The violation count is the primary
metric.** Zero is the target state; the number is the truth.

## 4. The app

Out-of-tree Go module (`edgetriage/`), depending on `github.com/c360studio/semstreams` at the pinned tag. Neutral
sensor domain only — no product vocabulary (no MAVLink, no COP, no dev-workflow terms). Local NATS (container or
binary). Model endpoint: a local Ollama instance with a small instruct model (8B class) is the reference
configuration; a mock or cloud endpoint is permitted for R1–R6 if no local model is available, and the run record says
which was used. R7 requires a real endpoint that can be made unreachable.

## 5. Requirements (behavior, not API)

Each is scored MET / MET WITH FRICTION / NOT MET / NOT ATTEMPTED with one sentence of evidence.

- **R1 Compose.** One binary boots the framework plus the app's own types, with the app's tool available to the
  agent. Record: the number of framework registration or wiring calls the builder had to write, and their source.
- **R2 Ingest.** A domain reading (station id, temperature, battery, timestamp) sent to the running binary becomes a
  graph entity with a deterministic 6-part ID, retrievable by ID over HTTP.
- **R3 Rule.** When a station's temperature exceeds a threshold, a rule fires once per crossing (not once per
  reading) and an agent loop starts whose prompt names the station entity.
- **R4 Tool.** The agent can call one app-defined effectful tool (`set_station_mode`, which records a mode change as
  a graph fact) and the framework's read-only graph tools.
- **R5 Human in the loop.** The effectful tool does not execute until a human approves. The pending approval is
  observable from outside the process (HTTP or KV) with enough context to decide. Approve → the tool runs and the
  loop continues. Reject → the loop records the rejection and reaches a terminal state without executing.
- **R6 Result and evidence.** After completion, a non-Go client (curl) can fetch: the loop's terminal state, the
  tool call that ran, what the tool returned, and the approval decision. Record the number of requests and the number
  of distinct ports or services it took.
- **R7 Uplink loss.** With the loop mid-flight, make the model endpoint unreachable. Record what the loop does,
  what state is visible, and whether anything fails silently. Restore the endpoint; send a new crossing; record
  whether a new loop completes. Store-and-forward is NOT required; honest, observable behavior is.
- **R8 Air gap.** With no search API key and no egress, ask the agent to look something up. Record whether the
  framework refuses, errors, or returns fabricated results. Fabrication is NOT MET.
- **R9 Self-description.** Without reading Go, the builder can discover from the running binary: the components and
  their subjects, the tools available to the agent, and the rule action's required fields. Record which of the
  three were discoverable and from where.
- **R10 Optional, wrong clock.** If the environment allows, boot with the clock set back one day and repeat R5 with a
  pending approval spanning the skew. Record what happens to the approval.

## 6. The friction log

One row per event, kept as the builder goes, in this shape:

| # | Requirement | Needed to know | Where the docs said (or "nowhere") | Where it was actually found | Class | Minutes lost |
|---|---|---|---|---|---|---|

Classes: `undocumented` · `doc-wrong` (says something false) · `doc-stale` (was true, no longer) · `doc-misplaced`
(true, but not where an outsider looks) · `silent` (it did not work and nothing said so) · `violation` (had to read a
forbidden path; say which).

The scorer verifies every row against the transcript and rejects rows it cannot verify.

## 7. Measurements

| Measure | How |
|---|---|
| Violations | count of `violation` rows |
| Silent failures | count of `silent` rows |
| Time to first completed loop | wall clock from module init to R3+R4 completing once |
| Time to first approved effectful call | wall clock to the first R5 approve path |
| Composition burden | lines in the builder's composition root; count of framework registration calls |
| Evidence retrieval cost | requests and distinct services for R6 |
| Idle footprint (optional) | RSS of the binary after boot with the structural profile, on the run hardware |

Counts are compared run over run at their tags. No score is computed; the table is the result.

## 8. The run record

Filed by the scorer as `docs/contributing/golden-edge-agent-runs/<tag>.md`: tag, date, hardware,
model endpoint used, the R1–R10 table, the seven measures, the verified friction log, and a link to the app source at
the commit built. One paragraph of what changed since the previous run, and nothing else — no recommendations. What
to do about the log is design work for the architect, filed as issues with milestones.

## 9. The builder's brief (verbatim spawn prompt)

> You are a developer outside the SemStreams project. You have never opened this repository before today. Build a
> small edge triage app on SemStreams at tag `<TAG>` in a new Go module outside the repository, using only the
> project's README, its `docs/` directory, Go doc comments, whatever those documents link to, and the running
> binary's own surfaces. Do not open `cmd/`, `examples/`, `configs/`, `test/`, `scripts/`, `taskfiles/`,
> `openspec/`, or any other repository unless a doc links you there. If you must, log it as a violation with the
> reason and keep going; do not stop the run. Requirements R1–R10 are attached. Keep the friction log in §6's shape
> as you go, one row per time you needed something the docs did not give you, with minutes lost. Do not fix the
> framework or its docs; record and route around. Stop when R1–R9 are each MET, MET WITH FRICTION, or NOT MET with
> evidence, or after eight hours, whichever first. Report the log, the app source, and the R-table.

## 10. Out of scope, and the later option

Not a model-quality benchmark. Not a test of any sister. Not a product. Does not land in-tree until it is built on
the composition primitive that #1301 designs; until then it lives out-of-tree, pinned.

Later option, not scheduled (owner 2026-09-14): once the loop has a wire tool-provider seam (#1263), a variant of
this golden swaps `set_station_mode`'s neighbour for SemSource over MCP and measures the seam. Not part of run 1.

## 11. Changing this spec

The spec is versioned in its header. A change to a requirement's behavior invalidates comparison with earlier runs
and says so in the next run record. Adding a measurement does not.

## 12. Proposed NEAR supplement N1

Draft pending owner adoption. NEAR means Necessity, Evidence, Authority, and Recovery. #1315 owns the run;
this supplement adds observations to the same worked application, not another audit or a release gate.

### Scheduling and comparison

The prerequisite correction follows the [owner-transcribed sequence](https://github.com/C360Studio/semstreams/issues/1362#issuecomment-5797928199)
\#1362 → #1301 → tag. [PR #1159 closed without merge](https://github.com/C360Studio/semstreams/pull/1159#issuecomment-5733410892).
The coordinator verifies landed prerequisites and supplies an eligible published tag. This post-tag run cannot
be a condition for publishing that same tag. Existing milestone/RC rerun cadence remains.

Record behavioral version 1, protocol P2, and supplement N1 separately. P2 excludes the new planning corpus and
this section from builder reading; give the builder the unchanged §9 brief and permitted baseline instructions.
The R1–R10 requirements, statuses, and seven measures are unchanged. Compare across different knowledge
boundaries only with an explicit qualification; never claim unqualified comparability or hide prior exposure.
Seal the baseline transcript, source commit, timings, and results before supplemental preparation or debrief.
Keep the original continuous builder clock; no composition, evidence retrieval, or repair work is transferred
outside it to improve the baseline. Record pre-run environment setup separately. Do not modify the sealed app.

### Roles, limits, and reference facts

The existing scorer coordinates this supplement. The operator is a human who did not build the application and
has not read its transcript, audit planning/review corpus, prior results, or the scorer's reference sheet.
Record relevant prior experience. No substitute agent produces a human-usability result; absent a human, NOT RUN.

Proposed additional ceiling: two hours total, comprising 30 minutes preparation, 60 minutes operator time, and
30 minutes scoring. The baseline builder ceiling remains eight hours. Record actual time for each phase.
Before execution, record available endpoint, maximum provider calls, and any monetary/resource cap. Prefer the
existing local reference configuration. Stop at a cap, loss of isolation, or a provably wedged run; log the reason
and remaining cases as NOT RUN. Paid/resource-intensive runs require authoritative progress checks every 30–60s.
Preparation uses existing documented surfaces and fresh test data in the same built application. No framework,
application, or documentation repair; no new harness. An unavailable case is NOT RUN with its missing prerequisite.

Before showing cases, the scorer timestamps and saves a reference sheet: task purpose; deterministic/model steps;
named accountable role; permitted station/mode changes and evidence requirements; test inputs and observable facts;
expected human choices; expected runtime observations; and facts that remain unknown. Cite source or record IDs.
Use the example's declared policy; do not invent framework authorization, spending enforcement, rollback, or retry
guarantees. If the expected result cannot be justified, label that reference UNRESOLVED before observing the human.
Keep the answer sheet private until responses are recorded. Never rewrite it afterward to make an answer correct.

Give the operator only the neutral task, declared policy/role, documented public entry points, and their allowed
actions. They may use the example's existing user guidance and linked public guides. They may not inspect source,
issues, planning/review documents, run records, the builder transcript, or the reference sheet. Log every requested
hint and document lookup; retain the unassisted answer before offering help. Stop each case after 15 minutes.

Operator brief:

> You are responsible for the supplied station task within the stated policy. Inspect the proposed action and
> its evidence. Explain why model judgment is involved, what you may authorize, and what remains uncertain.
> Approve, reject, or leave the action undecided, stating your reason. Inspect the resulting state and evidence.
> After an interruption, identify what happened and the next documented action you can take. Say “unknown” where
> the available record cannot establish an answer. Ask for help if needed; we will record the difficulty.

### Four cases and four questions

Use isolated cases with predeclared facts. These are supplemental observations, never new R1–R10 pass conditions.

| Case | Reference and observation |
|---|---|
| Supported action | A policy-permitted call with its required evidence; observe approval and recorded result. |
| Disallowed action | A call outside the declared policy; observe rejection and whether an effect occurred. |
| Insufficient evidence | Missing input/basis through existing surfaces; observe whether uncertainty is recognized. |
| Process replacement | A pending approval across replacement; inspect retained identity, evidence, and next action. |

Do not delete retained evidence to manufacture the insufficient-evidence case or claim its absence from a failed
read. The replacement case does not presume restored deadlines, safe blind retry, or automatic reconciliation.
If a case cannot be prepared within the bounds, record NOT RUN; do not implement the missing behavior.

| NEAR question | Observation |
|---|---|
| Necessity | Identify deterministic steps, model contribution, and the check on its output; “no model needed” is valid. |
| Evidence | Locate basis, decision, and result; distinguish reported results, observed effects, and unknowns. |
| Authority | Identify accountable role, permitted action, and approval scope; recognize missing enforcement. |
| Recovery | Identify retained outcome and next documented action; recognize when a responsible person must intervene. |

Record the builder's Necessity explanation only after sealing the baseline, within supplemental preparation time.
Do not require building a second deterministic implementation or reward use of a model for its own sake.

### Evidence and disposition

Append an N1 section to the existing tag run record: versions, application commit, reference sheet, operator brief,
transcript, case results, provenance links, phase durations, resource use, and limitations. Publish the reference
sheet after the exercise. Record human answer MATCH / MISMATCH / UNRESOLVED / NOT RUN against the frozen reference,
separately from runtime outcome OBSERVED AS EXPECTED / OBSERVED DIFFERENCE / UNKNOWN / NOT RUN. An accurate answer
of “unknown” can MATCH. No response, an inaccessible record, or an omitted case is never evidence of success.

For each case count document opens, requests for assistance, approval prompts, retrieval requests and distinct
services; include repeats and elapsed time to the unassisted answer. Use NA with a reason where not measurable.
Retain operator words and observed state with timestamps and IDs. Record unsupported assumptions and contradictory
facts. Use counts and evidence, not an aggregate NEAR score. These measurements do not enter the baseline totals.

The run record remains observations without recommendations. Owner disposition follows separately on #1315 or
properly placed follow-up issues: existing-contract defect, documentation/usability improvement, product-owned
need, or accepted/deferred limitation. A finding neither authorizes a fix nor automatically becomes a v1 blocker.
ADR-106 and the release-candidate-proof contract remain authoritative; this exercise does not certify all surfaces.
