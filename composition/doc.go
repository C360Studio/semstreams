// Package composition validates a component composition — the boot
// configuration's components composed with the catalog a binary registers —
// as a pure function, and projects it as a graph.
//
// Two evidence classes share one interpreter (ADR-100):
//
//   - Validate(catalog, cfg) is the offline judgment: each factory's static
//     port declarer (component.Registration.Ports) predicts the ports the next
//     boot would admit, with no NATS, no process, and no construction.
//   - Analyze(declarations, streams) is the graph-level half of the same
//     function over admitted declarations and the configuration's explicit
//     streams; ComponentManager runs it at boot over what was actually
//     admitted, logs every finding, and serves the result (ADR-100 P5 —
//     whether an error-severity finding refuses boot is the owner's ruling
//     recorded in the change's tasks 3.6).
//
// Findings carry a closed vocabulary (the Type* constants), one severity table
// (severityOf), non-nil arrays, and a deterministic order, so two runs over
// equal inputs marshal to byte-equal JSON.
package composition
