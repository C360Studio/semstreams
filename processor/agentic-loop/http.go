package agenticloop

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/service"
)

func init() {
	service.RegisterOpenAPISpec("agentic-loop", agenticLoopOpenAPISpec())
}

// Compile-time check that Component implements the HTTP handler interface
var _ interface {
	RegisterHTTPHandlers(prefix string, mux *http.ServeMux)
} = (*Component)(nil)

// OpenAPISpec returns the OpenAPI specification for trajectory endpoints.
func (c *Component) OpenAPISpec() *service.OpenAPISpec {
	return agenticLoopOpenAPISpec()
}

// RegisterHTTPHandlers registers HTTP endpoints for trajectory access.
func (c *Component) RegisterHTTPHandlers(prefix string, mux *http.ServeMux) {
	if !strings.HasSuffix(prefix, "/") {
		prefix = prefix + "/"
	}

	mux.HandleFunc("GET "+prefix+"trajectories", c.handleListTrajectories)
	mux.HandleFunc("GET "+prefix+"trajectories/{loopId}", c.handleGetTrajectory)

	c.logger.Debug("agentic-loop HTTP handlers registered",
		slog.String("list", "GET "+prefix+"trajectories"),
		slog.String("detail", "GET "+prefix+"trajectories/{loopId}"))
}

// trajectoryFilter holds parsed query parameters for trajectory list filtering.
type trajectoryFilter struct {
	Outcome      string
	Role         string
	WorkflowSlug string
	Since        time.Time
	MetaKey      string
	MetaValue    string
}

// parseTrajectoryFilter extracts filter parameters from an HTTP request.
// Returns an error for invalid parameter values (e.g., malformed since timestamp).
func parseTrajectoryFilter(r *http.Request) (trajectoryFilter, error) {
	f := trajectoryFilter{
		Outcome:      r.URL.Query().Get("outcome"),
		Role:         r.URL.Query().Get("role"),
		WorkflowSlug: r.URL.Query().Get("workflow_slug"),
		MetaKey:      r.URL.Query().Get("metadata_key"),
		MetaValue:    r.URL.Query().Get("metadata_value"),
	}
	if s := r.URL.Query().Get("since"); s != "" {
		parsed, err := time.Parse(time.RFC3339, s)
		if err != nil {
			return f, fmt.Errorf("invalid 'since' parameter, expected RFC3339 format: %s", s)
		}
		f.Since = parsed
	}
	if f.MetaValue != "" && f.MetaKey == "" {
		return f, fmt.Errorf("'metadata_value' requires 'metadata_key'")
	}
	return f, nil
}

// matches returns true if the entity passes all filter criteria.
func (f *trajectoryFilter) matches(entity *agentic.LoopEntity) bool {
	if f.Outcome != "" && entity.Outcome != f.Outcome {
		return false
	}
	if f.Role != "" && entity.Role != f.Role {
		return false
	}
	if f.WorkflowSlug != "" && entity.WorkflowSlug != f.WorkflowSlug {
		return false
	}
	if !f.Since.IsZero() && entity.StartedAt.Before(f.Since) {
		return false
	}
	if f.MetaKey != "" && entity.Metadata != nil {
		val, ok := entity.Metadata[f.MetaKey]
		if !ok {
			return false
		}
		if f.MetaValue != "" {
			if str, ok := val.(string); !ok || str != f.MetaValue {
				return false
			}
		}
	}
	return true
}

// entityToListItem converts a LoopEntity to a TrajectoryListItem.
func (c *Component) entityToListItem(entity *agentic.LoopEntity) agentic.TrajectoryListItem {
	item := agentic.TrajectoryListItem{
		LoopID:       entity.ID,
		TaskID:       entity.TaskID,
		Outcome:      entity.Outcome,
		Role:         entity.Role,
		Model:        entity.Model,
		WorkflowSlug: entity.WorkflowSlug,
		WorkflowStep: entity.WorkflowStep,
		Iterations:   entity.Iterations,
		StartTime:    entity.StartedAt,
		Metadata:     entity.Metadata,
	}
	if !entity.CompletedAt.IsZero() {
		t := entity.CompletedAt
		item.EndTime = &t
	}

	// Enrich with trajectory data if available in cache
	if c.trajectoryCache != nil {
		if traj, found := c.trajectoryCache.Get(entity.ID); found {
			item.TotalTokensIn = traj.TotalTokensIn
			item.TotalTokensOut = traj.TotalTokensOut
			item.Duration = traj.Duration
		}
	}

	// Also check in-memory TrajectoryManager for active loops
	if item.Duration == 0 && c.handler != nil {
		if traj, trajErr := c.handler.trajectoryManager.GetTrajectory(entity.ID); trajErr == nil {
			item.TotalTokensIn = traj.TotalTokensIn
			item.TotalTokensOut = traj.TotalTokensOut
			item.Duration = traj.Duration
		}
	}

	return item
}

