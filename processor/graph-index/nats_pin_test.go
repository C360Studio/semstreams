package graphindex

// One pin for this package's evidence gates (owner-filter load, predicate-
// layout smoke). Both integration harnesses alias these constants, so the
// package cannot carry two divergent pins — the 2026-08-02 audit found both
// gates running server 2.12.4 + reporting SDK v1.48.0 while go.mod built
// v1.52.0 and the harness docs claimed 2.14.4: the evidence log lines were
// silently false. The server fragment is also matched by the repo-wide
// convergence guard (test/contract/nats_version_contract_test.go), which is
// what makes the NEXT bump loud here instead of silent.
//
// Digest is the same normative pin natsclient's KV key contract runs
// (natsclient/kv_key_contract_integration_test.go).
// scripts/lint-nats-kv-sdk-pin.sh checks the resolved nats.go module against
// this evidence pin and the KV contract matrix pin before test binaries run.
const (
	graphIndexNATSServerPin = "2.14.4-alpine@sha256:f2123f533c2b0cada0a5c5ec434fb2b8cfe1cf220215ef9d7517e1372917ad66"
	graphIndexNATSGoPin     = "v1.52.0"
)
