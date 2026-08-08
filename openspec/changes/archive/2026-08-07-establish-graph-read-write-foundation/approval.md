# Approval record — graph read/write foundation

- **Owner decision:** approved in the Codex owner task on 2026-08-05 and amended there on 2026-08-05.
- **Approved design:** GS-01 revision 39, “mutation authority without semantic ownership,” including the explicit
  per-subject append outcome union added during implementation review.
- **Design SHA-256:** `9c6913ad558205b89c4197bb813228f40133432364698e1413666df8fe11f161`.
- **Approved artifact size:** 632 lines, 51,822 bytes.
- **Design review:** both the original artifact and the exact amended artifact received independent
  `DESIGN REVIEW PASS` verdicts.
- **Implementation plan:** approved in the same owner task on 2026-08-05.

The artifact identity above includes both explicit owner amendments: use an idiomatic discriminated Go result for
every append subject, and make exactly one framework request/reply mutation attempt. Components may choose their own
retry policy after observing the classified result; the framework does not retry automatically. The earlier
`64d09967...` identity recorded only the append-result amendment and is superseded because it still prescribed an
automatic retry rejected by the second owner ruling.

The approved delivery shape is one documentation/specification foundation-record PR followed by one draft runtime
cutover PR. Runtime slices may be reviewed independently, but no partially migrated breaking wire contract may merge
to `main`.

This repository record is the durable adoption evidence. It does not broaden the 16 owner rulings, authorize a
compatibility layer, or revive recovery, CQRS, semantic ownership, leader election, or downstream implementation work.
