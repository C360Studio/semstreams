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
      entries-without-predicates arm (unreachable through the reader, which drops empty predicates).
- [ ] 2.2 `task e2e:statistical` green on the branch; the #1117 path-only semantic run green or its reds filed
      with causes.
- [ ] 2.3 Review through the project reviewer contract; owner acceptance on #1426.

Landing tasks (archive, spec sync, ticks) live on the PR checklist per the #1230 ruling, not here.
