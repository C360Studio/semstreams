# R1 decomposed execution design approval

Status: **accepted**.

Owner approval was recorded on 2026-08-06 after the index result/API freeze was added and independently reviewed.

## Accepted artifacts

- `post-gs01-r1-decomposed-execution-design.md`
  - lines/bytes: 426 / 17,180
  - SHA-256: `e1d7c47898824b4bfdca33a4e53da75dd4d59af147315ba2871f2cbebe2c017f`
- `post-gs01-r1-roadmap-amendment.md`
  - lines/bytes: 193 / 9,034
  - SHA-256: `85c837aca8ccbf38483848f322c85aba929596f24f5e517b125b6bc42a883e5b`
- `post-gs01-r1-decomposed-execution-design-review.md`
  - lines/bytes: 74 / 3,869
  - SHA-256: `ed1fc0a4ae4cd87225ff8ca6d6728e07e84f56deb8e59891bebf6d0f5a8b15d2`

The accepted inventory remains:

- `post-gs01-r1-acquisition-lifecycle-retry-inventory.md`
  - lines/bytes: 487 / 35,930
  - SHA-256: `b5bb0fa79f584a7ec8e06965d9885b9cd87629791f0accd620d5043c2bbfc22c`

The independently reviewed composite R1 design remains superseded evidence and is not implementation authority.

## Accepted rulings

1. R1a truthfully retains lifecycle `ListKeys`/`Watch`/`WatchAll`; R1b atomically deletes `WatchAll` and the
   Manager-wide guard.
2. Execute linearly: R1a → R1b → R1c → R1d → R1e.
3. Track the gated-DAG watcher-loss coverage gap under the foundation program, assigned to `@cglusky`, targeting
   `task e2e:structural`.
4. Apply the mandatory repository-first pattern census and extraction gate to every slice.
5. Freeze index result/API contracts through R1. A required change stops the slice, records a falsification, and moves
   to its owning R3–R6 increment without a preparatory abstraction or dual contract.
6. Add no compatibility shim, deprecated path, alternate spelling, or speculative universal runtime.

## Authorization

R1a implementation is authorized against the accepted identities above. Each later slice still requires its own
architect inventory, TDD proof, independent review, baton update, and designated verification before merge.
