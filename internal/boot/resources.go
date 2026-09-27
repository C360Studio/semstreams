package boot

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/c360studio/semstreams/config"
	"github.com/c360studio/semstreams/natsclient"
	shutdownerrs "github.com/c360studio/semstreams/pkg/errs"
)

// rootResources is what the root owns outside the ServiceManager: the NATS
// transport, the config manager, the MaxDeliver observer, and the closers the
// responder extensions returned. close runs once, either from the ordered
// shutdown or from the bounded abort when boot returns early.
type rootResources struct {
	natsClient              *natsclient.Client
	configManager           *config.Manager
	stopMaxDeliveryObserver func(context.Context) error
	responders              []io.Closer
	closeAttempted          bool
}

func (r *rootResources) close(ctx context.Context) error {
	var closeErr error
	for _, responder := range r.responders {
		closeErr = errors.Join(closeErr, closeResponder(responder))
	}
	if r.stopMaxDeliveryObserver != nil {
		closeErr = errors.Join(closeErr, shutdownerrs.NewShutdownError(
			appName+"/max-delivery", shutdownerrs.PhaseDrainConsumers, r.stopMaxDeliveryObserver(ctx),
		))
	}
	if r.configManager != nil {
		closeErr = errors.Join(closeErr, stopWithinShutdownBudget(ctx, r.configManager.Stop))
	}
	r.closeAttempted = true
	return errors.Join(closeErr, shutdownerrs.NewShutdownError(
		appName, shutdownerrs.PhaseCloseTransport, r.natsClient.Close(ctx),
	))
}

func (r *rootResources) abortOnReturn(timeout time.Duration, runErr *error) {
	if r.closeAttempted {
		return
	}
	abortCtx, abortCancel := context.WithTimeout(context.Background(), timeout)
	defer abortCancel()
	*runErr = errors.Join(*runErr, r.close(abortCtx))
}
