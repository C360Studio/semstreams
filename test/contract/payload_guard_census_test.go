package contract

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The payload-bounds guard (natsclient's checkPayloadSize funnel) is only a
// guarantee if it is UNAVOIDABLE OR every avoidance is ENUMERATED. This census
// pins the enumeration: every non-test production Go file that holds a raw
// jetstream.KeyValue handle (typed field/param/var), acquires one directly
// (js.KeyValue(...)), or reaches for the raw core connection
// (GetConnection()) must appear on the allowlist below WITH a reason. A new
// file matching any pattern turns this test red, forcing the author to either
// use the guarded natsclient lanes (KVStore / Publish*) or consciously add an
// enumerated entry.
//
// History (payload-size-chokepoints, gh#857): the migrated write-lane
// bypasses were graph/clustering/summary_store.go, graph/embedding/storage.go
// (both now ride guarded KVStore lanes), and storage/objectstore's two raw
// core-NATS publishes (now Client.Publish). The design's original premise —
// "allowlist = natsclient internals only after the migrations" — did not
// survive the code: ~50 production files legitimately hold raw KV handles for
// reads, watches, deletes, and bucket provisioning, which carry no payload to
// guard. They are enumerated here with reasons instead.
//
// Reason vocabulary:
//   - "guard owner"        — natsclient itself; the funnel lives here.
//   - "read/watch"         — the raw handle serves Get/Watch/Keys/Status/
//     Delete only; no payload-bearing send rides it.
//   - "wraps guarded lane" — acquires the handle, then wraps it in
//     Client.NewKVStore; writes ride the guarded lane.
//   - "raw write lane"     — pre-existing raw writes, ENUMERATED (the census
//     answer to "unavoidable or enumerated"); migrating each to the guarded
//     lane is follow-up work, and removing one from this list requires the
//     migration, not a wave-through.
//   - "conn for subscribe/status" — GetConnection() used for subscriptions,
//     liveness checks, or request/reply plumbing; not a payload send lane.
//   - "core publish (ingest fast path)" — pre-existing raw core-NATS
//     publishes on input hot paths, ENUMERATED; oversize there dies with the
//     raw server error today.
var payloadGuardCensusAllowlist = map[string]string{
	// ---- natsclient: the guard owner --------------------------------------
	"natsclient/client.go":                  "guard owner",
	"natsclient/kv.go":                      "guard owner",
	"natsclient/kv_temporal.go":             "guard owner",
	"natsclient/kvspec.go":                  "guard owner",
	"natsclient/storage_report.go":          "guard owner",
	"natsclient/storage_report_consumer.go": "guard owner",
	"natsclient/test_client.go":             "guard owner (test infrastructure)",

	// ---- acquires + wraps in the guarded KVStore lane ---------------------
	"processor/agentic-loop/component.go":                            "wraps guarded lane (loopsStore = NewKVStore; raw handle only at acquisition)",
	"processor/graph-ingest/component.go":                            "wraps guarded lane for entity writes",
	"processor/graph-index/component.go":                             "wraps guarded lane for index writes",
	"config/manager.go":                                              "wraps guarded lane (config KV)",
	"flowstore/manager.go":                                           "wraps guarded lane (flow definitions KV)",
	"frameworkcapabilities/graphresearch/register_tool.go":           "wraps guarded lane (research-loop completion writes)",
	"processor/agentic-tools/executors/register_flow_monitor.go":     "wraps guarded lane (reader; KVStore for typed Get)",
	"processor/agentic-tools/executors/register_read_loop_result.go": "wraps guarded lane (reader; KVStore for typed Get)",
	"graph/clustering/summary_store.go":                              "wraps guarded lane (migrated in payload-size-chokepoints; raw handle only at constructor seam)",
	"graph/embedding/storage.go":                                     "wraps guarded lane for writes; raw handle for reads/watch/delete (migrated in payload-size-chokepoints)",

	// ---- read/watch-only raw handles --------------------------------------
	"processor/graph-query/community_cache.go":         "read/watch (community + summary read-join)",
	"processor/graph-query/component.go":               "read/watch (query surfaces)",
	"processor/graph-clustering/anomaly.go":            "read/watch (anomaly scan)",
	"processor/graph-clustering/component.go":          "read/watch (bucket acquisition; writes ride clustering storages)",
	"processor/graph-embedding/component.go":           "read/watch (bucket acquisition; writes ride embedding.Storage's guarded lanes)",
	"processor/agentic-tools/executors/graph_query.go": "read/watch (entity reads)",
	"processor/rule/processor.go":                      "read/watch (bucket acquisition for watchers)",
	"processor/rule/entity_watcher.go":                 "read/watch (entity KV watch)",
	"graph/kvcatalog.go":                               "read/watch (catalog descriptor/provisioning; no payload writes)",
	"graph/embedding/worker.go":                        "read/watch (pending-record watch)",
	"graph/clustering/enhancement_worker.go":           "read/watch (COMMUNITY_INDEX trigger watch; writes ride the summary store's guarded lane)",
	"graph/readiness/watcher.go":                       "read/watch (GRAPH_STATUS watch)",
	"graph/inference/review_worker.go":                 "read/watch (review queue watch)",
	"service/message_logger_kv_watch.go":               "read/watch (debug-only message logger)",
	"service/message_logger_http.go":                   "read/watch (debug-only message logger)",
	"service/storage_observability.go":                 "read/watch (bucket status observability)",
	"gateway/graph-gateway/component.go":               "read/watch (bucket acquisition for query reads)",
	"pkg/dispatch/completion_watcher.go":               "read/watch (completion KV watch)",
	"pkg/lifecycle/manager_query.go":                   "read/watch (history revision replay)",
	"pkg/graphview/view.go":                            "read/watch (KV-watch-driven view)",
	"pkg/retry/doc.go":                                 "documentation only (doc.go example text)",
	"processor/rule/kv_test_helpers.go":                "test-support helper file (in-memory fake for rule tests)",

	// ---- enumerated raw write lanes (follow-up migrations) ----------------
	"graph/clustering/storage.go":                 "raw write lane — carries its own gh#837 ErrMaxPayload permanent classification; guarded-lane migration is follow-up",
	"graph/inference/storage.go":                  "raw write lane — inference index writes; follow-up",
	"graph/structural/storage.go":                 "raw write lane — structural index writes; follow-up",
	"graph/query/client.go":                       "raw write lane — query-side KV plumbing; follow-up",
	"graph/readiness/publisher.go":                "raw write lane — small fixed-shape readiness envelopes; follow-up",
	"component/lifecycle_reporter.go":             "raw write lane — small fixed-shape lifecycle reports; follow-up",
	"processor/agentic-tools/store.go":            "raw write lane — scratchpad/tool KV; follow-up",
	"processor/rule/state_tracker.go":             "raw write lane — rule state tracking; follow-up",
	"processor/rule/schedule_tracker.go":          "raw write lane — schedule tracking; follow-up",
	"processor/graph-index-temporal/component.go": "raw write lane — temporal index writes; follow-up",
	"processor/graph-index-spatial/component.go":  "raw write lane — spatial index writes; follow-up",
	"pkg/lifecycle/manager.go":                    "raw write lane — lifecycle instance writes; follow-up",
	"test/e2e/client/nats.go":                     "e2e harness client (not a production binary path)",

	// ---- raw core connection (GetConnection) ------------------------------
	"input/udp/udp.go":                 "core publish (ingest fast path) — datagram-bounded payloads; enumerated",
	"input/file/file.go":               "core publish (ingest fast path) — line-bounded payloads; enumerated",
	"input/http/http.go":               "core publish (ingest fast path) — poll-response payloads; enumerated",
	"storage/objectstore/component.go": "conn for subscribe/status (API subscriptions; publishes migrated to the guarded lane in payload-size-chokepoints)",
	"service/flow_runtime_logs.go":     "conn for subscribe/status (log subscription)",
	"service/service_manager.go":       "conn for subscribe/status (liveness check)",
	"gateway/http/http.go":             "conn for subscribe/status (connectivity check)",
}

// payloadGuardCensusPatterns are the acquisition shapes the census scans for.
// Matching ANY pattern puts a file in the census.
var payloadGuardCensusPatterns = []*regexp.Regexp{
	// A raw KV handle held as a type (field, param, var, return). \b keeps
	// KeyValueConfig/KeyValueEntry/KeyValueStatus etc. out.
	regexp.MustCompile(`jetstream\.KeyValue\b`),
	// Direct raw-handle acquisition: js.KeyValue(ctx, bucket).
	regexp.MustCompile(`\.KeyValue\(`),
	// The raw core connection — its Publish bypasses every guard.
	regexp.MustCompile(`GetConnection\(\)`),
}

// TestPayloadGuardBypassCensus enumerates every production file that can
// reach NATS without the payload-bounds seam guard and pins the set. Red on
// a NEW bypass (add the guarded lane or an enumerated allowlist entry with a
// reason) and red on a STALE entry (a migrated file must leave the list, so
// the enumeration never accretes fiction).
func TestPayloadGuardBypassCensus(t *testing.T) {
	root := repoRootForKVCatalogScan(t)

	found := map[string]bool{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			name := info.Name()
			if name == ".git" || name == "testdata" || name == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, pat := range payloadGuardCensusPatterns {
			if pat.Match(content) {
				found[rel] = true
				break
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	var newBypasses, stale []string
	for f := range found {
		if _, ok := payloadGuardCensusAllowlist[f]; !ok {
			newBypasses = append(newBypasses, f)
		}
	}
	for f := range payloadGuardCensusAllowlist {
		if !found[f] {
			stale = append(stale, f)
		}
	}
	sort.Strings(newBypasses)
	sort.Strings(stale)

	if len(newBypasses) > 0 {
		t.Errorf("NEW payload-guard bypasses (raw jetstream.KeyValue handle, .KeyValue( acquisition, "+
			"or GetConnection()) outside the pinned census:\n  %s\n"+
			"Use the guarded natsclient lanes (Client.NewKVStore / Client.Publish*) — or, if the raw "+
			"handle is genuinely read/watch-only, add an enumerated allowlist entry with a reason "+
			"(payload-bounds spec: the guard is unavoidable or the avoidance is enumerated).",
			strings.Join(newBypasses, "\n  "))
	}
	if len(stale) > 0 {
		t.Errorf("stale census entries (file no longer matches any pattern — remove from the allowlist "+
			"so the enumeration stays true):\n  %s", strings.Join(stale, "\n  "))
	}
}
