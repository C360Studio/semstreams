package service

import (
	"net"
	"net/http"
	_ "net/http/pprof" // populate DefaultServeMux so the happy-path test serves /debug/pprof
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestMaybeStartPProf_DisabledOrInvalid_NoListener verifies the gate: with debug
// off or a non-positive port, no acquisition is attempted.
func TestMaybeStartPProf_DisabledOrInvalid_NoListener(t *testing.T) {
	called := false
	refuse := func(string, string) (net.Listener, error) {
		called = true
		return nil, nil
	}
	for _, test := range []struct {
		enabled bool
		port    int
	}{{false, 1}, {true, 0}, {true, -1}} {
		done := startPProf(test.enabled, test.port, &http.Server{}, refuse)
		select {
		case <-done:
		default:
			t.Fatal("disabled pprof returned unfinished completion")
		}
	}
	require.False(t, called)
	MaybeStartPProf(false, 1)
	MaybeStartPProf(true, 0)
	MaybeStartPProf(true, -1)
}

// TestMaybeStartPProf_Enabled_ServesPprof verifies the happy path: with debug on
// and a valid port, the pprof index becomes reachable over HTTP — proving the
// full chain (blank import → DefaultServeMux → served by the helper).
func TestMaybeStartPProf_Enabled_ServesPprof(t *testing.T) {
	server := &http.Server{}
	acquired := make(chan net.Listener, 1)
	done := startPProf(true, 1, server, func(_, _ string) (net.Listener, error) {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err == nil {
			acquired <- listener
		}
		return listener, err
	})
	t.Cleanup(func() {
		require.NoError(t, server.Close())
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("pprof Serve did not finish after Close")
		}
	})
	var listener net.Listener
	select {
	case listener = <-acquired:
	case <-done:
		t.Fatal("pprof acquisition ended before providing a listener")
	case <-time.After(2 * time.Second):
		t.Fatal("pprof acquisition did not complete")
	}
	url := "http://" + listener.Addr().String() + "/debug/pprof/"
	waitForListener(t, url, 2*time.Second)
	resp, err := http.Get(url)
	require.NoError(t, err, "pprof endpoint must become reachable")
	require.Equal(t, http.StatusOK, resp.StatusCode, "/debug/pprof/ must serve the pprof index")
	require.NoError(t, resp.Body.Close())
}
