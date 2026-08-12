//go:build integration

package maxdelivery

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	natstest "github.com/nats-io/nats-server/v2/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/config"
	"github.com/c360studio/semstreams/natsclient"
)

const (
	testAdminUser       = "test-admin"
	testAdminPassword   = "test-admin-password"
	testRuntimeUser     = "semstreams-runtime"
	testRuntimePassword = "semstreams-runtime-password"
)

var sufficientObserverRuntimePublishPermissions = []string{
	"$JS.API.STREAM.INFO.*",
	"$JS.API.STREAM.CREATE.*",
	"$JS.API.STREAM.UPDATE.*",
	"$JS.API.CONSUMER.INFO.MAX_DELIVERY_EVENTS.semstreams-max-delivery-observer",
	"$JS.API.CONSUMER.CREATE.MAX_DELIVERY_EVENTS.semstreams-max-delivery-observer.>",
	"$JS.API.CONSUMER.MSG.NEXT.MAX_DELIVERY_EVENTS.semstreams-max-delivery-observer",
	"$JS.ACK.MAX_DELIVERY_EVENTS.semstreams-max-delivery-observer.>",
}

func TestRestrictiveAuthorizationRuntimeContract(t *testing.T) {
	t.Run("sufficient provisioning and binding permissions succeed without advisory subscription", func(t *testing.T) {
		srv := runAuthorizedServer(t, sufficientObserverRuntimePublishPermissions, []string{"_INBOX.>"})
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		runtimeClient := connectAuthorizedClient(t, ctx, srv.ClientURL(), testRuntimeUser, testRuntimePassword)
		manager := config.NewStreamsManager(runtimeClient, discardLogger())
		require.NoError(t, manager.EnsureStreams(ctx, &config.Config{}))

		admin := connectAuthorizedClient(t, ctx, srv.ClientURL(), testAdminUser, testAdminPassword)
		adminJS, err := admin.JetStream()
		require.NoError(t, err)
		capture, err := adminJS.Stream(ctx, captureStreamName)
		require.NoError(t, err)
		captureInfo, err := capture.Info(ctx)
		require.NoError(t, err)
		drifted := captureInfo.Config
		drifted.MaxAge = time.Hour
		_, err = adminJS.UpdateStream(ctx, drifted)
		require.NoError(t, err)
		require.NoError(t, manager.EnsureStreams(ctx, &config.Config{}),
			"the sufficient set must allow central STREAM.UPDATE reconciliation")
		captureInfo, err = capture.Info(ctx)
		require.NoError(t, err)
		require.Equal(t, 7*24*time.Hour, captureInfo.Config.MaxAge)

		telemetry := newIntegrationTelemetry(false)
		stop, err := start(ctx, runtimeClient, telemetry)
		require.NoError(t, err)
		defer stop()

		baseline := captureInfo.State.LastSeq
		want := forceMaxDeliveryAdvisory(t, ctx, admin, "AUTH_PROOF", "auth.proof")
		select {
		case got := <-telemetry.events:
			assert.Equal(t, want.Stream, got.Stream)
			assert.Equal(t, want.StreamSequence, got.StreamSequence)
		case <-ctx.Done():
			t.Fatal("authorized observer did not handle the retained advisory")
		}
		captured, err := capture.GetLastMsgForSubject(ctx, advisorySubjectPrefix+".AUTH_PROOF.exhaust-once")
		require.NoError(t, err)
		require.Greater(t, captured.Sequence, baseline)
		require.Eventually(t, func() bool {
			consumer, consumerErr := adminJS.Consumer(ctx, captureStreamName, observerConsumerName)
			if consumerErr != nil {
				return false
			}
			info, infoErr := consumer.Info(ctx)
			return infoErr == nil && info.NumAckPending == 0 && info.AckFloor.Stream == captured.Sequence
		}, 2*time.Second, 20*time.Millisecond, "the fixed durable ACK floor must cover the handled advisory")
		select {
		case duplicate := <-telemetry.events:
			t.Fatalf("settled advisory redelivered: %+v", duplicate)
		case <-time.After(250 * time.Millisecond):
		}
	})

	for _, test := range []struct {
		name      string
		publish   []string
		subscribe []string
	}{
		{name: "missing JetStream API publish", publish: []string{"$JS.ACK.MAX_DELIVERY_EVENTS.semstreams-max-delivery-observer.>"}, subscribe: []string{"_INBOX.>"}},
		{name: "missing reply inbox subscribe", publish: sufficientObserverRuntimePublishPermissions},
	} {
		t.Run(test.name+" fails boot before observer bind", func(t *testing.T) {
			srv := runAuthorizedServer(t, test.publish, test.subscribe)
			ctx, cancel := context.WithTimeout(context.Background(), 750*time.Millisecond)
			defer cancel()
			client := connectAuthorizedClient(t, ctx, srv.ClientURL(), testRuntimeUser, testRuntimePassword)
			err := config.NewStreamsManager(client, discardLogger()).EnsureStreams(ctx, &config.Config{})
			require.Error(t, err)
		})
	}

	t.Run("missing consumer create permission fails observer bind", func(t *testing.T) {
		withoutConsumerCreate := []string{
			"$JS.API.STREAM.INFO.*",
			"$JS.API.CONSUMER.INFO.MAX_DELIVERY_EVENTS.semstreams-max-delivery-observer",
			"$JS.API.CONSUMER.MSG.NEXT.MAX_DELIVERY_EVENTS.semstreams-max-delivery-observer",
			"$JS.ACK.MAX_DELIVERY_EVENTS.semstreams-max-delivery-observer.>",
		}
		srv := runAuthorizedServer(t, withoutConsumerCreate, []string{"_INBOX.>"})
		ctx, cancel := context.WithTimeout(context.Background(), 750*time.Millisecond)
		defer cancel()
		admin := connectAuthorizedClient(t, ctx, srv.ClientURL(), testAdminUser, testAdminPassword)
		require.NoError(t, config.NewStreamsManager(admin, discardLogger()).EnsureStreams(ctx, &config.Config{}))
		runtimeClient := connectAuthorizedClient(t, ctx, srv.ClientURL(), testRuntimeUser, testRuntimePassword)
		_, err := start(ctx, runtimeClient, newIntegrationTelemetry(false))
		require.Error(t, err)
	})

	t.Run("missing stream update permission fails drift reconciliation", func(t *testing.T) {
		withoutUpdate := []string{
			"$JS.API.STREAM.INFO.*", "$JS.API.STREAM.CREATE.*",
			"$JS.API.CONSUMER.INFO.MAX_DELIVERY_EVENTS.semstreams-max-delivery-observer",
			"$JS.API.CONSUMER.CREATE.MAX_DELIVERY_EVENTS.semstreams-max-delivery-observer.>",
			"$JS.API.CONSUMER.MSG.NEXT.MAX_DELIVERY_EVENTS.semstreams-max-delivery-observer",
			"$JS.ACK.MAX_DELIVERY_EVENTS.semstreams-max-delivery-observer.>",
		}
		srv := runAuthorizedServer(t, withoutUpdate, []string{"_INBOX.>"})
		ctx, cancel := context.WithTimeout(context.Background(), 750*time.Millisecond)
		defer cancel()
		admin := connectAuthorizedClient(t, ctx, srv.ClientURL(), testAdminUser, testAdminPassword)
		manager := config.NewStreamsManager(admin, discardLogger())
		require.NoError(t, manager.EnsureStreams(ctx, &config.Config{}))
		js, err := admin.JetStream()
		require.NoError(t, err)
		capture, err := js.Stream(ctx, captureStreamName)
		require.NoError(t, err)
		info, err := capture.Info(ctx)
		require.NoError(t, err)
		drifted := info.Config
		drifted.MaxAge = time.Hour
		_, err = js.UpdateStream(ctx, drifted)
		require.NoError(t, err)

		runtimeClient := connectAuthorizedClient(t, ctx, srv.ClientURL(), testRuntimeUser, testRuntimePassword)
		err = config.NewStreamsManager(runtimeClient, discardLogger()).EnsureStreams(ctx, &config.Config{})
		require.Error(t, err)
	})
}

