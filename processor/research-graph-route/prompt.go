package researchroute

import (
	"fmt"
	"sort"
	"strings"

	"github.com/c360studio/semstreams/agentic/research"
)

// buildSystemPrompt returns the static system message that frames the
// router's role + decision criteria + output contract. Stable across
// requests so a per-loop prompt change doesn't ripple into a system-
// message change (and any per-tenant cache stays warm).
//
// Discipline: every action sentence carries a "when to use" criterion
// per feedback_persona_prose_needs_decision_criteria. No magic-number
// thresholds (no "confidence < 0.6 → retighten"); decision-axis
// framing instead so the model picks via reasoning, not lookup.
// Concrete examples for non-obvious choice points
// (decompose vs walk_seeds vs retighten can look similar from
// classifier output alone). Negative-shape for synthesize_directly
// so the model doesn't default to it when uncertain.
func buildSystemPrompt() string {
	return `You are the routing stage of a graph-search pipeline. You receive a research topic and the upstream classifier's initial candidate set. You choose ONE of four next actions and emit a structured JSON object.

Your job is the structural decision — what shape of work comes next. The downstream stages do the work. You do not synthesize, walk, or refine yourself; you only route.

# The four actions

## synthesize_directly
Use when the candidate set already answers the topic well enough that further graph work would only restate what the classifier surfaced.

Decision axis: would running multi-hop expansion or re-classification produce evidence the synthesizer doesn't already have? If no, route here.

Negative shape — do NOT default to synthesize_directly when uncertain. It is the route that bypasses the rest of the chain. Pick it when the candidate set is already sufficient, not when you cannot tell which other action fits.

## retighten
Use when the topic and the candidates obviously don't match — the classifier surfaced the wrong slice of the graph and rerunning with refined hints should land closer.

Decision axis: is the gap a TOPIC PHRASING problem (vague topic, ambiguous term, missing entity-type or temporal qualifier) that a better topic + hints would fix? If yes, retighten with a refined topic.

Example: topic "sensor failures" returned entities about sensor calibration sessions because "failure" wasn't disambiguated. Retighten with topic "sensor hardware failures during operation" and hint {entity_type: "fault_event"}.

Do NOT retighten when the candidates look right but you need more graph context — that's walk_seeds. Capped at 2 retighten passes per chain, so a second retighten should only fire if the first retighten genuinely changed the candidate shape.

## walk_seeds
Use when one or more candidates look like the right starting points but you need their NEIGHBORHOOD — observations, related entities, temporal context — to answer the topic.

Decision axis: is the answer reachable by traversing OUT from concrete seeds in the candidate set? If yes, walk those seeds.

Reference seeds by their display name (e.g. "drone-001"), a partial federated ID (e.g. "robotics.gcs.drone"), or by their position in the candidate list (e.g. "0" for the first candidate). The backend resolves these to full 6-part entity IDs. Do not emit full 6-part IDs yourself — you will not spell them reliably.

## decompose
Use when the topic spans MULTIPLE structural dimensions that won't be answered by walking a single seed — different entity kinds, multiple time windows, different predicate families, multiple spatial regions.

Decision axis: would the answer require fusing results from STRUCTURALLY DIFFERENT sub-queries? If yes, decompose.

Emit the decomposition INTENT (axes to decompose along, focus concept, scope). The backend constructs the typed sub-queries from templates. Do not emit typed sub-query objects yourself — you will not spell them reliably.

# How to choose between similar-looking actions

- walk_seeds vs decompose: walk_seeds is "go deeper from these starting points"; decompose is "answer this in pieces that fuse together." If the candidate set has a small number of clear starting points, walk_seeds. If the topic itself has multiple structural pieces regardless of which entities the classifier found, decompose.

- retighten vs decompose: retighten when the classifier found the wrong neighborhood; decompose when the classifier found a piece of the right neighborhood but the answer needs more pieces.

- retighten vs synthesize_directly: if the candidate set is sparse or off-topic, retighten is almost always preferable to synthesize_directly. The synthesizer cannot invent evidence the classifier missed.

# Output contract

Emit a single JSON object, no prose, no markdown fences, with this shape:

` + "```" + `
{
  "action": "synthesize_directly" | "retighten" | "walk_seeds" | "decompose",
  "args": { ... per-action shape below ... },
  "rationale": "one or two sentences explaining the structural reason for this action"
}
` + "```" + `

Per-action args:

- synthesize_directly: ` + "`" + `{}` + "`" + ` (empty)
- retighten: ` + "`" + `{ "topic": "<refined topic string>", "hints": { "<key>": "<value>", ... } }` + "`" + `
- walk_seeds: ` + "`" + `{ "seeds": [ { "ref": "<name | partial_id | candidate_index>", "ref_type": "name" | "partial_id" | "candidate_index" }, ... ] }` + "`" + `
- decompose: ` + "`" + `{ "axes": ["<dimension>", ...], "focus": "<central concept>", "scope": "narrow" | "medium" | "broad" }` + "`" + `

Rationale is for operator trajectory review — write the structural reason, not a restatement of the topic. The downstream chain does not pattern-match on rationale; it branches on action only.`
}

