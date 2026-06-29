package researchassess

import (
	"fmt"
	"sort"
	"strings"

	"github.com/c360studio/semstreams/agentic/research"
	"github.com/c360studio/semstreams/pkg/fusion"
)

// buildSystemPrompt returns the static system message that frames the
// assessor's role + decision criteria + output contract. Stable across
// requests so per-loop prompt changes don't ripple into a system-
// message change (and any per-tenant cache stays warm).
//
// Discipline: every guidance sentence carries a "when to use" criterion
// per feedback_persona_prose_needs_decision_criteria. No magic-number
// thresholds (no "if score < 0.6 → refine"); decision-axis framing
// instead so the model picks via reasoning, not lookup. Concrete
// examples for the two non-obvious failure shapes (looks-adequate-
// but-isn't, looks-sparse-but-is). Negative-shape for Sufficient=true
// so the model doesn't default to it when uncertain.

// SystemPromptMarker is the first sentence of buildSystemPrompt's
// output, exported for the e2e mock LLM marker-matching. See
// processor/research-graph-route/prompt.go SystemPromptMarker for
// the full rationale.
const SystemPromptMarker = "You are the sufficiency-assessment stage of a graph-search pipeline"

func buildSystemPrompt() string {
	return SystemPromptMarker + `. You receive a research topic and the upstream evidence the retrieval stages surfaced. You decide ONE thing: is the evidence enough to write a useful answer, or does the chain need to refine and retrieve more?

You do not synthesize the answer. You only judge whether one CAN be written from what's present.

# The two decisions

## sufficient: true
Use when the evidence array carries enough material to ground a useful answer to the topic. The downstream synthesizer will quote the evidence directly — your standard is "would a careful writer have enough here to write an answer without inventing facts?"

Decision axis: do the evidence items COVER the topic's named actors, time scope, and asked-about properties? If yes, sufficient.

Negative shape — do NOT default to sufficient=true when uncertain. It short-circuits the refine loop. If the evidence is thin (one entity when the topic asks about many; one tier when the topic spans structures the other tier should have caught), and you can name a SPECIFIC gap a refined query would close, route to sufficient=false.

Example of looks-adequate-but-isn't: topic is "drone-001 status across the fleet"; evidence contains three rich entries about drone-001 alone. The fleet view is missing — refine.

## sufficient: false (refine)
Use when there is a specific gap a sharper sub-query would close. Emit "refined_queries" — short natural-language descriptions of what to fetch next. The backend constructs typed sub-queries from your descriptions; do not emit typed sub-query objects yourself.

Decision axis: can you name 1-5 specific MISSING pieces? If yes, refine and list them. If you cannot name what is missing, route to sufficient=true even on thin evidence — the refine loop cannot work blind.

Example of looks-sparse-but-is: topic is "what is the warmest temperature the engine reached last hour"; evidence is one entity with one snippet quoting the peak temperature. That is sufficient — even though it is one item, it directly answers the question.

# How to write refined_queries

Each query is a short natural-language phrase (5-20 words) describing what to look for, NOT a typed sub-query, NOT an entity ID, NOT a predicate name. The backend translates your descriptions to typed dispatches.

Good: "battery alert frequency for drone-001 in the last 7 days"
Good: "maintenance events on the fleet that paged operations"
Bad: "{entity_state: drone-001}"  (typed sub-query — backend's job)
Bad: "acme.ops.robotics.gcs.drone.001"  (entity ID — backend's job)

Keep the list focused — 1 to 5 queries is the productive range. More than 5 suggests the refine loop should be split across multiple iterations, not packed into one pass.

# Output contract

Emit a single JSON object, no prose, no markdown fences, with this shape:

` + "```" + `
{
  "sufficient": true,
  "refined_queries": [ "<short description>", ... ],
  "rationale": "one or two sentences naming the structural reason for the decision",
  "confidence": 0.0
}
` + "```" + `

Field notes:
- "sufficient": true or false (boolean).
- "refined_queries": list of short descriptions. Omit the field or pass an empty list when sufficient is true. Keep to 1-5 entries.
- "rationale": one or two sentences for operator trajectory review. Name the structural reason — "evidence covers maintenance window but topic asks about fleet-wide status" beats "the data looks ok". The downstream chain branches on sufficient only.
- "confidence": a number between 0.0 and 1.0 describing your own conviction in the decision. Trajectory review uses it to spot prompts that flip flags with low conviction.`
}

