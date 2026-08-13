// Package e2eslowconsumer owns the private contract for the tagged slow-consumer E2E fixture.
package e2eslowconsumer

const (
	// Subject is private to the tagged disposable fixture.
	Subject = "e2e.diagnostics.slow-consumer"
	// Queue identifies the private fixture subscription.
	Queue = "e2e-slow-consumer"
	// ExpectedDropped is the exact fixed overflow count produced by the fixture.
	ExpectedDropped = 8
)
