# Tasks — Gated-DAG durable dispatch (framework side)

## 1. natsclient `ConsumeDurable` + heartbeat/ackwait enforcement

- [ ] 1.1 `Client.ConsumeDurable(ctx, cfg, heartbeat, handler func(ctx, []byte) error)`
      composing `ConsumeStreamWithConfig` + `ConsumeWithHeartbeat` + ack/nak
- [ ] 1.2 Enforce `heartbeat < AckWait` (with margin) at config-validate /
      ConsumeDurable time; reject with an error naming both + unit test
- [ ] 1.3 Test the wrapper acks on nil / naks on error (integration against a real
      server, tag integration)
- [ ] 1.4 File the sibling enforcement gap against `agentic-loop/config.go`

## 2. gated-dag config knobs

- [ ] 2.1 `config.go`: `DispatchStream`, `DispatchDurable`, `AckWait`,
      `MaxAckPending`, `HeartbeatInterval`, `StrandedAfter`, stream retention
- [ ] 2.2 `Validate()`: `HeartbeatInterval < AckWait`, non-negative durations + test

## 3. Durable publish + claim rollback

- [ ] 3.1 Provision the dispatch stream (`EnsureStream`, bounded `MaxAge`/`MaxMsgs`)
      at Start
- [ ] 3.2 `natsPublisher.Dispatch` → `PublishToStreamWithAck` (return error on
      non-ack)
- [ ] 3.3 `claimThenDispatch`: roll the claim back on a `Dispatch` error (mirror
      the claim-error rollback); `dispatch_publish_failures_total` metric; drop the
      "stranded until reset" comment + test (publish-fail re-arms the unit)

## 4. Stranded-unit stall detector

- [ ] 4.1 `stallAfterInflight`: a claimed unit older than `StrandedAfter` (claim
      timestamp) no longer suppresses the stall; fresh claimed units still read as
      in-flight; `StrandedAfter==0` disables + test (stranded alerts, fresh does not)

## 5. Shared decode helper

- [ ] 5.1 `gateddagexec.DecodeDispatch(data []byte) (*DispatchMessage, error)` +
      test (framework-side; consumers import it)

## 6. Spec + close

- [ ] 6.1 `openspec validate`; gates green (`go test -race`, `task lint`, schema
      no-drift); semstreams-reviewer; then archive → promote `gated-dag-dispatch`
      into `openspec/specs/`
