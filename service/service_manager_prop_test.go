package service

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

// Model-based lifecycle test over Manager.StopAll (rapid spike). A rapid state
// machine composes Register / ArmStopFailure / StopAll into sequences the
// example tests enumerate one at a time — repeated StopAll passes, failures
// armed between passes, services registered after a pass — and checks the
// service-shutdown postconditions after every action. Each postcondition cites
// the requirement it encodes.

// propStopCtxKey tags the per-pass token this test threads through StopAll so
// the spy can prove which context reached it.
type propStopCtxKey struct{}

// propStopService is a spy Service implementing the portable idempotent-Stop
// contract: teardown happens at most once, and a Stop after completion returns
// the service's drawn repeat style (nil or ErrAlreadyStopped — both are clean
// success per the Service interface contract) without repeating teardown.
// spec: service-shutdown / A framework service Stop is idempotent on repeated invocation
type propStopService struct {
	MockService
	visits      *[]string // manager-wide visit log shared across services
	teardowns   int
	stoppedFlag bool
	failNext    error // armed genuine failure, consumed by the next visit
	repeatStyle error // nil or ErrAlreadyStopped, drawn at registration

	// lastStopCtx records the context this spy was actually handed, so the
	// stopAll postcondition can prove StopAll forwarded the caller's context
	// rather than inventing a replacement. This is a TEST SPY recording an
	// argument; the repository rule against retaining a context binds
	// PRODUCTION structs and is not weakened here.
	lastStopCtx context.Context
}

func (s *propStopService) Stop(ctx context.Context) error {
	s.lastStopCtx = ctx
	*s.visits = append(*s.visits, s.name)
	if s.stoppedFlag {
		return s.repeatStyle
	}
	if s.failNext != nil {
		err := s.failNext
		s.failNext = nil
		return err
	}
	s.teardowns++
	s.stoppedFlag = true
	return nil
}

// TestStopAllRetryAfterFailedPassTreatsAlreadyStoppedAsClean is the rapid
// counterexample recorded in
// testdata/rapid/TestPropStopAllShutdownContract/...-20260831140109-41933.fail,
// promoted to a deterministic example so the coverage does not depend on a
// replayable byte stream.
//
// Provenance, measured 2026-08-31: that seed was recorded while the
// ErrAlreadyStopped filter in stopAll was mutated away, so it is a
// MUTATION-KILL WITNESS, not a defect ever present on main. Replaying it
// against `if stopErr != nil {` at service_manager.go:888 still reproduces the
// exact original message after 0 tests.
//
// Why the seed alone is not enough: t.Repeat records a stream that ENDS where
// the failure was, so on green code rapid can only log "fail file is no longer
// valid" under -v; and adding or renaming any action changes the
// SampledFrom(actionKeys) cardinality, silently re-decoding the same bytes
// into a different sequence. This test names the sequence, so neither can
// erode it.
//
// spec: service-shutdown / Coordinated shutdown treats an already-stopped service as clean success
func TestStopAllRetryAfterFailedPassTreatsAlreadyStoppedAsClean(t *testing.T) {
	manager := createTestServiceManager(ManagerConfig{}, nil)
	visits := []string{}

	svcA := &propStopService{
		MockService: MockService{name: "svcA", status: StatusRunning, healthy: true},
		visits:      &visits,
		failNext:    fmt.Errorf("genuine stop failure in svcA"),
	}
	svcB := &propStopService{
		MockService: MockService{name: "svcB", status: StatusRunning, healthy: true},
		visits:      &visits,
		repeatStyle: ErrAlreadyStopped,
	}
	// Registration order is load-bearing: StopAll visits in reverse, so svcA
	// must be registered first for the retry to visit svcB then svcA.
	for _, svc := range []*propStopService{svcA, svcB} {
		if err := manager.RegisterInstance(svc.name, svc); err != nil {
			t.Fatalf("RegisterInstance(%q): %v", svc.name, err)
		}
	}

	// Pass 1 visits svcB (clean) then svcA (armed failure). The genuine failure
	// is surfaced, and the registry is retained because the pass was not clean.
	err := manager.StopAll(context.Background())
	if err == nil || !strings.Contains(err.Error(), "svcA") {
		t.Fatalf("first pass returned %v, want svcA's genuine failure surfaced", err)
	}
	if got := len(manager.GetAllServices()); got != 2 {
		t.Fatalf("registry holds %d services after a FAILED pass, want both retained for retry", got)
	}

	// Pass 2: svcA's armed failure was consumed, so it now tears down; svcB is
	// already stopped and answers ErrAlreadyStopped, which is clean success.
	visits = visits[:0]
	if err := manager.StopAll(context.Background()); err != nil {
		t.Fatalf("retry returned %v, want nil — ErrAlreadyStopped from an already-stopped service is clean success", err)
	}
	if want := []string{"svcB", "svcA"}; !slices.Equal(visits, want) {
		t.Fatalf("retry visit order %v, want exact reverse registration order %v", visits, want)
	}
	if svcA.teardowns != 1 || svcB.teardowns != 1 {
		t.Fatalf("teardown counts svcA=%d svcB=%d, want exactly one each", svcA.teardowns, svcB.teardowns)
	}
	if got := len(manager.GetAllServices()); got != 0 {
		t.Fatalf("registry holds %d services after a CLEAN pass, want it cleared", got)
	}
}