// buildUserPrompt renders the per-request context the router sees:
// the topic + hints from the Intent, the classifier's tier +
// confidence + candidates. Bounded by maxCandidatesInPrompt so the
// prompt stays within small-model context windows.
//
// Discipline: candidates render with stable ordering (relevance
// descending; ties broken by entity ID) so the same loop replayed
// produces byte-identical prompts. Stable ordering also lets the
// model's "candidate_index" seed references resolve deterministically
// at the backend.
func buildUserPrompt(intent *research.Intent, classifierOut *research.ClassifierOutput, maxCandidatesInPrompt int) string {
	var sb strings.Builder

	fmt.Fprintf(&sb, "Topic: %s\n", intent.Topic)

	if len(intent.Hints) > 0 {
		sb.WriteString("\nUpstream hints:\n")
		// Sort hint keys for determinism.
		keys := make([]string, 0, len(intent.Hints))
		for k := range intent.Hints {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(&sb, "  - %s: %s\n", k, intent.Hints[k])
		}
	}

	fmt.Fprintf(&sb, "\nClassifier tier: %s\n", classifierOut.Tier)
	if classifierOut.Confidence > 0 {
		fmt.Fprintf(&sb, "Classifier confidence: %.2f\n", classifierOut.Confidence)
	}
	if classifierOut.Intent != "" {
		fmt.Fprintf(&sb, "Classifier intent label: %s\n", classifierOut.Intent)
	}
	if classifierOut.Degraded {
		fmt.Fprintf(&sb, "Classifier degraded: yes (%s)\n", classifierOut.DegradedReason)
	}

	if len(classifierOut.Hints) > 0 {
		sb.WriteString("\nClassifier hints:\n")
		keys := make([]string, 0, len(classifierOut.Hints))
		for k := range classifierOut.Hints {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(&sb, "  - %s: %v\n", k, classifierOut.Hints[k])
		}
	}

	sb.WriteString("\nClassifier candidates (top by relevance, indexed for walk_seeds):\n")
	candidates := orderedCandidates(classifierOut.Candidates, maxCandidatesInPrompt)
	if len(candidates) == 0 {
		sb.WriteString("  (none — consider retighten or synthesize_directly per rationale)\n")
	}
	for i, c := range candidates {
		fmt.Fprintf(&sb, "  [%d] %s", i, c.EntityID)
		if c.Label != "" {
			fmt.Fprintf(&sb, " — %s", c.Label)
		}
		if c.Type != "" {
			fmt.Fprintf(&sb, " (type=%s)", c.Type)
		}
		if c.Relevance > 0 {
			fmt.Fprintf(&sb, " [relevance=%.2f]", c.Relevance)
		}
		if c.Tier != "" {
			fmt.Fprintf(&sb, " [tier=%s]", c.Tier)
		}
		sb.WriteString("\n")
	}

	sb.WriteString("\nChoose ONE action and emit the JSON object per the system prompt's output contract.")
	return sb.String()
}

// orderedCandidates returns the top-N candidates by relevance
// descending. Ties break by EntityID ascending so the same input
// list produces a byte-identical prompt across runs (important for
// "candidate_index" reference stability).
func orderedCandidates(in []research.Candidate, limit int) []research.Candidate {
	if limit <= 0 || len(in) == 0 {
		return nil
	}
	out := make([]research.Candidate, len(in))
	copy(out, in)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Relevance != out[j].Relevance {
			return out[i].Relevance > out[j].Relevance
		}
		return out[i].EntityID < out[j].EntityID
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}
