// Package service composes long-lived SemStreams services, the HTTP server,
// and the component topology selected at process boot.
//
// The Manager owns service registration, initialization, HTTP composition,
// health aggregation, startup, and reverse-order shutdown. ComponentManager
// constructs the component map from Config Manager's sealed BootConfig and
// does not reconcile later desired-state writes into the running process.
//
// # Flow authoring
//
// FlowService exposes saved-diagram CRUD and validation. Its explicit
// publish-component-configs operation compiles a saved diagram and upserts
// component configuration candidates for a later boot. It never starts,
// stops, replaces, or deletes running components. Diagram omission never
// implies desired-component deletion.
//
// Flow observations query health, metrics, or messages using component names
// declared by the saved diagram. They are observations only: they do not
// assert that the diagram deployed or owns those components.
//
// # Lifecycle
//
// Services receive lifecycle context from their owner. Start-owned work must
// stop accepting work, cancel, and join before Stop returns. The package does
// not implement component hot reload; changing component configuration after
// boot requires a clean process restart.
//
// # HTTP composition
//
// Manager installs system endpoints before service endpoints and aggregates
// service OpenAPI fragments after initialization. Services register handlers
// on the caller-owned ServeMux. Authentication, TLS termination, and external
// rate limiting remain deployment concerns at the gateway or reverse proxy.
//
// # Errors and health
//
// Constructors reject invalid dependencies and configuration. Initialize and
// Start return failures to the composition root. Runtime failures update
// health or logs as appropriate. Shutdown aggregates owner errors so one
// failing service does not prevent remaining owners from receiving Stop.
package service
