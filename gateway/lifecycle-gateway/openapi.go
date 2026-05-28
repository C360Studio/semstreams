// Package lifecyclegateway — OpenAPI spec.
//
// The lifecycle-gateway implements gateway.OpenAPIProvider so the
// operator-facing endpoints show up in the framework's auto-
// generated OpenAPI surface (specs/openapi.v3.yaml). Matches the
// pattern from gateway/graph-gateway/openapi.go.
//
// Path templates use literal "{prefix}/{type}/{id}" placeholders —
// the actual path prefix is operator-configurable per deployment,
// so the spec advertises the shape, not the bound paths.
package lifecyclegateway

import (
	"reflect"

	"github.com/c360studio/semstreams/service"
)

// StatePatchRequest is the JSON body shape for POST {type}/{id}/state.
// Reflective-typed schema; the actual content is a free-form
// `map[string]any` field-name → value patch validated server-side
// against `lifecycle:"operator_writable"` tags.
type StatePatchRequest map[string]any

// TransitionRequest is the JSON body shape for POST {type}/{id}/transition.
// Phase is required; Note is operator commentary persisted into the
// TransitionEvent.Note for audit.
type TransitionRequest struct {
	Phase string `json:"phase"`
	Note  string `json:"note,omitempty"`
}

func init() {
	service.RegisterOpenAPISpec(ComponentName, lifecycleGatewayOpenAPISpec())
}

// OpenAPISpec implements gateway.OpenAPIProvider.
func (c *Component) OpenAPISpec() *service.OpenAPISpec {
	return lifecycleGatewayOpenAPISpec()
}

