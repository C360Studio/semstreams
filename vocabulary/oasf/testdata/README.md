# `vocabulary/oasf/testdata/`

Snapshots of the upstream AGNTCY OASF schema files captured at the time
this package was scaffolded. The Go constants in `categories.go` are
pinned against these snapshots by `TestCategoriesMatchUpstream` in
`categories_test.go`, so any upstream renumbering or renaming surfaces
as a failing test rather than as a silent wire-format drift.

## Provenance

- Source repo: <https://github.com/agntcy/oasf>
- Source paths: `schema/skills/<category>/<category>.json`
- Captured at commit: `2447e3ed5a4f9236fa939748c4ce537522d637ab` (main, 2026-04-27)
- Capture date: 2026-05-14

## Refreshing the snapshots

When the upstream taxonomy changes and the test fails, re-capture from
the current `main` of `agntcy/oasf`:

```bash
cd vocabulary/oasf/testdata/categories
for cat in natural_language_processing analytical_skills \
           retrieval_augmented_generation security_privacy \
           data_engineering agent_orchestration \
           evaluation_monitoring governance_compliance \
           tool_interaction advanced_reasoning_planning; do
  gh api repos/agntcy/oasf/contents/schema/skills/$cat/$cat.json |
    python3 -c "import json,sys,base64; \
                print(base64.b64decode(json.load(sys.stdin)['content']).decode())" \
    > $cat.json
done
```

Then update the captured-at SHA above and re-run `go test ./vocabulary/oasf/`.
If a category's `uid` changed, also update the corresponding constant in
`vocabulary/oasf/categories.go`. If a category was deleted upstream, see
[ADR-042](../../../docs/adr/042-oasf-taxonomy-adoption.md) for the
deprecation strategy.

## Coverage

The 10 categories tracked here are the MVP set chosen in ADR-042 for
framework-substrate relevance. Other published OASF categories (computer
vision, audio, tabular/text, multi-modal, devops/MLOps) are intentionally
not snapshot-tracked until a concrete internal capability needs them.