func TestPropStopAllShutdownContract(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		manager := createTestServiceManager(ManagerConfig{}, nil)
		visits := []string{}
		services := map[string]*propStopService{}
		var order []string
		counter := 0
		passes := 0

		notStopped := func() []string {
			names := make([]string, 0, len(services))
			for _, name := range order {
				if !services[name].stoppedFlag {
					names = append(names, name)
				}
			}
			return names
		}

		t.Repeat(map[string]func(*rapid.T){
			"register": func(t *rapid.T) {
				counter++
				name := fmt.Sprintf("svc%02d", counter)
				svc := &propStopService{
					MockService: MockService{name: name, status: StatusRunning, healthy: true},
					visits:      &visits,
					repeatStyle: rapid.SampledFrom([]error{nil, ErrAlreadyStopped}).Draw(t, "repeatStyle"),
				}
				if err := manager.RegisterInstance(name, svc); err != nil {
					t.Fatalf("RegisterInstance(%q): %v", name, err)
				}
				order = append(order, name)
				services[name] = svc
			},
			"armStopFailure": func(t *rapid.T) {
				candidates := notStopped()
				if len(candidates) == 0 {
					t.Skip("no service left to fail")
				}
				name := rapid.SampledFrom(candidates).Draw(t, "failing")
				services[name].failNext = fmt.Errorf("genuine stop failure in %s", name)
			},
			"stopAll": func(t *rapid.T) {
				visits = visits[:0]
				var expectFailing []string
				for _, name := range order {
					if services[name].failNext != nil {
						expectFailing = append(expectFailing, name)
					}
				}

				// Each pass carries its own token, so the postcondition can tell
				// "the caller's context" from "some context" — including a
				// previous pass's.
				passes++
				passToken := fmt.Sprintf("pass%02d", passes)
				passCtx := context.WithValue(context.Background(), propStopCtxKey{}, passToken)

				err := manager.StopAll(passCtx)

				// Every registered service is visited exactly once, in exact
				// reverse registration order — already-stopped services included.
				// spec: service-shutdown / Coordinated shutdown treats an already-stopped service as clean success
				wantVisits := slices.Clone(order)
				slices.Reverse(wantVisits)
				if !slices.Equal(visits, wantVisits) {
					t.Fatalf("visit order %v, want exact reverse registration order %v", visits, wantVisits)
				}

				// StopAll forwards the caller-owned shutdown context to every
				// service and never invents a replacement. Asserting the token
				// rather than pointer identity keeps a legitimate derived child
				// (WithTimeout, WithCancel) passing while still failing any
				// invented root — a fresh Background carries no token.
				// spec: service-shutdown / Coordinated shutdown treats an already-stopped service as clean success
				for _, name := range order {
					got := services[name].lastStopCtx
					if got == nil {
						t.Fatalf("service %s was visited with no context recorded", name)
					}
					if token := got.Value(propStopCtxKey{}); token != passToken {
						t.Fatalf("service %s received a context carrying %v, want the caller's %q", name, token, passToken)
					}
				}

				// Clean shutdown returns nil; every genuine failure is preserved
				// in the aggregate and never halts the pass.
				// spec: service-shutdown / Coordinated shutdown treats an already-stopped service as clean success
				if len(expectFailing) == 0 {
					if err != nil {
						t.Fatalf("clean shutdown returned %v", err)
					}
				} else {
					if err == nil {
						t.Fatalf("genuine failures in %v not surfaced", expectFailing)
					}
					for _, name := range expectFailing {
						if !strings.Contains(err.Error(), name) {
							t.Fatalf("aggregate %q lost genuine failure of %s", err.Error(), name)
						}
					}
				}

				// Every service that entered the pass without an armed failure
				// completed teardown.
				for _, name := range order {
					if !slices.Contains(expectFailing, name) && !services[name].stoppedFlag {
						t.Fatalf("service %s not stopped after a pass it did not fail", name)
					}
				}

				// spec: service-shutdown / Terminal StopAll success deregisters every service; failure retains them for retry
				// A clean pass deregisters every service, so a later StopAll
				// visits nothing; a failed pass retains every registration for
				// retry. Found by this machine on register→stopAll→stopAll
				// (#1214) and stated as a requirement rather than inferred.
				if len(expectFailing) == 0 {
					order = nil
				}
			},
			"": func(t *rapid.T) {
				// spec: service-shutdown / Terminal StopAll success deregisters every service; failure retains them for retry
				// Global invariant, checked after EVERY action: the manager's
				// own registry membership matches the model's. This is what
				// makes the walk a state machine rather than a randomized
				// sequence — it observes the MANAGER, so a registry-lifetime
				// defect fails here, at the action that caused it, naming the
				// registry. This is the ASSERTION that enforces the cited
				// requirement; the model update in stopAll's postcondition
				// carries the same citation because it encodes the same clause.
				//
				// The predecessor of this block asserted teardowns <= 1 over
				// the spy's own counters. That was unfalsifiable: teardowns++
				// and stoppedFlag = true sit in the same branch guarded by
				// `if s.stoppedFlag { return }`, so it held by construction of
				// propStopService and no manager behavior could break it.
				registered := manager.GetAllServices()
				gotNames := slices.Sorted(maps.Keys(registered))
				wantNames := slices.Clone(order)
				slices.Sort(wantNames)
				if !slices.Equal(gotNames, wantNames) {
					t.Fatalf("manager registry holds %v, model expects %v", gotNames, wantNames)
				}
			},
		})
	})
}
