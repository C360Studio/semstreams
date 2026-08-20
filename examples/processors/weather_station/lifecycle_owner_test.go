package weatherstation

import (
	"context"
	"errors"
	"testing"

	"github.com/c360studio/semstreams/pkg/errs"
)

func TestLifecycleOwnerStopBeforeStartIsTerminal(t *testing.T) {
	owner := &Component{}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if err := owner.Start(canceled); err == nil {
		t.Fatal("pre-canceled Start succeeded")
	}
	if owner.lifecycleUsed {
		t.Fatal("pre-canceled Start consumed lifecycle authority")
	}
	if err := owner.Stop(nil); err == nil {
		t.Fatal("Stop(nil) succeeded")
	}
	if owner.lifecycleUsed {
		t.Fatal("Stop(nil) consumed lifecycle authority")
	}
	if err := owner.Stop(t.Context()); err != nil {
		t.Fatalf("Stop before Start: %v", err)
	}
	if err := owner.Start(t.Context()); !errors.Is(err, errs.ErrAlreadyStarted) {
		t.Fatalf("Start after terminal Stop error = %v, want ErrAlreadyStarted", err)
	}
	if err := owner.Stop(t.Context()); err != nil {
		t.Fatalf("repeated completed Stop: %v", err)
	}
	if err := owner.Stop(nil); err == nil {
		t.Fatal("terminal Stop(nil) succeeded")
	}
}
