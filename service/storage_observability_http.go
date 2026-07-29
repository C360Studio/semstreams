// storage_observability_http.go is the operator HTTP surface over the PUBLISHED
// storage report (storage-capacity-observability, task 4.4).
//
// It is a CONSUMER of the report bucket and nothing else. Every value it serves
// arrives through natsclient.StorageReportConsumer's Watch of the bucket the
// collector publishes; this file collects no inventory, evaluates no pressure,
// and does no growth arithmetic. That is the whole of design decision 8: the
// report is produced once, at publication, from inputs only a collection holds —
// the retained observation series and the account limits — so a route that
// recomputed any of them from what it happened to have in memory would
// eventually disagree with the metrics an operator reads next to it, and an
// operator cannot act on two answers.
//
// # No filtering, deliberately
//
// The route serves EVERY published row and takes no filter parameter. A row may
// carry no pressure state at all — its capacity was unreadable, or it has no bound
// of its own AND its storage tier offers no ceiling to inherit — so a
// `state != normal` filter would make exactly those resources invisible, the
// opposite of what task 4.7 exists to do. The summary therefore counts
// not-evaluated rows as their own bucket rather than folding them into normal, and
// the rows themselves are served verbatim.
//
// A row with no bound of its own that DOES have a tier ceiling is evaluated against
// it and carries `pressure.evaluated_against: "account-tier"`. Read that field
// before acting on such a row: the state is its tier's, shared with every other
// unbounded resource in the tier, and it cannot be relieved by changing anything
// about this resource.
//
// # Report-only
//
// The response is 200 whatever the report says. Critical pressure, an
// over-committed tier, an unreadable account and a report nobody has read yet
// are all 200 with the fact in the body. A status code derived from pressure
// would turn an observability route into a readiness signal for anything that
// polls it, which is exactly the enforcement this capability refuses.

package service

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/c360studio/semstreams/natsclient"
)

// Compile-time check that the service exposes its report over HTTP.
var _ HTTPHandler = (*StorageObservabilityService)(nil)

// StorageReportResponse is the body of `GET /storage-observability/report`.
//
// Resources are the PUBLISHED rows verbatim — the same natsclient.ResourceReport
// values the collector wrote and the metrics surface consumed — so the HTTP
// shape cannot drift away from the bucket's. Nothing here is recomputed.
type StorageReportResponse struct {
	// ReportOnly is always true and is stated on the response's face. A reader
	// automating against this route must be able to see, without consulting a
	// document, that nothing was rejected, throttled, or degraded because of
	// what it says.
	ReportOnly bool `json:"report_only"`

	// Synced reports that the watch has delivered every current value at least
	// once. Before it, an empty Resources means "not read yet" rather than "the
	// account holds nothing" — two facts an operator acts on differently.
	Synced bool `json:"synced"`

	// UpdatedAt is when the last change was applied to the in-process view. A
	// POINTER because `omitempty` is a no-op on a time.Time: a value field would
	// publish a zero timestamp on an unread report and invite a reader to treat
	// it as a real one.
	UpdatedAt *time.Time `json:"updated_at,omitempty"`

	// Summary is a TALLY of what the rows say, carried through from the
	// consumer. It is never a re-evaluation.
	Summary StorageReportSummary `json:"summary"`

	// Account is the per-tier declared-versus-limit comparison. Absent — not
	// zero-valued — when no account row has been read, because an empty
	// comparison reads as "no tiers, nothing over-committed".
	Account *natsclient.AccountReport `json:"account,omitempty"`

	// Resources is every published row, sorted by resource name. Never
	// filtered: see the file comment.
	Resources []natsclient.ResourceReport `json:"resources"`
}

// StorageReportSummary is the account at a glance.
type StorageReportSummary struct {
	// Resources is how many rows the report carries.
	Resources int `json:"resources"`

	// Pressure counts the EVALUATED rows by state.
	Pressure map[natsclient.PressureState]int `json:"pressure"`

	// NotEvaluated counts the rows carrying no pressure state at all. Its own
	// bucket rather than folded into normal: otherwise the resources nothing can
	// be said about would be the ones that disappear from the summary (task 4.7).
	//
	// The set CHANGED in task 5.9 and a consumer reading this field as "the
	// unbounded resources" now undercounts. A resource with no bound of its own is
	// evaluated against its storage tier's ceiling and counts under Pressure by
	// that state; what remains here is a resource whose capacity could not be
	// READ, plus an unbounded one whose tier offers no ceiling either. To count
	// the unbounded set, read the rows' capacity state — not this field.
	NotEvaluated int `json:"not_evaluated"`

	// WorstPressure is the worst EVALUATED state, omitted when nothing was
	// evaluated. Omission is NOT normal: an account whose every row declined to
	// evaluate has no pressure verdict, and publishing one would manufacture it.
	WorstPressure natsclient.PressureState `json:"worst_pressure,omitempty"`
}

