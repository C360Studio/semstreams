# Migration notes — v1.0.0-beta.164 → v1.0.0-beta.165

SemStreams-owned record of what each downstream product must do to adopt the beta.165 wave. Sister repositories are
**read-only** to SemStreams agents: every obligation is recorded here and linked from the landing PR. One `##` section
per landing; later landings append their own sections below.

## Rule processor bounded Stop (#1283)

### What changes

A bounded `Stop(ctx)` on a rule processor no longer blocks past its context when Stop closes runtime admission at the
instant the processor's runtime ends. The cron scheduler's Stop is now bounded by its caller's context and follows the
framework's `Stop(ctx context.Context) error` shape: a nil context is rejected before any action, a completed repeated
Stop returns nil, and a Stop concurrent with one in progress returns a transient error. The hand-rolled settlement
Context it used to return is gone; there is no compatibility shim.

| Surface | Before | After | Adopters found (21 sister checkouts, 2026-09-28) |
|---|---|---|---|
| `rule.CronScheduler.Stop` | `Stop() context.Context` | `Stop(ctx context.Context) error` | 0 |
| `rule.Processor.ApplyConfigUpdate` | unchanged signature | unchanged; its doc now states it blocks until the running processor applies the update, Stop fences admission, or the runtime ends — never past a bounded Stop | — |
| `rule.Processor.UpdateWatchBuckets` | unchanged | unchanged; same doc statement | — |
| `rule.ConfigManager` | unchanged | unchanged | — |

### The one obligation

A product that stops a `CronScheduler` directly replaces `<-scheduler.Stop().Done()` (or a select on it) with
`err := scheduler.Stop(ctx)`, passing the shutdown context that bounds it. No sister repository was found doing so.
