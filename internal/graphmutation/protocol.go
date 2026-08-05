// Package graphmutation owns the in-repository graph mutation protocol identity.
// Application callers use narrow typed clients rather than subjects directly.
package graphmutation

import (
	"fmt"
	"strings"
)

const (
	// InterfaceType is the canonical component-port interface identity.
	InterfaceType = "semstreams.graph.mutation"
	// InterfaceVersion is the canonical component-port interface version.
	InterfaceVersion = "v1"
	// SubjectFamily is the only admitted NATS request/reply subject family.
	SubjectFamily = "graph.mutation.>"
)

// Operation identifies one admitted graph mutation command.
type Operation string

const (
	// CreateEntity strictly creates one entity and rejects an existing ID.
	CreateEntity Operation = "entity.create"
	// ReconcilePredicates replaces a declared predicate set under a revision fence.
	ReconcilePredicates Operation = "entity.reconcile"
	// AppendTriples adds set-valued triples to existing subjects.
	AppendTriples Operation = "triple.append"
	// DeleteEntity removes one entity under a revision fence.
	DeleteEntity Operation = "entity.delete"
)

// ResolveSubject returns the exact subject for an admitted operation within
// the canonical declared family. It rejects every other family and operation;
// callers cannot fall back to a port name or a literal subject table.
func ResolveSubject(family string, operation Operation) (string, error) {
	if family != SubjectFamily {
		return "", fmt.Errorf("graph mutation family %q is not canonical", family)
	}
	switch operation {
	case CreateEntity, ReconcilePredicates, AppendTriples, DeleteEntity:
		return strings.TrimSuffix(family, ">") + string(operation), nil
	default:
		return "", fmt.Errorf("graph mutation operation %q is not admitted", operation)
	}
}
