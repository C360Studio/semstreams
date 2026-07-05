# Tasks — websocket output pass-through (gh#471)

> Scoping change (Proposed). Tasks unchecked; implementation follows approval.

## 1. Config surface

- [x] 1.1 Add `Passthrough bool` to `Config` (`websocket.go:40`) with
      `json:"passthrough,omitempty"` + `schema:"type:bool,description:...,category:advanced,default:false"`.
- [x] 1.2 Add `Passthrough bool` to `ConstructorConfig`; default `false` in
      `DefaultConstructorConfig`.
- [x] 1.3 Add a `passthrough bool` field to `Output`; set it in `NewOutputFromConfig`.
- [x] 1.4 Wire the factory: read `cfg.Passthrough` into `ctorCfg.Passthrough`.

## 2. Handler paths

- [x] 2.1 Factor the passthrough decision into a small helper used by both handlers,
      e.g. `broadcastPayload(ctx, subject, data)`: if `w.passthrough && json.Valid(data)`
      → broadcast `data` unchanged; else run the existing decode/inject/re-encode
      (which itself falls back to the `raw_data` wrapper for non-JSON). This keeps
      the two paths from drifting.
- [x] 2.2 `handleNATSMessageData` uses the helper.
- [x] 2.3 `handleNATSMessage` uses the helper (same passthrough behavior on the
      `*nats.Msg` entrypoint).
- [x] 2.4 Confirm metrics (`messagesReceived`) and error accounting are preserved on
      the passthrough branch (count received; a `json.Marshal` error is impossible on
      the passthrough branch since we don't marshal).

## 3. Tests

- [x] 3.1 Passthrough ON + envelope-complete JSON: the broadcast bytes are the
      producer's ORIGINAL bytes — assert key order preserved and NO `timestamp`/
      `subject` injected (the two things the default path perturbs/adds).
- [x] 3.2 Passthrough ON + JSON lacking `timestamp`/`subject`: still broadcast
      unchanged (NOT injected) — locks the documented "producer owns its envelope"
      contract.
- [x] 3.3 Passthrough ON + non-JSON bytes: falls back to the `raw_data` wrapper
      (same as default), so the flag is safe on a mixed subject.
- [x] 3.4 Passthrough OFF (default): existing inject-when-absent behavior byte-for-byte
      unchanged (regression guard) — a JSON payload without `timestamp`/`subject`
      gets them injected.
- [x] 3.5 Cover BOTH handler entrypoints (data path + `*nats.Msg` path) so neither
      drifts.

## 4. Spec + gates + close

- [x] 4.1 `openspec validate --strict`.
- [x] 4.2 Gates: `go test -race ./output/websocket/...` (unit + integration),
      `task lint`, `task schema:generate` + `git diff schemas/ specs/` no-drift
      (the new bool field regenerates the ws component schema — commit it),
      `go vet -tags=integration`.
- [x] 4.3 semstreams-reviewer pre-merge — APPROVE-WITH-NITS. Addressed: the
      "byte-identical" overstatement (real win is key-order + numeric-precision;
      the shared envelope marshal still compacts/HTML-escapes) via reworded
      spec/proposal + `TestPassthrough_EnvelopeMarshalCompactsAndEscapes`; added
      `TestCreateOutput_PassthroughConfigRoundTrip` (operator JSON wire); LOW
      (bare-scalar/array passthrough divergence) documented in spec.
- [x] 4.4 Archive → promote `websocket-output` into `openspec/specs/`.
- [ ] 4.5 e2e:core green (core-dataflow exercises the default ws egress path). PR;
      CI green; merge; tag.
- [ ] 4.6 Confirm back to semboids on gh#471 (opt-in `passthrough: true`; producer
      owns its envelope).
