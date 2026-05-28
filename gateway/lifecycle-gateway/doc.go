// Package lifecyclegateway exposes the pkg/lifecycle.Manager's
// read + operator-mutation surface over HTTP and WebSocket. ADR-047
// PR 3. The endpoints are deliberately framework-shape — every
// app's lifecycle-managed workflow types (drone missions, sensor
// lifecycles, manufacturing batches, scenario executions, API
// request lifecycles) become uniformly inspectable + operator-
// patchable through one gateway component.
//
// Wiring contract:
//
//   - The component reads Dependencies.LifecycleManager. Apps that
//     declare no lifecycle-managed entity types simply don't run
//     this gateway; apps that DO run it pass the same Manager into
//     Dependencies that the rule processor consumes for
//     lifecycle_* actions + $entity.lifecycle.* substitutions.
//   - RegisterHTTPHandlers mounts the routes under the configured
//     PathPrefix (default "/workflows"). Each app deployment chooses
//     where in its gateway placement to mount this — there's no
//     hardcoded port or standalone binary.
//
// Endpoint surface (ADR-047 line 305-315):
//
//   - GET    {prefix}                                     → list registered workflow types + instance counts
//   - GET    {prefix}/{type}                              → list instances (Phase/Active/Match/Limit/Offset query params)
//   - GET    {prefix}/{type}/{id}                         → get full instance state
//   - GET    {prefix}/{type}/{id}/history                 → phase transition history (KV revision replay)
//   - GET    {prefix}/{type}/{id}/children                → child instance summaries
//   - POST   {prefix}/{type}/{id}/state                   → operator patch (validates lifecycle:"operator_writable")
//   - POST   {prefix}/{type}/{id}/transition              → operator-initiated transition (validates transitions table)
//   - GET    {prefix}/{type}?stream=true (websocket)      → live updates via Manager.Watch
//
// All endpoints loud-fail on missing manager / unknown workflow /
// unknown entity rather than silently returning empty results.
// Error responses use a uniform JSON envelope {"error": "..."} so
// operator dashboards can render the underlying cause without
// per-endpoint parsing.
//
// Scaling cliffs documented at ADR-047 § "Scaling cliff (operator
// guidance)" apply to the gateway's List/Watch/History/Children
// paths since they are direct Manager pass-throughs. Operators
// observing dashboard latency at high cardinality should file a
// bottleneck issue per the ADR's v2 secondary-index upgrade path.
package lifecyclegateway
