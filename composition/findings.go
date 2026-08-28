package composition

import (
	"sort"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/component/flowgraph"
)

// Finding types: the closed vocabulary every validator output draws from.
const (
	// TypeConfigInvalid: config.Config.Validate rejected the document.
	TypeConfigInvalid = "config_invalid"
	// TypeUnknownComponent: the configuration names a factory the catalog does not register.
	TypeUnknownComponent = "unknown_component"
	// TypeComponentTypeMismatch: the configured type differs from the factory's registered type.
	TypeComponentTypeMismatch = "component_type_mismatch"
	// TypeComponentConfigInvalid: the component entry itself is invalid (name, type, or raw config security check).
	TypeComponentConfigInvalid = "component_config_invalid"
	// TypePortDeclarationError: the factory's port declarer rejected the configuration.
	TypePortDeclarationError = "port_declaration_error"
	// TypeExclusiveResourceConflict: two components claim the same exclusive resource.
	TypeExclusiveResourceConflict = "exclusive_resource_conflict"
	// TypeConnectionPatternError: connecting the declared ports failed (network binding or KV writer conflicts, malformed graph-mutation providers).
	TypeConnectionPatternError = "connection_pattern_error"
	// TypeStreamRequirement: a JetStream subscriber is fed only by core-NATS publishers, so no stream will exist.
	TypeStreamRequirement = "stream_requirement"
	// TypeDisconnectedNode: a component with no connections at all.
	TypeDisconnectedNode = "disconnected_node"
	// TypeOrphanedPort: a port with no counterpart; an error when it is a required stream input with no publisher.
	TypeOrphanedPort = "orphaned_port"
	// TypeInterfaceMismatch: a derived edge joins an output and an input whose interface contracts differ.
	TypeInterfaceMismatch = "interface_mismatch"
	// TypeMissingInterface: a derived edge's target requires an interface its source does not declare.
	TypeMissingInterface = "missing_interface"
	// TypeEmptyComposition: no enabled components.
	TypeEmptyComposition = "empty_composition"
	// TypeEntityDomainOverlap: two or more producers delegate one entity
	// domain. Permitted (owner ruling 2026-08-28, superseding #1095 O-5) — the
	// taxonomy vocabulary is shared — so this is an observation for the
	// operator composing them, never a refusal. Emitted only by Validate; the
	// boot path runs Analyze, which takes no delegations.
	TypeEntityDomainOverlap = "entity_domain_overlap"
)

// Finding severities.
const (
	SeverityError   = "error"
	SeverityWarning = "warning"
)

// Result statuses, derived errors → warnings → valid.
const (
	StatusValid    = "valid"
	StatusWarnings = "warnings"
	StatusErrors   = "errors"
)

// Finding is one validation finding. Component and Message are always
// non-empty; Suggestions is never nil.
type Finding struct {
	Type        string   `json:"type"`
	Severity    string   `json:"severity"`
	Component   string   `json:"component"`
	Port        string   `json:"port,omitempty"`
	Message     string   `json:"message"`
	Suggestions []string `json:"suggestions"`
}

// severityOf is the one severity table. An orphaned port is an error exactly
// when it is a required stream input with no publisher; every other orphan is
// a warning. The rule was lifted from the since-deleted engine validator
// (engine/validator.go:313-361 as it stood at 5cc0c7fb, removed by #1093) —
// cited for provenance only; that path no longer exists to compare against.
func severityOf(typ string, orphan *flowgraph.OrphanedPort) string {
	switch typ {
	case TypeDisconnectedNode, TypeMissingInterface, TypeEmptyComposition, TypeEntityDomainOverlap:
		return SeverityWarning
	case TypeOrphanedPort:
		if orphan != nil && orphan.Issue == flowgraph.IssueNoPublishers &&
			orphan.Required && orphan.Pattern == component.PatternStream {
			return SeverityError
		}
		return SeverityWarning
	default:
		return SeverityError
	}
}

// Result is the outcome of validating or analyzing one composition. Every
// array is non-nil and every ordering is deterministic.
type Result struct {
	Status   string    `json:"status"`
	Errors   []Finding `json:"errors"`
	Warnings []Finding `json:"warnings"`
	Graph    Graph     `json:"graph"`
}

func newResult() *Result {
	return &Result{
		Status:   StatusValid,
		Errors:   []Finding{},
		Warnings: []Finding{},
		Graph:    Graph{Nodes: []Node{}, Edges: []Edge{}},
	}
}

func (r *Result) add(finding Finding) {
	if finding.Suggestions == nil {
		finding.Suggestions = []string{}
	}
	if finding.Severity == SeverityError {
		r.Errors = append(r.Errors, finding)
		return
	}
	r.Warnings = append(r.Warnings, finding)
}

// finalize orders the findings and derives the status.
func (r *Result) finalize() {
	sortFindings(r.Errors)
	sortFindings(r.Warnings)
	switch {
	case len(r.Errors) > 0:
		r.Status = StatusErrors
	case len(r.Warnings) > 0:
		r.Status = StatusWarnings
	default:
		r.Status = StatusValid
	}
}

func sortFindings(findings []Finding) {
	sort.SliceStable(findings, func(i, j int) bool {
		a, b := findings[i], findings[j]
		if a.Type != b.Type {
			return a.Type < b.Type
		}
		if a.Component != b.Component {
			return a.Component < b.Component
		}
		if a.Port != b.Port {
			return a.Port < b.Port
		}
		return a.Message < b.Message
	})
}
