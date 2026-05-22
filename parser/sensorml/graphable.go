package sensorml

import (
	"github.com/c360studio/semstreams/message"
)

// Asset pairs a parsed SensorML [Process] with the 6-part
// SemStreams entity ID assigned by the operator. Asset
// implements the [graph.Graphable] contract; the underlying
// Process does not, because SensorML's document-local id is not
// a SemStreams entity ID.
//
// Multiple Assets may share the same underlying Process value
// (e.g. when ingestion deduplicates structurally identical
// system descriptions across deployments). EntityID is the
// stable handle; pair construction is the operator's
// responsibility.
type Asset struct {
	entityID string
	Process  Process
	// ChildIDFn maps a child component's local id (the inner
	// SensorML id field, e.g. "battery") to its 6-part SemStreams
	// entity ID. When nil, child triples reference the bare
	// local id — fine for in-document graph queries but a
	// downgrade for cross-system entity resolution.
	ChildIDFn func(localID string) string
}

// NewAsset wraps a parsed Process with a 6-part SemStreams
// entity ID. The 6-part ID is opaque to this package — callers
// own the org.platform.domain.system.type.instance convention.
func NewAsset(entityID string, process Process) *Asset {
	return &Asset{entityID: entityID, Process: process}
}

// EntityID implements [graph.Graphable].
func (a *Asset) EntityID() string { return a.entityID }

// Triples implements [graph.Graphable]. Emits an rdf:type
// triple, label / description / definition where present, every
// identifier value as a separate triple, and the structural
// relationships to children (sosa:hosts for PhysicalSystem,
// ssn:hasSubSystem for AggregateProcess) plus the procedure
// reference (sosa:usedProcedure) for leaf physical / simple
// processes.
func (a *Asset) Triples() []message.Triple {
	if a.Process == nil {
		return nil
	}
	base := a.Process.Base()
	if base == nil {
		return nil
	}
	out := []message.Triple{
		{Subject: a.entityID, Predicate: PredType, Object: processClassIRI(a.Process)},
	}
	if base.Label != "" {
		out = append(out, message.Triple{Subject: a.entityID, Predicate: PredLabel, Object: base.Label})
	}
	if base.Description != "" {
		out = append(out, message.Triple{Subject: a.entityID, Predicate: PredDescription, Object: base.Description})
	}
	if base.Definition != "" {
		out = append(out, message.Triple{Subject: a.entityID, Predicate: PredDefinition, Object: base.Definition})
	}
	if base.UniqueID != "" {
		out = append(out, message.Triple{Subject: a.entityID, Predicate: PredUniqueID, Object: base.UniqueID})
	}
	for _, ident := range base.Identifiers {
		if ident.Value != nil {
			out = append(out, message.Triple{Subject: a.entityID, Predicate: PredIdentifierValue, Object: ident.Value})
		}
	}
	for _, cap := range base.Capabilities {
		if cap.Label != "" && cap.Value != nil {
			out = append(out, message.Triple{Subject: a.entityID, Predicate: PredCapabilityValue, Object: cap.Value})
		}
	}
	for _, char := range base.Characteristics {
		if char.Label != "" && char.Value != nil {
			out = append(out, message.Triple{Subject: a.entityID, Predicate: PredCharacteristicValue, Object: char.Value})
		}
	}
	if base.Position != nil && len(base.Position.Raw) > 0 {
		out = append(out, message.Triple{
			Subject:   a.entityID,
			Predicate: PredPosition,
			Object:    string(base.Position.Raw),
		})
	}
	out = append(out, a.typeSpecificTriples()...)
	return out
}

// typeSpecificTriples emits structural triples that depend on
// the concrete Process kind. Split out to keep Triples readable.
func (a *Asset) typeSpecificTriples() []message.Triple {
	switch p := a.Process.(type) {
	case *PhysicalSystem:
		return a.physicalSystemTriples(p)
	case *PhysicalComponent:
		return a.physicalComponentTriples(p)
	case *SimpleProcess:
		return a.simpleProcessTriples(p)
	case *AggregateProcess:
		return a.aggregateProcessTriples(p)
	default:
		return nil
	}
}

func (a *Asset) physicalSystemTriples(p *PhysicalSystem) []message.Triple {
	out := make([]message.Triple, 0, len(p.Components))
	if p.AttachedTo != nil && p.AttachedTo.Href != "" {
		out = append(out, message.Triple{Subject: a.entityID, Predicate: PredAttachedTo, Object: p.AttachedTo.Href})
	}
	for _, child := range p.Components {
		childID := a.childID(child)
		if childID == "" {
			continue
		}
		out = append(out,
			message.Triple{Subject: a.entityID, Predicate: PredHosts, Object: childID},
			message.Triple{Subject: childID, Predicate: PredIsHostedBy, Object: a.entityID},
		)
	}
	return out
}

func (a *Asset) physicalComponentTriples(p *PhysicalComponent) []message.Triple {
	var out []message.Triple
	if p.Method != nil && p.Method.Href != "" {
		out = append(out, message.Triple{Subject: a.entityID, Predicate: PredUsedProcedure, Object: p.Method.Href})
	}
	if p.AttachedTo != nil && p.AttachedTo.Href != "" {
		out = append(out, message.Triple{Subject: a.entityID, Predicate: PredAttachedTo, Object: p.AttachedTo.Href})
	}
	return out
}

func (a *Asset) simpleProcessTriples(p *SimpleProcess) []message.Triple {
	if p.Method != nil && p.Method.Href != "" {
		return []message.Triple{
			{Subject: a.entityID, Predicate: PredUsedProcedure, Object: p.Method.Href},
		}
	}
	return nil
}

func (a *Asset) aggregateProcessTriples(p *AggregateProcess) []message.Triple {
	out := make([]message.Triple, 0, len(p.Components))
	for _, child := range p.Components {
		childID := a.childID(child)
		if childID == "" {
			continue
		}
		out = append(out, message.Triple{Subject: a.entityID, Predicate: PredHasSubSystem, Object: childID})
	}
	return out
}

// childID resolves a child process's entity ID, preferring the
// configured ChildIDFn for 6-part resolution, falling back to
// the bare SensorML local id when the operator has not wired a
// resolver.
func (a *Asset) childID(child Process) string {
	local := child.LocalID()
	if local == "" {
		return ""
	}
	if a.ChildIDFn != nil {
		return a.ChildIDFn(local)
	}
	return local
}
