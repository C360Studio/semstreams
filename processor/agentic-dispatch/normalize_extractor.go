package agenticdispatch

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/c360studio/semstreams/agentic"
	operatingmodel "github.com/c360studio/semstreams/agentic/operating-model"
	"github.com/c360studio/semstreams/model"
	agenticmodel "github.com/c360studio/semstreams/processor/agentic-model"
)

// LayerNormalizer is the function-shaped contract for turning a user's
// freeform onboarding answer into structured operating-model entries. The
// component exposes it as a field so tests can inject a deterministic
// normalizer without spinning up an LLM.
//
// A normalizer that returns no entries (or any non-nil error) signals the
// caller to fall back to the deterministic stub.
type LayerNormalizer func(ctx context.Context, layer, answer string) ([]operatingmodel.Entry, error)

// extractionMaxTokens caps the per-call output budget. 1024 tokens is enough
// for a JSON envelope of 5–10 entries with all optional fields populated and
// leaves headroom on small-context endpoints (the typical Ollama default).
const extractionMaxTokens = 1024

// extractionTimeoutDefault is the wall-clock cap for a normalization call.
// ADR-024's precedence chain still applies — task-level Timeout (set on the
// AgentRequest below) overrides endpoint and capability defaults.
const extractionTimeoutDefault = 30 * time.Second

// extractionModelName is the capability/endpoint name used to resolve a
// model for normalization. The registry resolves it through the standard
// fallback chain (configured name → fallback chain → default), so a
// deployment that registers a "default" capability (the convention) gets
// normalization for free without a dispatch config knob.
const extractionModelName = "default"

// normalizeLayerAnswerWithLLM is the production normalizer. It builds a
// per-layer prompt, calls the wired model endpoint, and parses the JSON
// response into []Entry. Returns (nil, err) on any model-call failure so
// callers fall back to the deterministic stub.
func (c *Component) normalizeLayerAnswerWithLLM(ctx context.Context, layer, answer string) ([]operatingmodel.Entry, error) {
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return nil, nil
	}
	if c.modelRegistry == nil {
		return nil, fmt.Errorf("modelRegistry not wired")
	}

	ep := resolveModelEndpoint(c.modelRegistry, extractionModelName)
	if ep == nil {
		return nil, fmt.Errorf("no endpoint resolved for normalization")
	}

	client, err := agenticmodel.NewClient(ep)
	if err != nil {
		return nil, fmt.Errorf("model client: %w", err)
	}
	client.SetAdapter(agenticmodel.AdapterFor(ep.Provider))
	client.SetLogger(c.logger)

	reqCtx, cancel := context.WithTimeout(ctx, extractionTimeoutDefault)
	defer cancel()

	resp, err := client.ChatCompletion(reqCtx, agentic.AgentRequest{
		RequestID:   fmt.Sprintf("normalize_%d", time.Now().UnixNano()),
		Role:        "onboarding_extractor",
		Model:       extractionModelName,
		Messages:    buildNormalizationMessages(layer, answer),
		MaxTokens:   extractionMaxTokens,
		Temperature: 0.1,
		Timeout:     extractionTimeoutDefault.String(),
	})
	if err != nil {
		return nil, fmt.Errorf("chat completion: %w", err)
	}
	if resp.Status == agentic.StatusLengthTruncated {
		// A truncated response can leave malformed JSON. Fail fast so the
		// stub fallback path runs — better a coarse stub entry than half
		// of a structured array.
		return nil, fmt.Errorf("response truncated (finish_reason=length); rerun with larger budget")
	}

	entries, err := parseNormalizationResponse(resp.Message.Content, layer)
	if err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	return entries, nil
}

// resolveModelEndpoint mirrors the LLMIntentClassifier resolution chain:
// configured name → fallback chain → default. Returns nil if nothing
// resolves.
func resolveModelEndpoint(reg model.RegistryReader, name string) *model.EndpointConfig {
	if name == "" {
		name = "default"
	}
	if ep := reg.GetEndpoint(name); ep != nil {
		return ep
	}
	for _, n := range reg.GetFallbackChain(name) {
		if ep := reg.GetEndpoint(n); ep != nil {
			return ep
		}
	}
	if def := reg.GetDefault(); def != "" {
		return reg.GetEndpoint(def)
	}
	return nil
}

// normalizeLayerAnswer is the call-site entry point used by
// onboardingRecordAnswer. It tries the wired LayerNormalizer (LLM-backed in
// production, mock in tests, nil in unit-test harnesses) and falls back to
// the deterministic NormalizeLayerAnswer stub on any non-success outcome.
//
// Falls back when:
//   - normalizerFn is nil (no LLM wired)
//   - normalizerFn returns an error (timeout, parse failure, endpoint down)
//   - normalizerFn returns zero entries (nothing extracted)
//
// The fallback contract is intentionally generous: we'd rather ship a coarse
// stub entry than reject the user's answer because the LLM hiccupped.
func (c *Component) normalizeLayerAnswer(ctx context.Context, layer, answer string) []operatingmodel.Entry {
	if c.normalizerFn != nil {
		entries, err := c.normalizerFn(ctx, layer, answer)
		if err != nil {
			c.logger.WarnContext(ctx, "onboarding: LLM normalizer failed; falling back to stub",
				slog.String("layer", layer),
				slog.Any("error", err))
		} else if len(entries) > 0 {
			return finalizeExtractedEntries(layer, entries)
		}
	}
	return NormalizeLayerAnswer(layer, answer)
}

