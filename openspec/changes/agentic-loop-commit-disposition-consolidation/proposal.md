# Change: loop commit and disposition consolidation — bounded inventory and design, not a build

## Why

Ruled by the owner on #1146 (Q4, issuecomment-5854830449; Q7, issuecomment-5856142786) and filed as #1405. The
#1146 follow-through left one terminal fact with four durable homes — `COMPLETE_<loopID>` by create-once, the graph
stamp, the event on the wire, the loop record by compare-and-swap (`processor/agentic-loop/terminal_owner.go:166-206`
at `078782b1`) — and every accepted window (W1, W2, W3; 15 of the 67 inventory rows at #1146
issuecomment-5854802835) is a gap between two of those writes. Around them, locally reasonable changes repeated the
same decisions without meeting. Census at `078782b1`, `processor/agentic-loop` non-test (#1146
issuecomment-5854803094 § 1, § 4): 18 hand-ordered publish+KV-write sites (5 distinct orders, 5 gap policies); 15
"loop not held → read record → decide" cold arms in 8 files; `ErrLoopNotFound` decided at 21 sites in 5 files; 3
adopt functions plus the marker adopt; 7 tokens for "this delivery is inapplicable" (42 uses); 3 drop-metric families
for one event; 52 `WrapFatal` sites feeding one lane latch; 4 ad hoc test hooks. The typed `DeliveryDecision`
(`natsclient/delivery_settlement.go:17-30`) and `settleTerminalGuard` already exist, so part of this is inconsistent
use of existing owners (Codex's qualification, accepted).

Ruling 4 (#1146 issuecomment-5828511934) forbids *adding* durable authority inside a bug-fix child; it does not forbid
a design whose purpose is to *remove* authority. This change is that design, as its own change with its own docket —
never a child of a bug fix — and the docket's cheaper row is still written first.

## What changes

Nothing is built under this change. It delivers, in order:

1. A line-pinned surface inventory (`inventory.md`; `base:` the sha it was enumerated at; `task inventory:verify`
   green), then an independent inventory review to `INVENTORY PASS`.
2. After `INVENTORY PASS`, a design (`design.md` with § 0 rulings verbatim; options with costs, including doing
   nothing; every premise with its measurement; the docket with the cheaper row first; the acceptance numbers
   before → projected after), then an independent pre-owner design review.
3. The design posted on #1405 for the owner's ruling. Implementation, if any, is ruled on the design's docket
   afterwards. No watch migration.

## Scope, inventory first (architect contract categories 2 and 5)

1. Which of the census hits are true duplicates and which are legitimate (e.g. the terminal owner's own record
   read). Readers AND writers named for each durable home of the terminal fact, in this tree and in the sisters
   (read-only): semspec's key scan (`cmd/semspec/watch_live.go:263`), the dispatch `/activity` SSE
   (`processor/agentic-dispatch/http.go:1017`), semteams via that SSE, semsage.
2. Whether the terminal fact can have fewer durable homes — record as the commit with marker and event replayed
   from it; or marker+record with the event derived — and what each costs the sisters (the migration table sizes
   it; it never gates the design). The KV-watch contract (`docs/concepts/03-streams-vs-kv-watches.md:121`) applies
   to any derived owner.
3. One "resolve loop authority" step (held → warm; not held → record → cold / adopt / inapplicable) that every lane
   calls, if the inventory shows the arms are duplicates.
4. Consistent use of `DeliveryDecision` and the terminal guard so that "this delivery is inapplicable" never
   reaches "this lane is unsafe".
5. W1's disposition, decided by the owner on this evidence: fixed by the design, or an explicitly accepted bound
   with its reason.

## Acceptance test (the owner's words, 2026-09-27)

Idiomatic, pragmatic, simple, detailed without being complex. The design succeeds only if the code after it is
smaller and reads as ordinary Go, with no new interface layer: a terminal is one function with one ordered set of
effects a reader can hold in their head; "do I hold this loop, or do I read its record" is asked in one place;
delivery outcomes use the disposition type that already exists; fewer lines, fewer distinct orders, fewer words for
the same decision. The design states these as numbers (before → projected after). A proposed consolidation that adds
an indirection or grows the package fails its own test, and the design says so rather than building it.

## Done when

The inventory file is on the branch and `task inventory:verify` is green; the design (proposal, design.md with § 0
rulings verbatim, the docket with the cheaper row first, the acceptance numbers) is posted for the owner's ruling on
#1405; nothing is built under this change.

## Predecessors

#1399 (PR #1402, `73ea4f26`) and #1400 (PR #1403, `3dc4ccbe`) landed first and independently; this design reads the
tree at `3dc4ccbe` or later.
