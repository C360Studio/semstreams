package rule

import (
	"context"
	"runtime"
	"testing"
	"time"
)

// newProcessorOwnerProof uses the existing NATS-free accepted-Start seam, but
// gives the real Processor.Stop a concrete fixture owner from acquisition.
func newProcessorOwnerProof(operationCtx context.Context, t *testing.T, transfer bool) *processorTestOwner {
	t.Helper()
	cfg := mustTestConfig(t, "rule-owner-proof")
	processor, err := NewProcessor(nil, &cfg)
	if processor == nil {
		t.Fatalf("NewProcessor returned nil: %v", err)
	}
	owner := newProcessorTestOwner(processor)
	defer owner.provisionalFinish(operationCtx, t)
	if err != nil {
		t.Fatal(err)
	}
	if !transfer {
		return owner // Exercise the helper's provisional setup-escape finalizer.
	}
	startCtx := owner.startContext(operationCtx)
	runCtx, startDone, err := processor.beginStartAuthority(startCtx)
	if err != nil {
		t.Fatal(err)
	}
	runtimeDone := processor.runtimeDone
	go func() {
		processor.commandLane.run(runCtx)
		close(runtimeDone)
	}()
	if err := processor.finishStartAttempt(startCtx, startDone, true, nil); err != nil {
		t.Fatal(err)
	}
	owner.transfer()
	return owner
}

func TestProcessorTestOwnerProvisionalAndTransferredNativeStop(t *testing.T) {
	t.Run("setup escape", func(t *testing.T) {
		operationCtx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		owner := newProcessorOwnerProof(operationCtx, t, false)
		if !owner.attempted || !owner.processor.terminal {
			t.Fatal("provisional setup escape did not reach concrete Processor.Stop")
		}
	})

	t.Run("caller lexical finalizer", func(t *testing.T) {
		operationCtx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		var owner *processorTestOwner
		func() {
			owner = newProcessorOwnerProof(operationCtx, t, true)
			defer func() {
				select {
				case <-owner.processor.commandLane.done():
				default:
					t.Error("substrate marker ran before the Processor lane joined")
				}
				if !owner.attempted || !owner.processor.terminal {
					t.Error("substrate marker ran before concrete Processor.Stop")
				}
			}()
			defer owner.finish(operationCtx, t)
			completed := make(chan error, 1)
			completedObserved := false
			defer func() {
				if !completedObserved {
					joinCtx, cancelJoin := context.WithTimeout(context.WithoutCancel(operationCtx), 5*time.Second)
					defer cancelJoin()
					select {
					case <-completed:
					case <-joinCtx.Done():
						t.Error("test-owned Processor command did not complete before lexical finalizer")
					}
				}
			}()
			go func() {
				completed <- owner.processor.submitRuntimeCommand(func(ctx context.Context) error {
					return ctx.Err()
				})
			}()
			var commandErr error
			select {
			case commandErr = <-completed:
				completedObserved = true
			case <-operationCtx.Done():
				t.Fatal("admitted Processor command did not complete before operation deadline")
			}
			if commandErr != nil {
				t.Fatalf("admitted Processor command: %v", commandErr)
			}
		}()
		if owner.cancelStart == nil {
			t.Fatal("accepted Start did not register private cancellation")
		}
	})
}

func TestProcessorTestOwnerStopSettlesAdmittedCommandBeforeStartCancel(t *testing.T) {
	operationCtx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	owner := newProcessorOwnerProof(operationCtx, t, true)
	defer owner.finish(operationCtx, t)
	admitted := make(chan context.Context, 1)
	release := make(chan struct{})
	released := false
	submitted := make(chan error, 1)
	submittedObserved := false
	defer func() {
		if !released {
			close(release)
		}
		if !submittedObserved {
			joinCtx, cancelJoin := context.WithTimeout(context.WithoutCancel(operationCtx), 5*time.Second)
			defer cancelJoin()
			select {
			case <-submitted:
			case <-joinCtx.Done():
				t.Error("test-owned Processor submitter did not join after setup escape")
			}
		}
	}()
	go func() {
		submitted <- owner.processor.submitRuntimeCommand(func(ctx context.Context) error {
			admitted <- ctx
			<-release
			return nil
		})
	}()
	var commandCtx context.Context
	select {
	case commandCtx = <-admitted:
	case <-operationCtx.Done():
		t.Fatal("Processor command was not admitted before operation deadline")
	}
	stopped := make(chan error, 1)
	go func() { stopped <- owner.stop(operationCtx) }()
	stopObserved := false
	defer func() {
		if !released {
			close(release)
			released = true
		}
		joinCtx, cancelJoin := context.WithTimeout(context.WithoutCancel(operationCtx), 5*time.Second)
		defer cancelJoin()
		if !submittedObserved {
			select {
			case <-submitted:
				submittedObserved = true
			case <-joinCtx.Done():
				t.Error("test-owned Processor submitter did not join after release")
			}
		}
		if !stopObserved {
			select {
			case <-stopped:
			case <-joinCtx.Done():
				t.Error("concrete Processor Stop did not return after test-owned release")
			}
		}
	}()
	for {
		owner.processor.commandLane.mu.Lock()
		fenced := owner.processor.commandLane.fenced
		owner.processor.commandLane.mu.Unlock()
		if fenced {
			break
		}
		select {
		case err := <-stopped:
			stopObserved = true
			t.Fatalf("Processor.Stop returned before command fence: %v", err)
		case <-operationCtx.Done():
			t.Fatal("Processor.Stop did not fence admitted work")
		default:
			runtime.Gosched()
		}
	}
	if err := commandCtx.Err(); err != nil {
		t.Fatalf("Start authority canceled before admitted command released: %v", err)
	}
	close(release)
	released = true
	var submittedErr error
	select {
	case submittedErr = <-submitted:
		submittedObserved = true
	case <-operationCtx.Done():
		t.Fatal("admitted Processor command did not return after release")
	}
	if submittedErr != nil {
		t.Fatalf("admitted command after release: %v", submittedErr)
	}
	var stopErr error
	select {
	case stopErr = <-stopped:
		stopObserved = true
	case <-operationCtx.Done():
		t.Fatal("concrete Processor.Stop did not return after released command")
	}
	if stopErr != nil {
		t.Fatalf("concrete Processor.Stop: %v", stopErr)
	}
	select {
	case <-owner.processor.commandLane.done():
	default:
		t.Fatal("Processor.Stop returned before command lane joined")
	}
}
