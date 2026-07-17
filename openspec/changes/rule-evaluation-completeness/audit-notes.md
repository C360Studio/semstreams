# Grammar-collision audit — `.value` suffix (gh#519)

House rule: grep every `$`-prefixed token regex in the repo before landing a new
substitution suffix, confirm no existing pattern is broadened, and record the result.

## Command

```bash
grep -rn 'regexp\.\(MustCompile\|Compile\)(`[^`]*\\\$' --include="*.go" .
```

## Every `$`-token regex found, and disposition

| File:line | Pattern | Broadened by `.value`? | Disposition |
|---|---|---|---|
| `processor/rule/execution_context.go:34` (`unresolvedTemplateVarRe`) | `\$(?:entity\|related\|state\|schedule\|caller\|message)\.[a-z0-9][a-z0-9_.-]*` | No — matches any surviving `$entity.*` token including `...value`; unaffected by adding a new suffix upstream of it | No change. A `.value` token that fails arity disambiguation (falls back to literal-predicate handling) and stays unresolved is expected to still match this and warn — that's the unchanged bare-form behavior the spec requires. |
| `processor/rule/execution_context.go:54` (`tripleLengthRe`) | `\$(entity\|related)\.triple\.([a-z0-9.-]+?)\.length\b` | No — anchored on `.length`, disjoint suffix | No change. |
| `processor/rule/execution_context.go:87` (`tripleTriplesRe`) | `\$(entity\|related)\.triple\.([a-z0-9.-]+?)\.triples\b` | No — anchored on `.triples`, disjoint suffix | No change. |
| `processor/rule/execution_context.go:113` (`tripleValueRe`, **new**) | `\$(entity\|related)\.triple\.([a-z0-9][a-z0-9.-]*)\.value\b` | N/A — this is the new pattern | New; arity-disambiguated in Go code (`vocabulary.ParsePredicate`), not by the regex alone. |
| `processor/rule/typed_substitution.go:79,83` (`typedEntityTripleRe`, `typedRelatedTripleRe`) | `^\$entity\.triple\.([\w.-]+)$` / `^\$related\.triple\.([\w.-]+)$` | No — fully anchored (`^...$`), only matches when the ENTIRE `action.Object` template is exactly one token. A `.value`-suffixed template (e.g. `"$entity.triple.a.b.c.value"`) still matches this regex (capturing the literal string `"a.b.c.value"`), but `SubstituteVariablesTyped` only succeeds if a triple is LITERALLY stamped with predicate `"a.b.c.value"` — same documented behavior as the existing `.length`/`.triples` divergence (see file's package doc, "Two known semantic divergences"). No presence → `(nil, false)` → caller falls back to the string path, which is where the new arity disambiguation actually resolves `.value`. | No change needed; documented as consistent with the existing `.length`/`.triples` divergence, not a new one. |
| `processor/rule/typed_substitution.go:89` (`typedMessageRe`) | `^\$message\.(\w+(?:\.\w+)*)$` | No — different namespace (`$message`, not `$entity.triple`/`$related.triple`) | No change. |
| `processor/rule/message_substitution.go:54` (`messageTokenRe`) | `\$message\.[\w]+(?:\.[\w]+)*` | No — different namespace | No change. |
| `config/env.go:20` (`envVarRe`) | `\$\{([^}:]+)(:-([^}]*))?\}\|\$([A-Z_][A-Z0-9_]*)` | No — shell-style env-var interpolation, unrelated grammar (uppercase-only, braces) | No change. |
| `internal/entityidaudit/audit.go:74` (`intentionalTemplateRE`) | `^\$(?:entity\|related\|state\|schedule\|caller\|message)\.[a-z0-9][a-z0-9_.-]*$` | No — same shape as `unresolvedTemplateVarRe`, used to recognize "this is an intentional template token, not a real entity ID" in an unrelated audit tool. Matches `.value` tokens the same way it already matches `.length`/`.triples` tokens (as an opaque intentional-template shape) | No change. |
| `internal/predicateaudit/audit.go:39` (`substitutionRE`) + `substitutionCandidates` | `\$(?:entity\|related)\.triple\.([A-Za-z0-9_.-]+)`, then `strings.TrimSuffix(..., ".length")` / `".triples")` | **Yes — real gap found.** This feeds `task predicate:audit` (`vocabulary.ParsePredicate` validation of every extracted candidate across `.go`/`.json`/`.yaml`/structured-text source). Before this change, a `.value`-suffixed reference (e.g. `$entity.triple.openspec.change.revision.value`) would have its full 4-segment string handed to `ParsePredicate`, which always fails arity — a false-positive audit finding the moment any source/config adopts `.value`. | **Fixed**: added `predicate = strings.TrimSuffix(predicate, ".value")` alongside the existing `.length`/`.triples` strips. Same known/accepted limitation as the pre-existing `.length` strip: a predicate genuinely NAMED with a literal `.value` trailing segment would be mis-stripped (no production predicate does this today). Verified via `go run ./cmd/predicate-audit .` — passes with the current corpus (467 candidates, 0 findings) both before and after the fix (no config/source in this change actually uses `.value` outside `_test.go`, which the audit walker skips). |
| `test/reference_configs_test.go:43` (`tripleRefRe`) | `` \$(?:entity\|related)\.triple\.([\w.]+?)(?:\.(?:length\|triples)\b\|[^\w.]\|$) `` | **Yes — same class of gap.** `TestReferenceConfigs_AllTripleRefsResolveToKnownPredicates` walks `configs/rules/**/*.json` and cross-checks every extracted `$entity.triple.*`/`$related.triple.*` reference against the framework-stamped-predicate allowlist. Without a `.value` strip, a reference config using `.value` would have its predicate captured WITH the `.value` suffix attached, fail the "is this a known predicate" check, and require a bogus allowlist entry. | **Fixed**: added `value` to the suffix alternation (`(?:length\|triples\|value)`). Verified `TestReferenceConfigs_AllTripleRefsResolveToKnownPredicates` still passes (no shipped reference config uses `.value` yet — this is a preventive fix for the next pack that does). |

## Conclusion

No EVALUATION-TIME substitution regex is broadened by the new `.value` suffix — each
existing pattern is either disjoint (anchored on a different literal suffix or
namespace) or, for `typed_substitution.go`'s fully-anchored single-token regexes,
already carries the same documented divergence as `.length`/`.triples` (full-token
match against a literal predicate name, falls back to the string path on no match).

Two DOWNSTREAM AUDIT TOOLS (not part of the substitution grammar itself, but
consumers that parse `$entity.triple.*` strings to validate predicate identity) had
a real gap — both only stripped `.length`/`.triples`, not `.value` — and have been
fixed in this change: `internal/predicateaudit/audit.go` (`task predicate:audit`)
and `test/reference_configs_test.go` (`TestReferenceConfigs_AllTripleRefsResolveToKnownPredicates`).
Both fixes carry the same documented "syntactic strip, not full arity
disambiguation" caveat the `.length` strip already accepted — i.e. they inherit an
existing, not new, class of imprecision.
