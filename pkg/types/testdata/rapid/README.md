# `pkg/types/testdata/rapid/` — curated seeds vs. local debris

rapid writes a fresh `.fail` file into this directory **every time a property fails locally**, using the
same naming scheme as the files committed on purpose. Nothing in the filename distinguishes the two, so
this README is the register: **if a `.fail` file is not listed below, it is local debris — delete it, do
not commit it.**

Run mutation checks with `-rapid.nofailfile` so they never write here in the first place.

## Curated seeds in this directory

| File | Guards |
|---|---|
| `TestPropEntityIDByteBound/TestPropEntityIDByteBound-20260831140043-41866.fail` | The exact 256-byte acceptance boundary. Verified 2026-08-31: mutating `len(value) > MaxEntityIDBytes` to `>=` reproduces `256-byte canonical ID rejected` from this seed after 0 tests. |

## What a committed seed is, and is not

This seed was recorded while a **deliberate off-by-one mutation** was applied to production code, so it
is a *mutation-kill witness* — evidence that the property catches that defect class — not a defect ever
present on `main`.

`TestPropEntityIDByteBound` is a plain `rapid.Check` property, not a `t.Repeat` state machine, so its
recorded stream replays cleanly and silently on green code. It does **not** have the two erosion modes
that force `service/testdata/rapid/` to back its seed with a named deterministic test: no truncated
stream, and no `SampledFrom(actionKeys)` cardinality that shifts when an action is added. Leave it as
is.

Note that the *generator* is what actually holds this coverage — `TestPropEntityIDByteBound` draws half
its cases from a boundary-hugging range because rapid biases toward its generator's endpoints, not
toward domain boundaries it cannot know. A wide range alone let a `>=` mutation survive 100 cases.
