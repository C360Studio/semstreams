---
name: semstreams-preflight
description: Select and run existing SemStreams verification gates for a concrete diff, including integration-runner ownership and breaking-change E2E evidence. Use before implementation pushes or when assessing local readiness.
---

# Verify a SemStreams change

The [shared protocol](../../protocol.md) owns claims and landing; the
[testing policy](../../../docs/contributing/01-testing.md) owns test tiers and the integration runner.
Read both. This skill selects existing checks and records evidence; it grants no merge, tag or cleanup authority.
The protocol's initial claim push is distinct from an implementation push. Its documented delta-less OpenSpec
claim exception does not excuse another failure.

## Establish the diff and its owner

Work in the claim's worktree. Verify its branch, HEAD, dirty/untracked files, upstream and PR target before
choosing checks. Compare the committed diff against the actual PR base, including a non-default target in a
stack; also inspect the staged and unstaged work. Do not assume the discovery checkout's local `main` is current.
Before every commit or push, verify the branch still matches the claimed PR head.

## Select the existing gates

Inspect the [Taskfile](../../../Taskfile.yml), [test tasks](../../../taskfiles/test.yml),
[lint tasks](../../../taskfiles/lint.yml) and [CI workflow](../../../.github/workflows/ci.yml) when their exact
coverage matters. A task name or old successful log does not prove parity with current CI.

| Changed surface | Local verification |
| --- | --- |
| Documentation or skill instructions only | `task build:default`, `task lint`, and checks for the changed artifact: Markdown, skill frontmatter, relative links and `git diff --check` |
| Go behavior without an integration-dependent boundary | `task check`, `task test:race`, and `go test ./test/contract/...` |
| Framework packages, NATS/graph boundaries, validation/configuration, or integration tests | `task check:push`; it invokes the canonical integration runner |
| A breaking API, wire, registration or runtime change | The applicable gates above plus E2E coverage of the affected path, as required by the developer contract and testing policy before landing |

Use focused behavior tests during iteration. For focused integration, use the runner, for example:

```bash
scripts/run-integration-tests.sh ./processor/graph-index/...
```

Do not replace it with a direct tagged `go test`: the runner owns flags, Docker preflight, the shared host lock
and Reaper policy. `task test:integration` is additive; it includes ordinary and integration-tagged tests.
`task check:push` currently also runs a separate unit race pass. Report that actual coverage rather than
silently changing the command to deduplicate it.

For config/schema changes, generate and inspect the schema diff, commit intended changes, then require
`task schema:check-changes` to pass. For property or cited-spec changes, run `task spec:properties` and review
the invariant and generator against the clause. For OpenSpec changes, run `openspec validate --all --strict`
and read `task openspec:queue` in this worktree; an empty local queue does not describe other PR worktrees.

## Protect shared test infrastructure

Serialize heavy integration/E2E work on a shared host. The integration runner reports lock contention;
do not bypass its lock. Before an E2E run, identify other owners and check the selected tier's ports with
`task e2e:check-ports`. Docker inspection is diagnostic: container age, a timeout signature, or a name pattern
does not establish that a resource is abandoned or that the failure is infrastructure-only.

Inspect `docker system df` and container/Compose ownership when needed. Preserve other sessions' resources.
Cleanup is limited to an identified completed or abandoned run that this session is authorized to clean up;
host-wide pruning and stopping an unknown port holder are not preflight steps. The Claude `e2e-doctor` helper
provides additional diagnostics under the same ownership constraint.

## Check breaking-change coverage

Read the diff as well as titles/footers. Export removal, changed wire semantics, registration retirement,
or changed runtime behavior can be breaking even without a `!` marker. Follow the
[E2E guide](../../../docs/contributing/02-e2e-tests.md) and the actual tier assertions to choose coverage:

| Path | Starting tier |
| --- | --- |
| Rules, structural inference, graph mutation | `task e2e:structural` |
| BM25 and statistical search | `task e2e:statistical` |
| Embeddings and neural/LLM graph paths | `task e2e:semantic` |
| Agent loop and tools | `task e2e:agentic` |
| CRUD tool round trips | `task e2e:crud-tools` |
| Lifecycle harness | `task e2e:lifecycle` |

A tier name is a starting point, not proof that it exercises the changed path. A filed coverage gap does not
satisfy a required breaking-change gate. Follow the protocol's File ritual for uncovered work and report the hold.

## Report evidence and remaining gates

Record the tested HEAD (or dirty snapshot), exact command, exit status, assertions/tests actually exercised,
and log/artifact location. Separate failure, skip, no selected tests and compilation-only results. When the
implementation changes, older green results remain evidence for the older revision. Preserve the command's
failure status when filtering output; do not infer success from a quiet log.

Resolve failures from evidence; an isolated successful rerun is not a fix for a known required-job flake.
Apply the protocol's fix-or-recorded-waiver rule. Long or paid runs need the role contract's active progress
checks and bounded stopping behavior.

After local verification, assess the PR's current-head hosted checks and the protocol's review/archive gates.
Local green is not merge authorization. Follow existing user authorization without asking for it again, while
preserving all outstanding required gates. Release tags additionally require the
[release-candidate proof contract](../../../openspec/specs/release-candidate-proof/spec.md).
