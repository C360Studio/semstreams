package agenticdispatch

import (
	"context"
	"sync/atomic"
)

// commandEffect is one delivery's record of what its command handler actually
// did to the world. It exists because the settlement decision in handleCommand
// is about THIS delivery's effect, not about the shape of the request that
// produced it: a bare /cancel that published a signal cannot be replayed,
// while /help, /loops, a bare /status and the three arms of bare /cancel that
// publish nothing (no active loop, gate refusal, already settled) can — they
// resolved a target and then did nothing with it.
//
// The fact is recorded AT the publish site (commands.go:185) so it cannot drift
// from what happened. Inferring it from the command name would be a second
// spelling of "which commands publish", and inferring it from response text
// would be a parser over prose.
//
// It rides the context rather than the CommandHandler signature because
// processor/agentic-dispatch is Tier 1 (release/tier1-packages.txt:74) and
// CommandHandler is exported. What binds is ADR-106 RC-4: the incompatible Tier 1
// change count must descend to zero and stay there 30 days, and a return-value
// change adds one to it. CI would not have caught it — the apidiff job runs
// continue-on-error with API_COMPAT_MODE=report (.github/workflows/ci.yml:236-238,
// taskfiles/apicompat.yml:9-12), so the count is a governed number, not a gate.
// The break would be real but narrower than "every adopter": a handler literal
// passed to CommandRegistry().Register would stop compiling, while the
// CommandExecutor path adapts inside this package (component.go:1565-1569). None
// of it buys anything, because the framework's own publish site already knows the
// fact. The value is per delivery, never shared.
type commandEffect struct {
	signalPublished atomic.Bool
}

// commandEffectKey is a private type, so no other package can collide with or
// read this value.
type commandEffectKey struct{}

// withCommandEffect attaches a fresh recorder for one command delivery and
// returns it alongside the derived context.
func withCommandEffect(ctx context.Context) (context.Context, *commandEffect) {
	effect := &commandEffect{}
	return context.WithValue(ctx, commandEffectKey{}, effect), effect
}

// noteSignalPublished records that this delivery put a signal on the wire. A
// call with no recorder attached — the HTTP sync lane, which settles no
// delivery — is a no-op rather than an error.
func noteSignalPublished(ctx context.Context) {
	if effect, ok := ctx.Value(commandEffectKey{}).(*commandEffect); ok {
		effect.signalPublished.Store(true)
	}
}

// signalled reports whether the handler published a signal on this delivery. A
// nil recorder answers false, so an unattached path cannot quarantine.
func (e *commandEffect) signalled() bool {
	return e != nil && e.signalPublished.Load()
}
