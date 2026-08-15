## 1. Contract

- [x] 1.1 Add and strictly validate the `jetstream-consumer-policy` delta for component-local defaults.
- [x] 1.2 Preserve the reviewed inventory/design identities and accepted archived #963 artifacts byte-for-byte.

## 2. TDD implementation

- [x] 2.1 Add focused failing policy tests for document and IoT zero/default and positive override behavior.
- [x] 2.2 Add deterministic failing real-NATS cold-start tests for both processors without arbitrary sleeps.
- [x] 2.3 Implement the two private local resolvers and use them in the final managed consumer configuration.
- [x] 2.4 Prove explicit delivery/ack and positive max-delivery values win while `MaxAckPending` forwards independently.

## 3. Verification

- [x] 3.1 Run focused unit race tests for both example processor packages.
      Evidence: both package suites passed with `-race -count=1`.
- [x] 3.2 Run focused real-NATS integration race tests for both example processor packages.
      Evidence: both pre-start retained-message tests passed with the integration tag and race detector.
- [x] 3.3 Run lint, full race, schema generation/no-drift, contract tests, and strict OpenSpec validation.
      Evidence: all gates passed; schema generation changed no checked-in schema or OpenAPI output.
- [x] 3.4 Run structural, statistical, and semantic E2E tiers serially and record authoritative outcome evidence.
      Evidence: structural passed 38/38, statistical passed 41/41, and semantic passed 48/48. Owned stack teardown was
      clean and the sister container remained untouched.
- [x] 3.5 Obtain independent SemStreams implementation review on the exact final diff.
      Evidence: final independent SemStreams implementation review returned `APPROVED` with no remaining findings.
