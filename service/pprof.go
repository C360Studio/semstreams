package service

import (
	"fmt"
	"net/http"
)

// MaybeStartPProf starts the pprof HTTP server in a background goroutine when
// debug mode is enabled (debug && port > 0); otherwise it is a no-op.
//
// ADR-058 rollout step 4: pprof is deliberately NOT a service.Service. An HTTP
// /debug/pprof mux has no process state to flush — every handler reads live
// runtime state per request — so it fails the Is-it-a-Service test on criterion 3
// (needs a clean Stop/join): process exit cleans the listener with no correctness
// loss. Wrapping it in a StartAll-managed Service would also move the start from
// the composition root (before NATS) to StartAll (after NATS + all wiring),
// losing the ability to profile a wedged or slow boot — the canonical pprof use
// case. So, mirroring step 2's "shared helper, not a Service" conclusion, this is
// a plain helper called early and identically by both mains, killing the
// duplicated startPProfServer that otherwise drifts (the beta.18 class).
//
// Fire-and-forget by design: a bind failure is a best-effort debug aid degrading,
// never a boot gate (the R1 posture). The pprof handlers are registered on
// http.DefaultServeMux by the CALLER's `import _ "net/http/pprof"` blank import —
// this helper serves that mux (nil handler). The blank import stays in the mains,
// not here, so importing package service does not silently arm pprof everywhere.
func MaybeStartPProf(debug bool, port int) {
	if !debug || port <= 0 {
		return
	}
	go func() {
		addr := fmt.Sprintf(":%d", port)
		// stdout because this runs before slog is configured (pre-Phase-A).
		fmt.Printf("Starting pprof server on %s\n", addr)
		if err := http.ListenAndServe(addr, nil); err != nil && err != http.ErrServerClosed {
			fmt.Printf("pprof server error: %v\n", err)
		}
	}()
}