// buildUserPrompt renders the per-request context the assessor sees:
// the topic + hints from the Intent, the routing action that drove
// retrieval, the evidence array (bounded by maxEvidenceInPrompt). The
// degraded flag from upstream is passed through so the model can
// weigh decision conviction against retrieval health.
//
// Discipline: evidence renders with stable ordering (score
// descending; ties broken by entity ID) so the same loop replayed
// produces byte-identical prompts. SnippetText is truncated per
// maxSnippetCharsInPrompt so individual long snippets don't blow the
// prompt budget.
func buildUserPrompt(intent *research.Intent, exec *research.ExecutionOutput, maxEvidenceInPrompt, maxSnippetCharsInPrompt int) string {
	var sb strings.Builder

	fmt.Fprintf(&sb, "Topic: %s\n", intent.Topic)

	if len(intent.Hints) > 0 {
		sb.WriteString("\nUpstream hints:\n")
		keys := make([]string, 0, len(intent.Hints))
		for k := range intent.Hints {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(&sb, "  - %s: %s\n", k, intent.Hints[k])
		}
	}

	if exec.Action != "" {
		fmt.Fprintf(&sb, "\nRouting action that drove retrieval: %s\n", exec.Action)
	}
	fmt.Fprintf(&sb, "Sub-queries dispatched: %d\n", exec.SubQueryCount)
	fmt.Fprintf(&sb, "Tokens used by retrieval: %d (intent budget %d)\n", exec.BudgetTokensUsed, intent.ResolvedBudgetTokens())
	if exec.Degraded {
		fmt.Fprintf(&sb, "Retrieval degraded: yes (%s)\n", exec.DegradedReason)
		sb.WriteString("Treat the evidence as partial; weigh refine more heavily when in doubt.\n")
	}

	sb.WriteString("\nEvidence (top by score; tier 0 = predicate/walk, tier 1 = BM25):\n")
	evidence := orderedEvidence(exec.Evidence, maxEvidenceInPrompt)
	if len(evidence) == 0 {
		sb.WriteString("  (none — retrieval surfaced no evidence)\n")
	}
	for i, e := range evidence {
		fmt.Fprintf(&sb, "  [%d] %s", i, e.EntityID)
		if e.Tier != "" {
			fmt.Fprintf(&sb, " (tier=%s", e.Tier)
			if e.Source != "" {
				fmt.Fprintf(&sb, " src=%s", e.Source)
			}
			if e.Score > 0 {
				fmt.Fprintf(&sb, " score=%.2f", e.Score)
			}
			sb.WriteString(")")
		}
		if e.SnippetText != "" {
			snippet := e.SnippetText
			if maxSnippetCharsInPrompt > 0 && len(snippet) > maxSnippetCharsInPrompt {
				snippet = snippet[:maxSnippetCharsInPrompt] + "..."
			}
			fmt.Fprintf(&sb, "\n      snippet: %s", snippet)
		}
		sb.WriteString("\n")
	}

	if total := len(exec.Evidence); total > len(evidence) {
		fmt.Fprintf(&sb, "  (... %d more evidence items not shown)\n", total-len(evidence))
	}

	sb.WriteString("\nDecide sufficient or refine and emit the JSON object per the system prompt's output contract.")
	return sb.String()
}

// orderedEvidence returns the top-N evidence items in the canonical
// chain ordering: tier ascending (tier 0 authoritative first), score
// descending within a tier, EntityID ascending for tie-break. Matches
// processor/research-graph-execute/handler.go's sortEvidence so the
// assessor sees evidence in the same order downstream synthesize
// will quote it back in. Replay-determinism: same input list always
// produces a byte-identical prompt.
func orderedEvidence(in []fusion.Evidence, limit int) []fusion.Evidence {
	if limit <= 0 || len(in) == 0 {
		return nil
	}
	out := make([]fusion.Evidence, len(in))
	copy(out, in)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Tier != out[j].Tier {
			return out[i].Tier < out[j].Tier
		}
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].EntityID < out[j].EntityID
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}
