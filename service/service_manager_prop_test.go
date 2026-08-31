package service

import (
	"context"
	"fmt"
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
}

func (s *propStopService) Stop(context.Context) error {
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

func TestPropStopAllShutdownContract(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		manager := createTestServiceManager(ManagerConfig{}, nil)
		visits := []string{}
		services := map[string]*propStopService{}
		var order []string
		counter := 0

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

				err := manager.StopAll(context.Background())

				// Every registered service is visited exactly once, in exact
				// reverse registration order — already-stopped services included.
				// spec: service-shutdown / Coordinated shutdown treats an already-stopped service as clean success
				wantVisits := slices.Clone(order)
				slices.Reverse(wantVisits)
				if !slices.Equal(visits, wantVisits) {
					t.Fatalf("visit order %v, want exact reverse registration order %v", visits, wantVisits)
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

				// SPIKE FINDING (sequence register→stopAll→stopAll): a fully
				// clean pass clears the manager registry (stopAll tail), so a
				// later StopAll visits nothing and returns nil, while a failed
				// pass retains every registration for retry. No service-shutdown
				// requirement states this terminal transition; the model mirrors
				// the observed behavior so the walk can continue past it.
				if len(expectFailing) == 0 {
					order = nil
				}
			},
			"": func(t *rapid.T) {
				// Global invariant: teardown never repeats, whatever the sequence.
				// spec: component-lifecycle / Running Stop has no shared-generation contract
				for name, svc := range services {
					if svc.teardowns > 1 {
						t.Fatalf("service %s tore down %d times", name, svc.teardowns)
					}
					if svc.stoppedFlag && svc.teardowns != 1 {
						t.Fatalf("service %s stopped with %d teardowns", name, svc.teardowns)
					}
				}
			},
		})
	})
}
