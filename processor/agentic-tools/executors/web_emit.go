package executors

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// webEmitFailuresTotal counts per-result triple emission failures from
// the web_search and http_request executors, labelled by tool and
// failure class. Operators alert on rate-of-change here: a non-zero
// entity_id rate signals malformed URLs the LLM is producing (worth
// surfacing to persona tuning); a non-zero publish rate signals
// graph-ingest / NATS infrastructure trouble (worth paging).
//
// Reasons are a closed set:
//   - "entity_id" — TryWebObservationEntityID rejected the URL
//   - "publish"   — TriplePublisher.AddTriple returned an error
//
// Per the 2026-05-11 design decision, both tools log+counter+continue
// rather than failing the tool — graph emission is additive observation,
// not the primary contract.
var webEmitFailuresTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Namespace: "agentic_tool_web",
		Name:      "emit_failures_total",
		Help:      "Number of per-result triple emission failures from web_search/http_request, labelled by tool and reason. Non-zero entity_id rate suggests malformed URLs (persona tuning); non-zero publish rate suggests graph-ingest infra (page).",
	},
	[]string{"tool", "reason"},
)