func TestThreeNodeClusterReplicasOneRetainsAndHandlesOccurrenceOnce(t *testing.T) {
	servers := runThreeNodeCluster(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	clients := make([]*natsclient.Client, 0, len(servers))
	for _, srv := range servers {
		client, err := natsclient.NewClient(srv.ClientURL())
		require.NoError(t, err)
		require.NoError(t, client.Connect(ctx))
		t.Cleanup(func() { _ = client.Close(context.Background()) })
		clients = append(clients, client)
	}

	require.NoError(t, config.NewStreamsManager(clients[0], discardLogger()).EnsureStreams(ctx, &config.Config{}))
	js, err := clients[0].JetStream()
	require.NoError(t, err)
	capture, err := js.Stream(ctx, captureStreamName)
	require.NoError(t, err)
	info, err := capture.Info(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, info.Config.Replicas, "the fixed declaration intentionally remains R=1")

	telemetry := newIntegrationTelemetry(false)
	stopSecondNode, err := start(ctx, clients[1], telemetry)
	require.NoError(t, err)
	defer stopSecondNode()
	stopThirdNode, err := start(ctx, clients[2], telemetry)
	require.NoError(t, err)
	defer stopThirdNode()

	want := forceMaxDeliveryAdvisory(t, ctx, clients[0], "CLUSTER_PROOF", "cluster.proof")
	select {
	case got := <-telemetry.events:
		assert.Equal(t, want.Stream, got.Stream)
		assert.Equal(t, want.StreamSequence, got.StreamSequence)
	case <-ctx.Done():
		t.Fatal("cluster observers did not handle the retained occurrence")
	}
	select {
	case duplicate := <-telemetry.events:
		t.Fatalf("one clustered occurrence was handled twice: %+v", duplicate)
	case <-time.After(500 * time.Millisecond):
	}
	info, err = capture.Info(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint64(1), info.State.Msgs, "one server advisory is retained once in the R=1 ledger")
}

func runAuthorizedServer(t *testing.T, publish, subscribe []string) *natsserver.Server {
	t.Helper()
	quoteSubjects := func(subjects []string) string {
		out := ""
		for i, subject := range subjects {
			if i > 0 {
				out += ","
			}
			out += fmt.Sprintf("%q", subject)
		}
		return out
	}
	subscribePermissions := fmt.Sprintf("allow: [%s]", quoteSubjects(subscribe))
	if len(subscribe) == 0 {
		// In NATS permissions an empty allow list means allow all. An explicit
		// deny is therefore the only faithful way to model a missing class.
		subscribePermissions = `deny: ">"`
	}
	dir := t.TempDir()
	conf := fmt.Sprintf(`
port: -1
jetstream { store_dir: %q }
authorization {
  users: [
    { user: %q, password: %q, permissions: { publish: ">", subscribe: ">" } }
    { user: %q, password: %q, permissions: {
        publish: { allow: [%s] }
	        subscribe: { %s }
    } }
  ]
}
`, filepath.Join(dir, "store"), testAdminUser, testAdminPassword,
		testRuntimeUser, testRuntimePassword, quoteSubjects(publish), subscribePermissions)
	path := filepath.Join(dir, "nats.conf")
	require.NoError(t, os.WriteFile(path, []byte(conf), 0o600))
	srv, _ := natstest.RunServerWithConfig(path)
	t.Cleanup(srv.Shutdown)
	return srv
}

func connectAuthorizedClient(t *testing.T, ctx context.Context, url, user, password string) *natsclient.Client {
	t.Helper()
	client, err := natsclient.NewClient(url, natsclient.WithCredentials(user, password))
	require.NoError(t, err)
	require.NoError(t, client.Connect(ctx))
	t.Cleanup(func() { _ = client.Close(context.Background()) })
	return client
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func runThreeNodeCluster(t *testing.T) []*natsserver.Server {
	t.Helper()
	ports := reserveLoopbackPorts(t, 6)
	clientPorts := ports[:3]
	routePorts := ports[3:]
	servers := make([]*natsserver.Server, 0, 3)
	for i := range 3 {
		dir := t.TempDir()
		routeTarget := routePorts[0]
		if i == 0 {
			// JetStream refuses clustered startup without at least one configured
			// route, so the seed solicits node two before node two starts.
			routeTarget = routePorts[1]
		}
		routes := fmt.Sprintf("routes: [nats-route://127.0.0.1:%d]", routeTarget)
		conf := fmt.Sprintf(`
server_name: S%d
listen: 127.0.0.1:%d
jetstream { store_dir: %q }
cluster {
  name: MAX_DELIVERY_PROOF
  listen: 127.0.0.1:%d
  %s
}
`, i+1, clientPorts[i], filepath.Join(dir, "store"), routePorts[i], routes)
		path := filepath.Join(dir, "nats.conf")
		require.NoError(t, os.WriteFile(path, []byte(conf), 0o600))
		srv, _ := natstest.RunServerWithConfig(path)
		servers = append(servers, srv)
		t.Cleanup(srv.Shutdown)
	}

	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		routesReady := true
		metaReady := true
		leaders := 0
		for _, srv := range servers {
			routesReady = routesReady && srv.NumRoutes() == 2
			metaReady = metaReady && srv.JetStreamIsCurrent()
			if srv.JetStreamIsLeader() {
				leaders++
				metaReady = metaReady && len(srv.JetStreamClusterPeers()) == 3
			}
		}
		if routesReady && metaReady && leaders == 1 {
			return servers
		}
		select {
		case <-deadline.C:
			counts := make([]string, 0, len(servers))
			for _, srv := range servers {
				counts = append(counts, strconv.Itoa(srv.NumRoutes())+"/"+strconv.Itoa(len(srv.JetStreamClusterPeers())))
			}
			t.Fatalf("three-node JetStream cluster did not converge; routes/peers=%v", counts)
		case <-ticker.C:
		}
	}
}

func reserveLoopbackPorts(t *testing.T, count int) []int {
	t.Helper()
	listeners := make([]net.Listener, 0, count)
	ports := make([]int, 0, count)
	for range count {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		listeners = append(listeners, listener)
		ports = append(ports, listener.Addr().(*net.TCPAddr).Port)
	}
	// Holding every reservation at once eliminates duplicate selection; closing
	// immediately before embedded startup leaves only the unavoidable kernel bind
	// handoff, with all ports already resolved into the cluster configs.
	for _, listener := range listeners {
		require.NoError(t, listener.Close())
	}
	return ports
}