// lifecycleGatewayOpenAPISpec returns the OpenAPI spec for the
// 7 HTTP endpoints + WebSocket. Path templates use the
// operator-facing shape from ADR-047 § Operator API.
func lifecycleGatewayOpenAPISpec() *service.OpenAPISpec {
	return &service.OpenAPISpec{
		Tags: []service.TagSpec{
			{Name: "Lifecycle", Description: "pkg/lifecycle.Manager operator read + mutation surface (ADR-047)"},
		},
		Paths: map[string]service.PathSpec{
			"/workflows": {
				GET: &service.OperationSpec{
					Summary:     "List registered workflow types",
					Description: "Returns the workflow types registered with the Manager + per-phase instance counts. Per-workflow List errors surface as a `counts_error` field on the failing entry so partial degradation is visible without breaking the whole response.",
					Tags:        []string{"Lifecycle"},
					Responses: map[string]service.ResponseSpec{
						"200": {Description: "Workflow types with instance counts", ContentType: "application/json", IsArray: true},
						"405": {Description: "Method not allowed (GET only)"},
					},
				},
			},
			"/workflows/{type}": {
				GET: &service.OperationSpec{
					Summary:     "List instances of a workflow type",
					Description: "Lists Participant instances for the given workflow type with filter + pagination query parameters. When ?stream=true is set, the connection is upgraded to a WebSocket that streams bootstrap + live updates from Manager.Watch.",
					Tags:        []string{"Lifecycle"},
					Parameters: []service.ParameterSpec{
						{Name: "type", In: "path", Required: true, Description: "Workflow type identifier (matches Participant.Workflow())", Schema: service.Schema{Type: "string"}},
						{Name: "phase", In: "query", Description: "Filter to instances in this phase", Schema: service.Schema{Type: "string"}},
						{Name: "active", In: "query", Description: "Filter to non-terminal instances when true", Schema: service.Schema{Type: "boolean"}},
						{Name: "limit", In: "query", Description: "Maximum results returned (default unlimited)", Schema: service.Schema{Type: "integer"}},
						{Name: "offset", In: "query", Description: "Skip the first N matching results for pagination", Schema: service.Schema{Type: "integer"}},
						{Name: "match.<field>", In: "query", Description: "Field-equality filter (any number; one query param per field)", Schema: service.Schema{Type: "string"}},
						{Name: "stream", In: "query", Description: "Set to 'true' to upgrade to a WebSocket carrying Manager.Watch updates", Schema: service.Schema{Type: "string"}},
					},
					Responses: map[string]service.ResponseSpec{
						"200": {Description: "Instance array (or WebSocket frames when ?stream=true)", ContentType: "application/json", IsArray: true},
						"400": {Description: "Invalid query parameter"},
						"403": {Description: "WebSocket streaming disabled (enable_websocket=false)"},
						"404": {Description: "Workflow type not registered"},
						"405": {Description: "Method not allowed (GET only)"},
					},
				},
			},
			"/workflows/{type}/{id}": {
				GET: &service.OperationSpec{
					Summary:     "Get instance state",
					Description: "Returns the full Participant state for the given workflow type + entity ID.",
					Tags:        []string{"Lifecycle"},
					Parameters: []service.ParameterSpec{
						{Name: "type", In: "path", Required: true, Description: "Workflow type identifier", Schema: service.Schema{Type: "string"}},
						{Name: "id", In: "path", Required: true, Description: "Entity ID (Participant.EntityID())", Schema: service.Schema{Type: "string"}},
					},
					Responses: map[string]service.ResponseSpec{
						"200": {Description: "Participant state", ContentType: "application/json"},
						"404": {Description: "Workflow or entity not found"},
						"405": {Description: "Method not allowed (GET only)"},
					},
				},
			},
			"/workflows/{type}/{id}/history": {
				GET: &service.OperationSpec{
					Summary:     "List phase-transition history",
					Description: "Returns the phase-transition history derived from KV revision replay. Each entry includes from/to phases, wallclock timestamp, the TransitionSource (rule/operator/component/framework), and any operator-supplied note.",
					Tags:        []string{"Lifecycle"},
					Parameters: []service.ParameterSpec{
						{Name: "type", In: "path", Required: true, Description: "Workflow type identifier", Schema: service.Schema{Type: "string"}},
						{Name: "id", In: "path", Required: true, Description: "Entity ID", Schema: service.Schema{Type: "string"}},
					},
					Responses: map[string]service.ResponseSpec{
						"200": {Description: "TransitionEvent array", ContentType: "application/json", IsArray: true},
						"404": {Description: "Workflow or entity not found"},
						"405": {Description: "Method not allowed (GET only)"},
					},
				},
			},
			"/workflows/{type}/{id}/children": {
				GET: &service.OperationSpec{
					Summary:     "List child instances",
					Description: "Returns Participants whose ParentEntityID matches the given entity, across all registered workflows. The {type} segment is required for routing symmetry with the other endpoints; the underlying Manager.Children call scans cross-workflow.",
					Tags:        []string{"Lifecycle"},
					Parameters: []service.ParameterSpec{
						{Name: "type", In: "path", Required: true, Description: "Workflow type identifier (routing-only; cross-workflow scan ignores it)", Schema: service.Schema{Type: "string"}},
						{Name: "id", In: "path", Required: true, Description: "Parent entity ID", Schema: service.Schema{Type: "string"}},
					},
					Responses: map[string]service.ResponseSpec{
						"200": {Description: "Child Participant array", ContentType: "application/json", IsArray: true},
						"405": {Description: "Method not allowed (GET only)"},
					},
				},
			},
			"/workflows/{type}/{id}/state": {
				POST: &service.OperationSpec{
					Summary:     "Operator patch (state mutation)",
					Description: "Applies a JSON-body patch to the Participant. The body is a `{<field>: <value>}` map; every field MUST be tagged `lifecycle:\"operator_writable\"` on the registered state struct. Identity (`lifecycle:\"id\"`) and phase fields are protected by the same default-deny gate the rule layer enforces (ADR-047 § AssertRuleWritable).",
					Tags:        []string{"Lifecycle"},
					Parameters: []service.ParameterSpec{
						{Name: "type", In: "path", Required: true, Description: "Workflow type identifier", Schema: service.Schema{Type: "string"}},
						{Name: "id", In: "path", Required: true, Description: "Entity ID", Schema: service.Schema{Type: "string"}},
					},
					RequestBody: &service.RequestBodySpec{
						Description: "Field-name → value patch map",
						Required:    true,
						SchemaRef:   "#/components/schemas/StatePatchRequest",
					},
					Responses: map[string]service.ResponseSpec{
						"200": {Description: "Patch applied", ContentType: "application/json"},
						"400": {Description: "Invalid body OR field is not operator_writable OR type mismatch"},
						"404": {Description: "Workflow or entity not found"},
						"405": {Description: "Method not allowed (POST only)"},
						"409": {Description: "Optimistic-concurrency retries exhausted"},
						"413": {Description: "Request body exceeds max_body_bytes"},
					},
				},
			},
			"/workflows/{type}/{id}/transition": {
				POST: &service.OperationSpec{
					Summary:     "Operator-initiated phase transition",
					Description: "Transitions the Participant to the requested phase via Manager.Transition with TransitionSourceOperator. Phase must be declared in the workflow's Transitions table; current → target must be a declared edge; current must not be terminal.",
					Tags:        []string{"Lifecycle"},
					Parameters: []service.ParameterSpec{
						{Name: "type", In: "path", Required: true, Description: "Workflow type identifier", Schema: service.Schema{Type: "string"}},
						{Name: "id", In: "path", Required: true, Description: "Entity ID", Schema: service.Schema{Type: "string"}},
					},
					RequestBody: &service.RequestBodySpec{
						Description: "Target phase + optional operator note for the audit trail",
						Required:    true,
						SchemaRef:   "#/components/schemas/TransitionRequest",
					},
					Responses: map[string]service.ResponseSpec{
						"200": {Description: "Transition applied", ContentType: "application/json"},
						"400": {Description: "Invalid body OR target phase undeclared OR edge undeclared OR current phase terminal"},
						"404": {Description: "Workflow or entity not found"},
						"405": {Description: "Method not allowed (POST only)"},
						"413": {Description: "Request body exceeds max_body_bytes"},
					},
				},
			},
		},
		RequestBodyTypes: []reflect.Type{
			reflect.TypeOf(TransitionRequest{}),
		},
	}
}
