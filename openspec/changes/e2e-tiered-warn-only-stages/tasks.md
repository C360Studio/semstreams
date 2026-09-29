# Tasks

## 1. Inventory and design

- [x] 1.1 Line-pinned inventory of the warn paths (eight named plus six sweep-found; fourteen total, plus the zero-enhanced
      arm), the variants each runs in, the client deadlines they hit, and every reader of the metrics they set.
- [x] 1.2 Per stage: assert, or leave the per-PR variants, with the reason; timeout distinguished from empty.
- [x] 1.3 Seed the tiered-scenario capability spec (lazily, this change is its first toucher) with the delta:
      a per-PR stage never passes on the outcome it exists to detect.

## 2. Delivery

- [x] 2.1 Implement the accepted per-stage decisions (the fourteen paths and the twelve round-1 sweep stages); each
      assertion has one mutation check (restore the warning path, see green; fix, see red) recorded in evidence.md,
      except these inspection-only arms: `validate-virtual-edges`' count-read and mismatch arms (the one the design
      allowed); `validate-llm-enhancement`'s wait-error and post-wait re-fetch arms; the failed COMMUNITY_SUMMARIES read
      in `validate-llm-enhancement` and `validate-community-structure` (H2); `validate-incoming-index-predicates`'
      entries-without-predicates arm (unreachable through the reader, which drops empty predicates); and, as a class
      (review rounds 2 and 3: eleven such arms revert to `return nil` without a red, evidence § 6 and § 7), the NATS
      read-error and empty-ID arms of the four NATS-seam stages
      (`validate-hierarchy-inference`, `validate-incoming-index-predicates`, `validate-bidirectional-traversal`,
      `validate-inverse-edges-materialized`) and `validate-entity-structure`'s sample read-error arm.
- [x] 2.2 `task e2e:statistical` green on the branch (CI `e2e statistical` on the code head `263b60e2`, E2E Ladder
      run 36641734740). The #1117 path-only semantic run is sequenced after this PR by the owner ruling on #1117
      (2026-09-29, verbatim "the warn-only slice #1426 (beta.163) lands first"): its first measurement is PR #1425's
      rebased run, and its reds are filed there.
- [ ] 2.3 Review through the project reviewer contract; owner acceptance on #1426.

Landing tasks (archive, spec sync, ticks) live on the PR checklist per the #1230 ruling, not here.
