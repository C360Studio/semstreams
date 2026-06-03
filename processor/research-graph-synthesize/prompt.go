package researchsynthesize

import (
	"fmt"
	"sort"
	"strings"

	"github.com/c360studio/semstreams/agentic/research"
)

// buildSystemPrompt returns the static system message that frames the
// synthesizer's role + the no-fabrication contract + output shape.
// Stable across requests so per-loop prompt changes don't ripple
// into a system-message change.
//
// Discipline: per-section purpose framing per
// feedback_persona_prose_needs_decision_criteria. The no-fabrication
// section uses concrete examples (good ref vs invented ref). The
// output contract spells out the exact JSON shape so the LLM does
// not need to guess.

// SystemPromptMarker is the first sentence of buildSystemPrompt's
// output, exported for the e2e mock LLM marker-matching. See
// processor/research-graph-route/prompt.go SystemPromptMarker for
// the full rationale.
const SystemPromptMarker = "You are the synthesis stage of a graph-search pipeline"

func buildSystemPrompt() string {
	return SystemPromptMarker + `. You receive a research topic and the upstream evidence the retrieval and assessment stages surfaced. You write the answer: a natural-language synthesis grounded in the evidence, with a list of the evidence items you used.

You do not invent facts. You do not invent entity IDs, ObjectStore refs, or sources. If the evidence does not support an answer to the topic, say so plainly in the synthesis.

# Quote-back contract (load-bearing)

Every evidence reference you emit in "evidence_refs" MUST be either an EntityID or an ObjectStoreRef that appears EXACTLY in the input evidence array. The backend strips any reference that does not match and surfaces drift to operator review. Repeated drift suggests the prompt is being ignored — operators iterate the prompt, not bypass the validator.

Good: refer to entities by the exact EntityID strings in the evidence (e.g. "acme.ops.robotics.gcs.drone.001").
Good: refer to ObjectStore-backed content by the exact ObjectStoreRef string (e.g. "objstore://research/evidence-014.txt").
Bad: invent shorter IDs or rewrite IDs for readability ("drone-001" is wrong if the evidence carries "acme.ops.robotics.gcs.drone.001"; use the long form).
Bad: cite a source you can name from your training data but did not appear in the evidence.

# Writing the synthesis

Length: as long as the topic needs; do not pad. A 2-sentence answer to a focused topic is preferable to a 5-paragraph answer that restates the evidence in different words.

Structure: lead with the direct answer. Follow with the evidence that supports it. Close with caveats — gaps in the evidence, degraded retrieval flags, conflicting signals.

When evidence is thin: say so. "The evidence shows X but does not cover Y" is a useful answer; making up Y is not.

When evidence is empty: produce a short "no evidence available for this topic" synthesis and emit evidence_refs: []. Do NOT invent.

# Output contract

Emit a single JSON object, no prose, no markdown fences, with this shape:

` + "```" + `
{
  "synthesis": "<the natural-language answer; refer to evidence by EntityID or ObjectStoreRef strings exactly as they appear in the input>",
  "evidence_refs": [
    "<EntityID or ObjectStoreRef from input>",
    ...
  ]
}
` + "```" + `

Only emit evidence_refs you actually drew from in writing synthesis. The backend echoes those evidence items back to the caller as citations; padding the list with refs you did not use produces noisy citations for downstream consumers.`
}

// buildUserPrompt renders the per-request context the synthesizer
// sees. Evidence is rendered with stable ordering (Score descending;
// ties broken by EntityID) so the same loop replayed produces
// byte-identical prompts. SnippetText is truncated per
// maxSnippetCharsInPrompt.
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

	if exec.Degraded {
		fmt.Fprintf(&sb, "\nRetrieval degraded: yes (%s)\n", exec.DegradedReason)
		sb.WriteString("Treat the evidence as partial; surface gaps in the synthesis.\n")
	}

	sb.WriteString("\nEvidence (top by score; refer back to EntityID / ObjectStoreRef strings exactly):\n")
	evidence := orderedEvidence(exec.Evidence, maxEvidenceInPrompt)
	if len(evidence) == 0 {
		sb.WriteString("  (none — produce a no-evidence synthesis per the contract)\n")
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
		if e.ObjectStoreRef != "" {
			fmt.Fprintf(&sb, " obj=%s", e.ObjectStoreRef)
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

	sb.WriteString("\nWrite the synthesis and emit the JSON object per the system prompt's output contract.")
	return sb.String()
}

// orderedEvidence returns the top-N evidence items in the canonical
// chain ordering: tier ascending (tier 0 authoritative first), score
// descending within a tier, EntityID ascending for tie-break. Matches
// processor/research-graph-execute/handler.go's sortEvidence so the
// prompt-time ordering and the SearchResult.Evidence echo ordering
// stay in lockstep (operator trajectory replay reads the same shape
// at both ends).
func orderedEvidence(in []research.Evidence, limit int) []research.Evidence {
	if limit <= 0 || len(in) == 0 {
		return nil
	}
	out := make([]research.Evidence, len(in))
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
