# Golden: the edge triage agent, built blind

Version 1 — written 2026-09-14 at owner direction. Tracked by #1306. Status: READY, not yet run.

This is a measurement instrument, not a product. It is rerun by a fresh agent at every milestone tag and release
candidate. The metric is how far an outside developer gets on the agentic path from the documentation alone, and
what the framework does when the box loses its uplink. Requirements are stated as behavior so they survive API churn;
the friction log is the delta between runs.

## 1. When it runs

Preconditions for run 1, all three:

1. PR #1159 (agentic-loop restart, stacked on #1156) has squashed to main.
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
