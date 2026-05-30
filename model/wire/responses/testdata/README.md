# Responses wire-format fixtures

This directory holds golden JSON fixtures captured from live OpenAI
`/v1/responses` calls. They back the round-trip parity tests in
`types_test.go` and `client_test.go` so wire-shape regressions get
caught at unit-test speed without paying for an API call each run.

## Fixture inventory (target)

The Phase 1 doc-derived round-trip tests cover the request/response
shapes from public OpenAI documentation. The live fixtures land
during the Phase 4 PR cycle once
`openai_responses_live_test.go` is wired (the same test that hits
the live endpoint also harvests fixtures here on the first
run). Until then, the parity test for live shapes is stubbed and
skipped — see `types_test.go::TestResponses_GoldenFixture_Parity`.

Target fixtures (filenames stable):

- `request_simple_text.json` — minimal request with a single
  user message.
- `request_function_call_round.json` — request echoing a prior
  function_call + function_call_output + reasoning items.
- `response_simple_text.json` — minimal response with a single
  message output item.
- `response_function_call_with_reasoning.json` — response with
  reasoning item + function_call item (tool flow).
- `response_completed_text.json` — full response with usage +
  reasoning details.

## Capture protocol

When PR 4 lands, run:

```
go test -tags live_llm -run TestOpenAIResponses_CaptureFixtures \
    ./processor/agentic-model/...
```

with `OPENAI_API_KEY` set. The test issues a small set of canned
calls against both a Codex-class model and a GPT-5.5/o-series model,
serializes the wire request and response bodies, and writes them to
this directory. Re-run is idempotent — existing files are not
overwritten unless `CAPTURE_OVERWRITE=1` is set.

## Provenance

Fixtures are committed verbatim from live endpoints. Capture commit
SHA is recorded alongside each file in a sibling `.meta` file
(written by the capture test) for traceability when OpenAI evolves
the wire shape.