// handleListTrajectories returns a filtered list of trajectory summaries.
func (c *Component) handleListTrajectories(w http.ResponseWriter, r *http.Request) {
	limit := intParam(r, "limit", 20)
	if limit > 100 {
		limit = 100
	}
	offset := intParam(r, "offset", 0)

	filter, err := parseTrajectoryFilter(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	if c.loopsStore == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "loop storage not available"})
		return
	}

	keys, err := c.loopsStore.Keys(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list loops"})
		return
	}

	var items []agentic.TrajectoryListItem
	for _, key := range keys {
		if strings.HasPrefix(key, "COMPLETE_") {
			continue
		}
		entry, err := c.loopsStore.Get(r.Context(), key)
		if err != nil {
			continue
		}
		var entity agentic.LoopEntity
		if err := json.Unmarshal(entry.Value, &entity); err != nil {
			continue
		}
		if !filter.matches(&entity) {
			continue
		}
		items = append(items, c.entityToListItem(&entity))
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].StartTime.After(items[j].StartTime)
	})

	total := len(items)
	if offset >= len(items) {
		items = nil
	} else {
		items = items[offset:]
		if len(items) > limit {
			items = items[:limit]
		}
	}
	if items == nil {
		items = []agentic.TrajectoryListItem{}
	}

	writeJSON(w, http.StatusOK, agentic.TrajectoryListResponse{
		Trajectories: items,
		Total:        total,
	})
}

// handleGetTrajectory returns a single trajectory by loop ID.
func (c *Component) handleGetTrajectory(w http.ResponseWriter, r *http.Request) {
	loopID := r.PathValue("loopId")
	if loopID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "loopId required"})
		return
	}

	limitStr := r.URL.Query().Get("limit")
	limit := 0
	if limitStr != "" {
		if n, err := strconv.Atoi(limitStr); err == nil && n > 0 {
			limit = n
		}
	}

	// Try cache first (finalized trajectories)
	var traj *agentic.Trajectory
	if c.trajectoryCache != nil {
		traj, _ = c.trajectoryCache.Get(loopID)
	}

	// Fall back to in-memory TrajectoryManager (active loops)
	if traj == nil && c.handler != nil {
		if t, err := c.handler.trajectoryManager.GetTrajectory(loopID); err == nil {
			traj = &t
		}
	}

	if traj == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "trajectory not found"})
		return
	}

	if limit > 0 && len(traj.Steps) > limit {
		limited := *traj
		limited.Steps = limited.Steps[:limit]
		writeJSON(w, http.StatusOK, &limited)
		return
	}

	writeJSON(w, http.StatusOK, traj)
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// intParam parses an integer query parameter with a default value.
func intParam(r *http.Request, name string, defaultVal int) int {
	s := r.URL.Query().Get(name)
	if s == "" {
		return defaultVal
	}
	if n, err := strconv.Atoi(s); err == nil && n >= 0 {
		return n
	}
	return defaultVal
}

// agenticLoopOpenAPISpec returns the OpenAPI spec for agentic-loop HTTP endpoints.
func agenticLoopOpenAPISpec() *service.OpenAPISpec {
	return &service.OpenAPISpec{
		Tags: []service.TagSpec{
			{
				Name:        "Trajectories",
				Description: "Agentic loop trajectory listing and detail",
			},
		},
		ResponseTypes: []reflect.Type{
			reflect.TypeOf(agentic.TrajectoryListResponse{}),
			reflect.TypeOf(agentic.TrajectoryListItem{}),
			reflect.TypeOf(agentic.Trajectory{}),
			reflect.TypeOf(agentic.TrajectoryStep{}),
		},
		Paths: map[string]service.PathSpec{
			"/trajectories": {
				GET: &service.OperationSpec{
					Summary:     "List trajectory summaries with optional filters",
					Description: "Returns paginated trajectory summaries. Filters by outcome, role, workflow, time, and metadata.",
					Tags:        []string{"Trajectories"},
					Parameters: []service.ParameterSpec{
						{Name: "limit", In: "query", Description: "Max items (default 20, max 100)", Schema: service.Schema{Type: "integer"}},
						{Name: "offset", In: "query", Description: "Pagination offset", Schema: service.Schema{Type: "integer"}},
						{Name: "outcome", In: "query", Description: "Filter: success, failed, cancelled", Schema: service.Schema{Type: "string"}},
						{Name: "role", In: "query", Description: "Filter by agent role", Schema: service.Schema{Type: "string"}},
						{Name: "workflow_slug", In: "query", Description: "Filter by workflow", Schema: service.Schema{Type: "string"}},
						{Name: "since", In: "query", Description: "Filter: RFC3339 timestamp", Schema: service.Schema{Type: "string", Format: "date-time"}},
						{Name: "metadata_key", In: "query", Description: "Filter by metadata key", Schema: service.Schema{Type: "string"}},
						{Name: "metadata_value", In: "query", Description: "Filter by metadata value (requires metadata_key)", Schema: service.Schema{Type: "string"}},
					},
					Responses: map[string]service.ResponseSpec{
						"200": {
							Description: "Paginated list of trajectory summaries",
							ContentType: "application/json",
							SchemaRef:   "#/components/schemas/TrajectoryListResponse",
						},
						"400": {Description: "Invalid filter parameters"},
						"503": {Description: "Loop storage not available"},
					},
				},
			},
			"/trajectories/{loopId}": {
				GET: &service.OperationSpec{
					Summary:     "Get full trajectory with steps",
					Description: "Returns the complete trajectory including all steps for a specific loop.",
					Tags:        []string{"Trajectories"},
					Parameters: []service.ParameterSpec{
						{Name: "loopId", In: "path", Required: true, Description: "Loop ID", Schema: service.Schema{Type: "string"}},
						{Name: "limit", In: "query", Description: "Max steps to return", Schema: service.Schema{Type: "integer"}},
					},
					Responses: map[string]service.ResponseSpec{
						"200": {
							Description: "Full trajectory with steps",
							ContentType: "application/json",
							SchemaRef:   "#/components/schemas/Trajectory",
						},
						"400": {Description: "Missing loopId"},
						"404": {Description: "Trajectory not found"},
					},
				},
			},
		},
	}
}
