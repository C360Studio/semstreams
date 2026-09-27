package e2eboot

import (
	"github.com/c360studio/semstreams/internal/boot"
	"github.com/c360studio/semstreams/internal/e2eslowconsumer"
)

// enableSlowConsumer runs the slow-consumer probe once the NATS client is
// connected and before config arbitration — the only window in which it can
// observe the connection's installed error callback.
func enableSlowConsumer(opts *boot.Options, _ string) {
	opts.AfterConnect = append(opts.AfterConnect, e2eslowconsumer.Run)
}
