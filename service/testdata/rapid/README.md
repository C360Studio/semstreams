# `service/testdata/rapid/` — curated seeds vs. local debris

rapid writes a fresh `.fail` file into this directory **every time a property fails locally**, using the
same naming scheme as the files committed on purpose. Nothing in the filename distinguishes the two, so
this README is the register: **if a `.fail` file is not listed below, it is local debris — delete it, do
not commit it.**

Run mutation checks with `-rapid.nofailfile` so they never write here in the first place.

## Curated seeds in this directory

| File | Guards |
|---|---|
| `TestPropStopAllShutdownContract/TestPropStopAllShutdownContract-20260831140109-41933.fail` | The shortest sequence distinguishing "`ErrAlreadyStopped` is clean success" from "it is a failure": register, arm a genuine failure, register a service with an `ErrAlreadyStopped` repeat style, `StopAll` twice. See the `#` header inside the file. |

## What a committed seed is, and is not

These seeds were recorded while a **deliberate mutation** was applied to production code, so each is a
*mutation-kill witness* — evidence that a property catches a specific defect class — not a defect that
was ever present on `main`.

A seed is **not** the coverage. It is a byte stream, and for a `t.Repeat` state machine it erodes two ways:

- The recorded stream **ends where the failure was**, so a passing run cannot consume it to the end.
  `[rapid] fail file ... is no longer valid` under `-v` on green code is **expected, not a regression**.
- The stream is **positional**. Adding or renaming any action changes `SampledFrom(actionKeys)`
  cardinality and silently re-decodes the same bytes into a *different* sequence, with only that
  `-v`-only log line as signal.

So every curated seed here must also have a **named deterministic test** carrying its coverage. For the
seed above that is `TestStopAllRetryAfterFailedPassTreatsAlreadyStoppedAsClean` in
`service/service_manager_prop_test.go`. The seed is retained as the provenance record of how the case
was found.

(A plain `rapid.Check` property does not have the truncation problem — its stream replays cleanly. See
`pkg/types/testdata/rapid/README.md`.)
