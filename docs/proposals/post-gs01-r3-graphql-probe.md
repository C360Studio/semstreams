# Post-GS-01 gqlgen executor probe — one canonical operation, all read-contract gates

> **DESIGN EVIDENCE ONLY.** This probe evaluates whether an established GraphQL library (gqlgen) satisfies the
> post-GS-01 design's read-contract proof gates for one canonical operation. It accepts no dependency, gateway
> design, or target state. It complements `/private/tmp/post-gs01-r3-graphql-executor-evaluation.md`.

## Baseline and identity

- Repository dependency baseline: `182f2f23` (origin/main, includes #898/#899/#900), via detached checkout
  `/private/tmp/semstreams-probe-dep`
- Probe module: `/private/tmp/post-gs01-graphql-probe` (own `go.mod`; `replace` to the local checkout; zero repo edits)
- Pinned versions: `gqlgen v0.17.94`, `gqlparser v2.5.36`, Go 1.26.4
- Hand-written probe code: schema 29 lines, resolver 61, model 10, tests 226 (= 326 total);
  generated executor 2,942 lines (excluded from the net-negative gate by the design's own wording)
- Source SHA-256: schema `eaac26d3…572aac2`, gqlgen.yml `3c02557b…8fcd66a5`, resolver `c717df13…c3111b`,
  model `db18d30e…b4d17757`, tests `38279ebb…9f79be49`

## Shape

The probe serves `entity(id: String!): ExactEntity!` — the design's §6.4 exact-read operation — through gqlgen,
with the resolver consuming the **real** `graph.ExactEntityReader` interface (the adapter seam the design admits)
and returning **real** framework types: `graph.ExactEntity`, `graph.EntityState`, `message.Triple` (including a
map-valued `Object`). A stub reader stands in for the NATS transport, which PR #898 already proved; every open
question in this probe is on the gqlgen side.

## Results — all six proof-gate categories pass

| Read-contract gate | Test | Result |
|---|---|---|
| Selection projection (only requested fields on the wire) | `TestSelectionProjectionOnWire` | PASS |
| Revision fidelity (2^53+1 wire-exact) | `TestUint64RevisionWireFidelity` | PASS |
| Variables, aliases, fragments + `Any` scalar over a real map-valued triple object | `TestVariablesAliasesFragments` | PASS |
| Conformant introspection; query-only (no mutation root) | `TestIntrospectionQueryOnly` | PASS |
| Validation rejects unknown fields **before any resolver runs** (replaces `graph.query.unknown`) | `TestValidationRejectsUnknownFieldBeforeExecution` | PASS |
| Typed error extensions + path + conformant null propagation | `TestErrorExtensionsPathAndNullPropagation` | PASS |

Wire samples (verbatim):

```json
{"data":{"entity":{"kvRevision":9007199254740993,"entity":{"id":"acme.edge.demo.system.sensor.001"}}}}
```

```json
{"errors":[{"message":"entity not found: acme.edge.demo.system.sensor.999","path":["entity"],
"locations":[{"line":1,"column":3}],"extensions":{"code":"entity_not_found",
"entity":"acme.edge.demo.system.sensor.999"}}],"data":null}
```

## Findings beyond pass/fail

1. **`Marshal<Type>` discovery collision (real, cheap to resolve).** Binding schema `Entity` directly to
   `graph.EntityState` fails codegen: gqlgen's marshaler discovery finds the framework's canonical
   `graph.MarshalEntityState([]byte, error)` and mistakes it for a custom scalar marshaler. Resolution: a
   gateway-local defined type (`type EntityModel graph.EntityState`) plus one field resolver — 10 lines + 5 lines
   per wrapped type, and arguably better layering anyway (the gateway owns its wire models). Only `EntityState`
   collides today; `message.Triple` and `graph.ExactEntity` bind directly.
2. **Introspection is an explicit opt-in extension** (`extension.Introspection{}`), not a default. The real
   gateway enables it deliberately as the capability-discovery surface replacing the deleted
   `graph.query.capabilities` subject — and can equally choose not to on hardened deployments.
3. **`Uint64` serializes as a bare JSON number.** The wire is exact (9007199254740993 survives byte-for-byte;
   Go `json.Number` consumers preserve it) but **JavaScript clients parse JSON numbers as float64 and will
   silently lose precision above 2^53**. Recommendation for the real increment: serialize `kvRevision` and
   `aliasCoveredThroughRevision` through a string-serializing scalar (a ~10-line custom marshaler) so the
   revision contract survives every client language. This should be pinned in the gateway's wire tests.
4. **Conformance tests must assert against raw HTTP bytes, not the gqlgen test client** — the client decodes
   through mapstructure (numbers become float64), which would mask exactly the fidelity issues gate testing
   exists to catch. The probe's `rawWireQuery` pattern is the right shape for the real conformance suite.
5. **Dependency footprint (runtime, non-stdlib):** gqlgen's own packages, `gqlparser/v2`, plus
   `agnivade/levenshtein`, `coder/websocket` (present in the module graph via the transport package; unused at
   runtime with POST/GET-only transports), `go-viper/mapstructure` (test client only), `google/uuid`,
   `hashicorp/golang-lru/v2`, `sosodev/duration`. Modest, and consistent with the evaluation artifact's estimate.

## Conclusion

gqlgen satisfies every read-contract proof gate the post-GS-01 design names, for a real canonical operation over
real framework types through the real adapter seam, at 326 hand-written lines against the ~1,962-line facade it
would replace. The two genuine integration frictions (marshaler-name collision, number-vs-string revisions) have
known ten-line resolutions that belong in the gateway increment's design notes. The evaluation artifact's
recommendation stands with higher confidence: **gqlgen, primary; hand-written executor unjustified.**
