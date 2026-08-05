// Package graphmutation owns the in-repository graph mutation protocol identity.
// Application callers use narrow typed clients rather than subjects directly.
package graphmutation

import (
	"fmt"
	"strings"
)

const (
	InterfaceType    = "semstreams.graph.mutation"
	InterfaceVersion = "v1"
	SubjectFamily    = "graph.mutation.>"
)

// Operation identifies one admitted graph mutation command.
type Operation string

const (
	CreateEntity        Operation = "entity.create"
	ReconcilePredicates Operation = "entity.reconcile"
	AppendTriples       Operation = "triple.append"
	DeleteEntity        Operation = "entity.delete"
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
