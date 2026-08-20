//go:build integration

package websocket

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateOutputCustomPathServesProductionMux(t *testing.T) {
	created, err := CreateOutput(
		json.RawMessage(`{"path":"/factory-proof"}`),
		component.Dependencies{NATSClient: &natsclient.Client{}},
	)
	require.NoError(t, err)
	output := created.(*Output)
	require.NoError(t, output.Initialize())

	// Start normally creates this lifecycle state before setupHTTPServer. The
	// test serves the production handler with httptest's kernel-selected port so
	// it proves routing without changing or claiming Output listener binding.
	output.wg = &sync.WaitGroup{}
	require.NoError(t, output.setupHTTPServer(t.Context()))
	t.Cleanup(func() { require.NoError(t, output.listener.Close()) })
	output.requestMu.Lock()
	output.requestOpen = true
	output.requestMu.Unlock()

	server := httptest.NewServer(output.server.Handler)
	var connection *websocket.Conn
	var response *http.Response
	registrationObserved := false
	clientCount := func() int {
		output.clientsMu.RLock()
		defer output.clientsMu.RUnlock()
		return len(output.clients)
	}
	t.Cleanup(func() {
		if connection != nil {
			_ = connection.Close()
		}
		server.Close()
		if registrationObserved && assert.Eventually(t, func() bool {
			return clientCount() == 0
		}, time.Second, 10*time.Millisecond, "cleanup did not observe registered client removal") {
			output.wg.Wait()
		}
	})
	wsBaseURL := "ws" + strings.TrimPrefix(server.URL, "http")

	connection, response, err = websocket.DefaultDialer.Dial(wsBaseURL+"/factory-proof", nil)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		return clientCount() == 1
	}, time.Second, 10*time.Millisecond, "production handler did not register upgraded client")
	registrationObserved = true
	require.Equal(t, http.StatusSwitchingProtocols, response.StatusCode)
	require.NoError(t, connection.Close())
	require.Eventually(t, func() bool {
		return clientCount() == 0
	}, time.Second, 10*time.Millisecond, "production client goroutine did not observe close")
	output.wg.Wait()

	_, response, err = websocket.DefaultDialer.Dial(wsBaseURL+"/ws", nil)
	require.Error(t, err)
	require.NotNil(t, response)
	require.Equal(t, http.StatusNotFound, response.StatusCode)
	require.NoError(t, response.Body.Close())
}