// storageReportHTTPSurface serves the consumed report.
//
// The reader is a FUNCTION rather than the concrete consumer, mirroring
// storageHealthSurface: rendering is then exercised directly on the states that
// matter — critical pressure, an unbounded resource, a report nobody has read
// yet — without standing up an account to produce them. It does no I/O.
type storageReportHTTPSurface struct {
	snapshotOf func() natsclient.StorageReportSnapshot
	logger     *slog.Logger
}

// register mounts the route under the service's prefix, following the
// mux+prefix shape the other services use.
func (s *storageReportHTTPSurface) register(prefix string, mux *http.ServeMux) {
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	mux.HandleFunc(prefix+"report", s.handleReport)
}

// handleReport writes the published report as JSON.
func (s *storageReportHTTPSurface) handleReport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	response := storageReportResponseFrom(s.snapshot())

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		s.warn("could not encode the storage report response", err)
	}
}

func (s *storageReportHTTPSurface) snapshot() natsclient.StorageReportSnapshot {
	if s.snapshotOf == nil {
		return natsclient.StorageReportSnapshot{}
	}
	return s.snapshotOf()
}

func (s *storageReportHTTPSurface) warn(message string, err error) {
	logger := s.logger
	if logger == nil {
		logger = slog.Default()
	}
	logger.Warn(message, slog.String("error", err.Error()))
}

// storageReportResponseFrom projects the consumed snapshot onto the wire.
//
// Every field is a PROJECTION of a value the consumer already holds. There is no
// arithmetic here beyond the length of the slice being served, which is the
// property that makes this surface unable to disagree with the metrics surface.
func storageReportResponseFrom(snapshot natsclient.StorageReportSnapshot) StorageReportResponse {
	response := StorageReportResponse{
		ReportOnly: true,
		Synced:     snapshot.Synced,
		Summary: StorageReportSummary{
			Resources:    len(snapshot.Resources),
			Pressure:     snapshot.PressureCounts,
			NotEvaluated: snapshot.NotEvaluated,
			// Empty when nothing was evaluated, and omitted rather than
			// defaulted: see the field comment.
			WorstPressure: snapshot.WorstPressure,
		},
		// Never null on the wire: a JSON reader distinguishes "no rows" from
		// "this field was not populated", and only the first is true here.
		Resources: snapshot.Resources,
	}
	if response.Resources == nil {
		response.Resources = []natsclient.ResourceReport{}
	}
	if response.Summary.Pressure == nil {
		response.Summary.Pressure = map[natsclient.PressureState]int{}
	}
	if !snapshot.UpdatedAt.IsZero() {
		updated := snapshot.UpdatedAt
		response.UpdatedAt = &updated
	}
	if snapshot.AccountKnown {
		account := snapshot.Account
		response.Account = &account
	}
	return response
}

// RegisterHTTPHandlers mounts the operator report route.
func (s *StorageObservabilityService) RegisterHTTPHandlers(prefix string, mux *http.ServeMux) {
	s.http.register(prefix, mux)
	s.logger.Info("storage observability HTTP handlers registered", "prefix", prefix)
}

// OpenAPISpec documents the report route for the runtime `/openapi.json`.
//
// It is deliberately NOT registered through RegisterOpenAPISpec: the generator
// writes each registered spec's paths at the document ROOT, unprefixed
// (cmd/openapi-generator/openapi_builder.go), so registering would publish a
// bare `/report` in specs/openapi.v3.yaml — a top-level path that says nothing
// about which service serves it. The runtime document, which applies the
// service prefix, gets the route either way.
func (s *StorageObservabilityService) OpenAPISpec() *OpenAPISpec {
	return &OpenAPISpec{
		Tags: []TagSpec{
			{
				Name: "StorageObservability",
				Description: "Account storage capacity report. REPORT-ONLY: nothing is rejected, " +
					"throttled, or degraded because of what it says.",
			},
		},
		Paths: map[string]PathSpec{
			"/report": {
				GET: &OperationSpec{
					Summary: "Get the published account storage report",
					Description: "Returns the storage report as published to the framework report " +
						"bucket: one row per account storage resource with its owner or attribution " +
						"state, configured bound or its absence, usage, headroom, growth rate, " +
						"projected time-to-threshold, and pressure state. This route is a CONSUMER " +
						"of that bucket and recomputes nothing, so it cannot disagree with the " +
						"Prometheus surface. Every row is served and the route takes no filter: a " +
						"row whose capacity could not be read carries no pressure state and would " +
						"be the first to disappear under one. A resource with no bound of its own " +
						"is evaluated against its storage TIER's account ceiling and says so in " +
						"pressure.evaluated_against; that state is shared with every unbounded " +
						"resource in the tier and is not relieved by changing this resource.",
					Tags: []string{"StorageObservability"},
					Responses: map[string]ResponseSpec{
						"200": {
							Description: "The published storage report. Always 200, including when " +
								"pressure is critical or the report has not been read yet.",
							ContentType: "application/json",
						},
					},
				},
			},
		},
	}
}