// finalizeExtractedEntries assigns EntryIDs to LLM-produced entries that
// don't already carry one, applies SourceConfidence/Status defaults, and
// drops any entry that fails Validate. The LLM is not trusted to produce
// perfect EntryIDs — we generate them deterministically so the entity-ID
// format invariant (no dots) is guaranteed.
func finalizeExtractedEntries(layer string, raw []operatingmodel.Entry) []operatingmodel.Entry {
	out := make([]operatingmodel.Entry, 0, len(raw))
	for _, e := range raw {
		e.EntryID = entryID(layer)
		e.Title = strings.TrimSpace(e.Title)
		e.Summary = strings.TrimSpace(e.Summary)
		if e.SourceConfidence == "" {
			e.SourceConfidence = operatingmodel.ConfidenceConfirmed
		}
		if e.Status == "" {
			e.Status = operatingmodel.StatusActive
		}
		if err := e.Validate(); err != nil {
			continue
		}
		out = append(out, e)
	}
	return out
}

// extractionEnvelope is the JSON shape the LLM is asked to produce. Wrapping
// the entries in a single root object is friendlier than a bare array for
// extractJSON-style "find first { … }" recovery from chatty model output.
type extractionEnvelope struct {
	Entries []operatingmodel.Entry `json:"entries"`
}

// parseNormalizationResponse extracts the structured envelope from a model
// response. Handles pure-JSON output and JSON wrapped in markdown / prose.
func parseNormalizationResponse(content, _ string) ([]operatingmodel.Entry, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, fmt.Errorf("empty response")
	}

	// Try direct parse first.
	var env extractionEnvelope
	if err := json.Unmarshal([]byte(content), &env); err == nil {
		return env.Entries, nil
	}

	// Fall back to extractJSON for prose-wrapped responses (uses the helper
	// already defined in intent_classifier.go).
	extracted := extractJSON(content)
	if extracted == "" {
		return nil, fmt.Errorf("no JSON object found in response")
	}
	if err := json.Unmarshal([]byte(extracted), &env); err != nil {
		return nil, fmt.Errorf("unmarshal envelope: %w", err)
	}
	return env.Entries, nil
}

// buildNormalizationMessages composes the system+user message pair sent to
// the model. The system prompt is layer-aware so the model knows which Entry
// fields matter most for the current layer.
func buildNormalizationMessages(layer, answer string) []agentic.ChatMessage {
	return []agentic.ChatMessage{
		{Role: "system", Content: normalizationSystemPrompt(layer)},
		{Role: "user", Content: answer},
	}
}

// normalizationSystemPrompt returns the per-layer system prompt. The base
// instruction is shared; each layer appends a focus hint about which Entry
// fields the user's answer is most likely to populate.
func normalizationSystemPrompt(layer string) string {
	var b strings.Builder
	b.WriteString(`You extract structured entries from a user's freeform answer about how they work.

Respond with ONLY a JSON object of this shape:
{"entries": [
  {"title": "<short label>", "summary": "<one-or-two sentence description>",
   "cadence": "<optional: weekly|daily|monthly|quarterly|...>",
   "trigger": "<optional: e.g. Monday 9am, end of sprint>",
   "inputs": ["<optional list>"],
   "stakeholders": ["<optional list of people or systems>"],
   "constraints": ["<optional list>"]}
]}

Rules:
- Produce one entry per distinct rhythm, decision, dependency, or piece of knowledge in the user's answer. Multiple entries are normal — do not collapse them.
- title is short and human (under 80 chars). summary is one or two sentences.
- Omit optional fields when the user did not say anything that fits — empty strings or empty arrays are not useful.
- Do not invent facts. If the user said "weekly planning", do not add a stakeholder you imagined.
- Output JSON only. No prose, no markdown fences.

`)
	b.WriteString("Layer focus: ")
	b.WriteString(layerFocusHint(layer))
	return b.String()
}

// layerFocusHint produces a short sentence telling the model which optional
// fields are most relevant for the current layer. Helps the model populate
// the right facets without hallucinating.
func layerFocusHint(layer string) string {
	switch layer {
	case operatingmodel.LayerOperatingRhythms:
		return "operating rhythms — cadence and trigger matter most. Recurring blocks, standups, deep-work windows."
	case operatingmodel.LayerRecurringDecisions:
		return "recurring decisions — cadence, trigger, and inputs matter most. Decisions made on a regular schedule."
	case operatingmodel.LayerDependencies:
		return "dependencies — stakeholders and inputs matter most. People, systems, or data the work relies on."
	case operatingmodel.LayerInstitutionalKnowledge:
		return "institutional knowledge — constraints matter most. Tribal knowledge that isn't written down anywhere."
	case operatingmodel.LayerFriction:
		return "friction — constraints and stakeholders matter most. Where work gets stuck and what blocks it."
	default:
		return "general — populate any optional fields that fit the user's answer."
	}
}
