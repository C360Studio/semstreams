package model

import "context"

// Watcher is the minimal surface model.Watch needs to subscribe to KV-
// driven registry updates. semstreams's config.Manager satisfies it via
// WatchModelRegistry(). External consumers can adapt other config
// sources by implementing the same one-method interface.
//
// The returned channel may emit nil — callers should guard their apply
// callback if they expect a non-nil registry at all times.
type Watcher interface {
	WatchModelRegistry() <-chan *Registry
}

// Watch keeps a caller-owned *Registry pointer in sync with KV-driven
// model_registry changes. It blocks the calling goroutine, calling
// apply for every new *Registry until ctx is cancelled or the watcher
// channel closes. Run it in a goroutine spawned by the caller:
//
//	var holder atomic.Pointer[model.Registry]
//	holder.Store(initialRegistry)
//
//	ctx, cancel := context.WithCancel(context.Background())
//	defer cancel()
//	go model.Watch(ctx, cfgMgr, holder.Store)
//
// This exists for external library consumers that maintain their own
// *Registry alongside the semstreams runtime. semstreams components
// (components in flow configs) don't need it — ComponentManager
// restarts them automatically when their factory declares
// component.DepModelRegistry. See docs/operations for the full guide.
//
// Watch never panics; nil watcher or nil apply returns immediately.
func Watch(ctx context.Context, watcher Watcher, apply func(*Registry)) {
	if watcher == nil || apply == nil {
		return
	}
	ch := watcher.WatchModelRegistry()
	for {
		select {
		case <-ctx.Done():
			return
		case reg, ok := <-ch:
			if !ok {
				return
			}
			apply(reg)
		}
	}
}
