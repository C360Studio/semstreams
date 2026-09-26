// Package e2eslowconsumer owns the private contract for the slow-consumer E2E
// fixture. Only internal/e2eboot imports it, so only the E2E binary links it.
package e2eslowconsumer

const (
	// Subject is private to the disposable fixture.
	Subject = "e2e.diagnostics.slow-consumer"
	// Queue identifies the private fixture subscription.
	Queue = "e2e-slow-consumer"
	// ExpectedDropped is the exact fixed overflow count produced by the fixture.
	ExpectedDropped = 8
)
