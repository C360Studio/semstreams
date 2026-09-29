# Diagnostic implementation review — round 2

Reviewer: semstreams-reviewer, /root/gh1421_inventory_review, 2026-09-29.
Source SHA-256: c2975cce25a7a45021a6055f6d00cbaacdee215671112f9bf348d50ccfa4a2ac.

The prior three findings are resolved. Parent has a 27-second trigger and bounded three-second join without live
output reads. Case owner uses one 18-second/8-second work/terminal budget with stoppable facade, release/adoption
and joined results. Native witness gate checks exact functions, chan-send state and live framework context.
The corrected-source matrix and both mutation records are valid; cp backup checksums match.

HIGH source:782 — Focused early-finalizer proof starts facade.forward before lexical recovery. A failed nativeFirst
gate calls Fatal before owner.finish and leaks test-owned work. Register deferred checked owner.finish immediately
after construction, before starting forwarding. Normal explicit finish remains idempotent. A focused proof rerun
suffices if native owner/case behavior stays identical; no new native matrix is needed.

MEDIUM source:501 — facade_join labels the completion as native Keys delivery, but observes test facade.forwardDone.
Rename the label to test facade forwarding goroutine joined, or explicitly correct the semantics in retained evidence.
The existing native_after_release observation remains separate.

Corrected restored evidence: exact control set 1,024; native send witnesses before expiry around 28 ms; typed nil
error snapshots around 28 ms for cancellation and 5.007 s for the default deadline. Client.Close succeeds around
11.5–12.3 ms. Immediate native watcher presence is neither persistent-leak nor join evidence. Stop-delegation and
release mutations reach their intended assertions with owned completion; no package timeout is used as detection.
The historical fifteen-second drain failure is not reproduced. EOF is established by Wait completion, not an early
stdout-EOF observation while a child stays alive.

CHANGES REQUESTED — the small proof-fixture lexical cleanup fix. No reviewer tests or edits.
