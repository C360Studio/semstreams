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
// processor/agentic-dispatch is Tier 1 (release/tier1-packages.txt:36) and
// CommandHandler is exported: adding a return value would break every adopter
// that registers a command, to carry a fact the framework's own publish site
// already knows. The value is per delivery, never shared.
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
