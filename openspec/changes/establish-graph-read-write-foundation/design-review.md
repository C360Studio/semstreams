# Design review record

- **Reviewed artifact:** GS-01 revision 39, 619 lines and 50,995 bytes.
- **SHA-256:** `4399e4f50ffcfa90c32d12ff4667e5c3797150194ed509a7d01c9a5620c16c3e`.
- **Verdict:** `DESIGN REVIEW PASS`.
- **Review date:** 2026-08-05.
- **Owner decision:** all sixteen rulings approved; see [approval.md](approval.md).

The independent review verified both blocking corrections:

1. Atomic Create plus observed-revision CAS is bucket-wide across Graphable, RPC, and hierarchy writes. Unconditional
   existing-key `Put` paths are retired, and RPC handlers do not enter the keyed ingest pool or add coordination.
2. Hierarchy is retained only on Graphable ingest. Inferred containers are distinct from referential stubs, container
   birth is atomic, inverse writes use CAS, partial/dangling state is valid, and the structural E2E tier proves the
   chosen contract.

The same review confirmed conditional delete reports the matched expected revision rather than inventing a NATS delete
revision; rules retry one fresh read exactly once after a definite revision mismatch; mismatch metrics have bounded
labels; and the downstream census is communicate-only.
