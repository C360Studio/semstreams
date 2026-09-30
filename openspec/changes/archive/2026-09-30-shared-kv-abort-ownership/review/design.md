# Bounded abort experiment design review

Reviewer: independent semstreams-reviewer gh1421_inventory_review.
Verdict: DESIGN REVIEW PASS.
Reviewed design SHA-256: 546a27e74a75d41383a76fb31e3e081851c691a84e88f790f7b56495d5b23a0d.

The new question is useful because the earlier facade and rescue drain are absent. Real NewKVStore, exact native
channel, natural child expiry and synchronous Stop remain. The containment envelope fits: setup20s + two50s child
envelopes + fixture cleanup30s =150s inside180s. Implementation must reserve15s Close and reporting time inside
cooperative deadlines. The existing witness helper does not reject multiple matches: require fresh-child,
single-listing attribution and reject ambiguous evidence. Bound stdout and stderr and retain early-failure evidence.
Process exit never proves native joining. Focused proofs and classification mutation establish diagnostic fidelity,
not historical causation. No production repair, merge authority or additional native run is approved.
