// Package composition validates a component composition — the boot
// configuration's components composed with the catalog a binary registers —
// as a pure function, and projects it as a graph.
//
// Two evidence classes share one interpreter (ADR-100):
//
//   - Validate(catalog, cfg) is the offline judgment: each factory's static
//     port declarer (component.Registration.Ports) predicts the ports the next
//     boot would admit, with no NATS, no process, and no construction.
//   - Analyze(declarations) is the graph-level half of the same function over
//     admitted declarations; ComponentManager runs it at boot over what was
//     actually admitted and refuses to boot on an error-severity finding.
//
// Findings carry a closed vocabulary (the Type* constants), one severity table
// (severityOf), non-nil arrays, and a deterministic order, so two runs over
// equal inputs marshal to byte-equal JSON.
package composition
