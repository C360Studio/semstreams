package graphindexspatial

import (
	"context"

	"github.com/nats-io/nats.go/jetstream"
)

type minimalEntityStatesWatcher struct{}

func (minimalEntityStatesWatcher) WatchAll(context.Context, ...jetstream.WatchOpt) (jetstream.KeyWatcher, error) {
	return nil, nil
}

var (
	_ entityStatesWatcher = minimalEntityStatesWatcher{}
	_ interface {
		WatchAll(context.Context, ...jetstream.WatchOpt) (jetstream.KeyWatcher, error)
	} = (entityStatesWatcher)(nil)
)
