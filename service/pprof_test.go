package service

import (
	"fmt"
	"net/http"
	_ "net/http/pprof" // populate DefaultServeMux so the happy-path test serves /debug/pprof
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// (freePort lives in service_manager_health_listener_test.go — reused here.)

// TestMaybeStartPProf_DisabledOrInvalid_NoListener verifies the gate: with debug
// off or a non-positive port, no server is started (nothing answers on the port).
func TestMaybeStartPProf_DisabledOrInvalid_NoListener(t *testing.T) {
	// debug off → no goroutine spawned, so nothing serves on the port.
	port := freePort(t)
	MaybeStartPProf(false, port)
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/debug/pprof/", port)) //nolint:bodyclose // err path
	require.Error(t, err, "debug off must not start a pprof listener")
	if resp != nil {
		_ = resp.Body.Close()
	}

	// port <= 0 → early return, no panic, no bind.
	MaybeStartPProf(true, 0)
	MaybeStartPProf(true, -1)
}

// TestMaybeStartPProf_Enabled_ServesPprof verifies the happy path: with debug on
// and a valid port, the pprof index becomes reachable over HTTP — proving the
// full chain (blank import → DefaultServeMux → served by the helper).
func TestMaybeStartPProf_Enabled_ServesPprof(t *testing.T) {
	port := freePort(t)
	MaybeStartPProf(true, port)

	url := fmt.Sprintf("http://127.0.0.1:%d/debug/pprof/", port)
	var resp *http.Response
	var err error
	for i := 0; i < 50; i++ { // listener comes up asynchronously; poll up to ~1s.
		if resp, err = http.Get(url); err == nil { //nolint:bodyclose // closed below
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	require.NoError(t, err, "pprof endpoint must become reachable")
	require.Equal(t, http.StatusOK, resp.StatusCode, "/debug/pprof/ must serve the pprof index")
	require.NoError(t, resp.Body.Close())
}
