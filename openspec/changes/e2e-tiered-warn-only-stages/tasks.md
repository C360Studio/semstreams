# Tasks

## 1. Inventory and design

- [ ] 1.1 Line-pinned inventory of the eight warn paths (six stages plus virtual-edges and the zero-enhanced
      arm), the variants each runs in, the client deadlines they hit, and every reader of the metrics they set.
- [ ] 1.2 Per stage: assert, or leave the per-PR variants, with the reason; timeout distinguished from empty.
- [ ] 1.3 Seed the tiered-scenario capability spec (lazily, this change is its first toucher) with the delta:
      a per-PR stage never passes on the outcome it exists to detect.

## 2. Delivery

- [ ] 2.1 Implement the accepted per-stage decisions; each assertion has one mutation check (restore the
      warning path, see green; fix, see red) recorded on the PR.
- [ ] 2.2 `task e2e:statistical` green on the branch; the #1117 path-only semantic run green or its reds filed
      with causes.
- [ ] 2.3 Review through the project reviewer contract; owner acceptance on #1426.

Landing tasks (archive, spec sync, ticks) live on the PR checklist per the #1230 ruling, not here.
