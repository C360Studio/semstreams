# SemSource walkthrough documentation evidence

This directory records the source inventory and review evidence for
[issue #1299](https://github.com/C360Studio/semstreams/issues/1299) and
[PR #1300](https://github.com/C360Studio/semstreams/pull/1300).
The [worked chapter](../../basics/09-building-semsource.md) is the adopter-facing document;
[inventory.md](inventory.md) records the bounded source investigation.

## Scope and source versions

The authorized change is Markdown documentation: one source-to-context chapter, README framing and capability
wording, and documentation-index navigation. Runtime behavior, configuration, public API contracts, capability
specs, CI, release gates, and sister repositories are outside this change.

Framework baseline: `bb08a2f29d174d85bac3c845bce5c07c9130ea92`.
SemSource baseline: `4093d3ce421371f4a99d7168e372552899bf6795`.
Its SemStreams dependency is beta.160, commit `8403a2218000e45a31c5132fbfe01af42ed04f14`.
The chapter describes source inspection and explicitly distinguishes those versions. It does not establish
SemSource compatibility with current framework main or successful execution of the adopter exercise.

The architect considered extending existing concept pages, adding one worked chapter with short entry-point edits,
and changing only the introduction. The selected worked chapter gives the source-to-context path a coherent home
while linking existing contracts. It stays within the owner's accepted documentation direction.

## Independent review

The SemStreams reviewer returned `INVENTORY PASS` for the bounded inventory at SHA256
`c479b6e20c5706188ab226307f76c56ef5295edbf20869848bf7c4d314e1c460`.
README/index line pins were subsequently refreshed for the edited text; the reviewer inspected that refresh.

The first documentation review found one HIGH issue: the proposed exercise repeated a raw-source-path query claim
that the pinned docs lens and beta.160 adapter do not implement together. The correction selects statistical or
semantic retrieval and uses passage prose. The chapter records the mismatch; a path is a citation, not a promised
lookup key. No runtime or sister change is represented as fixing it.

The reviewer then returned `APPROVE`, with no blocking or HIGH findings remaining, for the corrected prose.
The reviewed guide hash was `501c16fc4c032889cfdba413ac75ce7b714bd5d78db034c5173ab20ef6d9c038`;
the only subsequent guide change wraps the three long lines identified by that reviewer.

Final guide SHA256: `e38a76e2552db81818bcb8157408db509cc6c9c59ee065243d8a8558039216e7`.
README SHA256: `ede66918a5ec61b319ca8f323fee8996b1adcb1924e5d764d77676c9b0d9d644`.
Documentation index SHA256: `4830407538e5651df19ec21417e500295f53fc6afefc17309cb94afeee158fca`.
Inventory SHA256: `28944eaa5027dc393f69d0901b6d27306e24944c10e0869c846899602968ec79`.

## Local verification

The canonical documentation preflight selected these checks. The Go tree remained identical to the framework
baseline throughout; no runtime or configuration edits were made.

| Check | Result and actual coverage |
| --- | --- |
| `task build:default` | Exit 0; built the production SemStreams binary |
| `task lint` | Exit 0; vet, fmt, pinned revive, fixed-port guard and request-guard tests |
| `task inventory:verify -- docs/proposals/semsource-walkthrough/inventory.md` | Exit 0; all 23 local pins match |
| Focused documentation checks | Relative targets/anchors, reference definitions, immutable Git objects and line targets |
| Markdown checks | Heading hierarchy, language-tagged fences and guide prose wrapping |
| `git diff --check` and diff-scope inspection | Whitespace clean; only Markdown files changed |

The focused checker is a session-local helper at `/private/tmp/semstreams-1299-doc-check.py`; it is not a repository
runtime or a new CI gate. It checks immutable source targets against local Git objects, not remote HTTP availability.
Long immutable URL definitions, inventory quotations and table rows are preserved; guide prose is wrapped below
120 characters. Command results are recorded in the authoring session and summarized in the PR.

No SemSource build, live product request, model call, Docker integration suite, or edge benchmark was run locally
for this documentation change. Hosted checks and current task status belong to the PR and issue, not this record.
No OpenSpec delta or archive is needed because the change introduces no runtime or adopter contract.
