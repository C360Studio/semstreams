package scenarios

import (
	"log"
	"os"
	"time"
)

// globalSearchTimeoutEnv optionally overrides the client-side patience for
// globalSearch calls on the semantic tier (the GraphQL gateway gate, the
// known-answer gate, and the B0 thematic eval). It exists for the HEAVY LOCAL
// qwen3-8b measurement
// (task e2e:semantic:8b): an 8B answer-synthesis round-trip legitimately runs
// tens of seconds — far longer than the 1.7b/CI default budget — so the client
// must wait longer or it will time out before synthesis completes and abort the
// run before the baseline is recorded.
//
// UNSET uses each call site's explicit default (60s for the three globalSearch
// gates). Set it (e.g. "150s") to replace that default for the 8B run.
const globalSearchTimeoutEnv = "SEMSTREAMS_E2E_GLOBALSEARCH_TIMEOUT"

// globalSearchClientTimeout returns the env override when set and parseable,
// otherwise the caller's default. A malformed value is logged and ignored
// (fail-safe to the default) rather than aborting the scenario.
func globalSearchClientTimeout(def time.Duration) time.Duration {
	return durationEnvOr(globalSearchTimeoutEnv, def)
}

// llmEnhancementWaitEnv optionally raises the ceiling on how long the semantic
// tier waits for LLM community summaries to populate before the stages that
// consume them (notably B0). Sized for the fast qwen3-1.7b summary tier by
// default (2m); the qwen3-8b measurement run sets it higher so B0 synthesizes
// over fully-populated Tier-2 summaries. UNSET leaves the caller's default.
const llmEnhancementWaitEnv = "SEMSTREAMS_E2E_LLM_ENHANCEMENT_WAIT"

// llmEnhancementWait returns the env override when set and parseable, else def.
func llmEnhancementWait(def time.Duration) time.Duration {
	return durationEnvOr(llmEnhancementWaitEnv, def)
}

// durationEnvOr parses a Go-duration env var, returning def when unset or
// malformed (malformed is logged and ignored — fail-safe to the default).
func durationEnvOr(env string, def time.Duration) time.Duration {
	raw := os.Getenv(env)
	if raw == "" {
		return def
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		log.Printf("[e2e] ignoring invalid %s=%q, using default %s", env, raw, def)
		return def
	}
	return d
}
