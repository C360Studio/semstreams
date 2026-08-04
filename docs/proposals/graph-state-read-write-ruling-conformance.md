# GS-00 ruling conformance evidence

This maps accepted decisions to exact GS-00 evidence. “Bound” means GS-00
scheduled a falsifiable gate without claiming later runtime conformance.

Reference key:

- `P:n-m` means `docs/proposals/graph-state-read-write-program.md:n-m`.
- `D:n-m` means `openspec/changes/establish-graph-state-foundation/design.md:n-m`.
- Owner approval is `docs/proposals/graph-state-read-write-decision.md:289-291`.

## ADR-090 numbered decisions

| Decision | Exact evidence | GS-00 disposition |
|---|---|---|
| 1 Authority/recovery | P:49-51,444-452; D:19-48 | GS-01; runtime pending |
| 2 No event sourcing/CQRS runtime | P:27-31; D:9-15 | Binding posture |
| 3 Role-specific obligations | P:262-286,453-481; D:136-179 | GS-01 through GS-10 |
| 4 Delete unused views | P:74-85,366-387,453-460,487-488 | Delete/retain gates scheduled |
| 5 Single-active default | P:284-286,444-471,550-553 | Owner-by-owner proof scheduled |
| 6 Adopter read defaults | P:126-150,404-412,644-647; D:102-134,243-248 | GS-12 |
| 7 Typed writes/internal transport | P:55-121; D:84-98 | Two seams in GS-02 |
| 8 Effect-free rebuild/inference/restore | P:444-481,546-558; D:193-213 | GS-01, GS-04–GS-10 |
| 9 Three-owner runtime gate | P:288-314; D:179-203 | No shared runtime admitted |

## Eight accepted owner rulings

| Ruling | Exact evidence | GS-00 disposition |
|---|---|---|
| 1 Authority/recovery | P:49-51,444-452; D:41-48 | Read, restore, startup, instance proof |
| 2 Capability survival | P:74-85,366-387,404-412,453-460,487-488 | Full disposition chain |
| 3 Obligations by role | P:262-286,453-481,542-545; D:136-179 | Every role gets an owner home |
| 4 Runtime instance model | P:284-286,444-471,550-553 | Ingest first, then each owner |
| 5 Reads | P:644-647 | DEVIATION (owner-approved): embedded-client promise superseded; GraphQL portion retained |
| 6 Public write defaults | P:55-121,451-452; D:84-98 | Typed seams and package removal |
| 7 Inference ownership | P:472-481,554-558; D:193-203 | GS-10 effect/ownership proof |
| 8 Clean rebuild/proof | P:469-481,546-558; D:193-213 | Scoped rebuild and stale-key proof |

Ruling 5's full design evidence is P:126-150,404-412,575-577. Its deviation is
owner-approved at P:644-647. All other rulings map without deviation.
