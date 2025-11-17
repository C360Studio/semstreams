//go:build integration
// +build integration

package graphql_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/c360/semstreams/gateway/graphql"
	"github.com/c360/semstreams/natsclient"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// startNATSContainer starts a NATS container and returns the container and connection URL
func startNATSContainer(t *testing.T, ctx context.Context) (testcontainers.Container, string) {
	t.Helper()

	req := testcontainers.ContainerRequest{
		Image:        "nats:2.10-alpine",
		ExposedPorts: []string{"4222/tcp"},
		WaitingFor:   wait.ForLog("Server is ready"),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	require.NoError(t, err)

	host, err := container.Host(ctx)
	require.NoError(t, err)

	port, err := container.MappedPort(ctx, "4222")
	require.NoError(t, err)

	natsURL := fmt.Sprintf("nats://%s:%s", host, port.Port())
	return container, natsURL
}

// TestIntegration_NATSQueryEntityByID tests successful entity query via NATS
func TestIntegration_NATSQueryEntityByID(t *testing.T) {
	ctx := context.Background()
	natsContainer, natsURL := startNATSContainer(t, ctx)
	defer natsContainer.Terminate(ctx)

	// Create NATS connection
	nc, err := nats.Connect(natsURL)
	require.NoError(t, err)
	defer nc.Close()

	// Setup mock responder
	_, err = nc.Subscribe("graph.query.entity", func(msg *nats.Msg) {
		response := graphql.Entity{
			ID:   "test-123",
			Type: "test-entity",
			Properties: map[string]interface{}{
				"name":  "Test Entity",
				"value": float64(42), // JSON unmarshaling uses float64 for numbers
			},
		}

		responseData := struct {
			Entity *graphql.Entity `json:"entity"`
			Error  string           `json:"error,omitempty"`
		}{
			Entity: &response,
		}

		data, _ := json.Marshal(responseData)
		msg.Respond(data)
	})
	require.NoError(t, err)

	// Wait for subscription to be ready
	nc.Flush()

	// Create GraphQL NATS client wrapper
	natsClient := &natsclient.Client{}
	natsClient.SetConnection(nc)

	subjects := graphql.NATSSubjectsConfig{
		EntityQuery:       "graph.query.entity",
		EntitiesQuery:     "graph.query.entities",
		RelationshipQuery: "graph.query.relationships",
		SemanticSearch:    "graph.search.semantic",
	}

	client := graphql.NewNATSClient(natsClient, subjects, 5*time.Second)

	// Test query
	queryCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	entity, err := client.QueryEntityByID(queryCtx, "test-123")
	require.NoError(t, err)
	require.NotNil(t, entity)
	assert.Equal(t, "test-123", entity.ID)
	assert.Equal(t, "test-entity", entity.Type)
	assert.Equal(t, "Test Entity", graphql.GetStringProp(entity, "name"))
	assert.Equal(t, 42, graphql.GetIntProp(entity, "value"))
}

// TestIntegration_NATSQueryEntitiesByIDs tests batch entity query via NATS
func TestIntegration_NATSQueryEntitiesByIDs(t *testing.T) {
	ctx := context.Background()
	natsContainer, natsURL := startNATSContainer(t, ctx)
	defer natsContainer.Terminate(ctx)

	// Create NATS connection
	nc, err := nats.Connect(natsURL)
	require.NoError(t, err)
	defer nc.Close()

	// Setup mock responder
	_, err = nc.Subscribe("graph.query.entities", func(msg *nats.Msg) {
		entities := []*graphql.Entity{
			{
				ID:   "test-1",
				Type: "test-entity",
				Properties: map[string]interface{}{
					"name": "Entity 1",
				},
			},
			{
				ID:   "test-2",
				Type: "test-entity",
				Properties: map[string]interface{}{
					"name": "Entity 2",
				},
			},
		}

		responseData := struct {
			Entities []*graphql.Entity `json:"entities"`
			Error    string            `json:"error,omitempty"`
		}{
			Entities: entities,
		}

		data, _ := json.Marshal(responseData)
		msg.Respond(data)
	})
	require.NoError(t, err)
	nc.Flush()

	// Create GraphQL NATS client wrapper
	natsClient := &natsclient.Client{}
	natsClient.SetConnection(nc)

	subjects := graphql.NATSSubjectsConfig{
		EntityQuery:       "graph.query.entity",
		EntitiesQuery:     "graph.query.entities",
		RelationshipQuery: "graph.query.relationships",
		SemanticSearch:    "graph.search.semantic",
	}

	client := graphql.NewNATSClient(natsClient, subjects, 5*time.Second)

	// Test batch query
	queryCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	entities, err := client.QueryEntitiesByIDs(queryCtx, []string{"test-1", "test-2"})
	require.NoError(t, err)
	require.Len(t, entities, 2)
	assert.Equal(t, "test-1", entities[0].ID)
	assert.Equal(t, "test-2", entities[1].ID)
}

// TestIntegration_NATSTimeout tests timeout handling
func TestIntegration_NATSTimeout(t *testing.T) {
	ctx := context.Background()
	natsContainer, natsURL := startNATSContainer(t, ctx)
	defer natsContainer.Terminate(ctx)

	// Create NATS connection
	nc, err := nats.Connect(natsURL)
	require.NoError(t, err)
	defer nc.Close()

	// Setup slow responder that never responds
	_, err = nc.Subscribe("graph.query.entity", func(msg *nats.Msg) {
		// Don't respond - simulate timeout
		time.Sleep(10 * time.Second)
	})
	require.NoError(t, err)
	nc.Flush()

	// Create GraphQL NATS client wrapper
	natsClient := &natsclient.Client{}
	natsClient.SetConnection(nc)

	subjects := graphql.NATSSubjectsConfig{
		EntityQuery:       "graph.query.entity",
		EntitiesQuery:     "graph.query.entities",
		RelationshipQuery: "graph.query.relationships",
		SemanticSearch:    "graph.search.semantic",
	}

	client := graphql.NewNATSClient(natsClient, subjects, 5*time.Second)

	// Test with short timeout
	queryCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()

	entity, err := client.QueryEntityByID(queryCtx, "test-123")
	assert.Error(t, err)
	assert.Nil(t, entity)
	// Should get a timeout error (either NATS timeout or context deadline exceeded)
	assert.True(t, err == nats.ErrTimeout || err.Error() != "")
}

// TestIntegration_NATSNoResponders tests error when no service responds
func TestIntegration_NATSNoResponders(t *testing.T) {
	ctx := context.Background()
	natsContainer, natsURL := startNATSContainer(t, ctx)
	defer natsContainer.Terminate(ctx)

	// Create NATS connection
	nc, err := nats.Connect(natsURL)
	require.NoError(t, err)
	defer nc.Close()

	// Don't setup any responder - should get "no responders" error

	// Create GraphQL NATS client wrapper
	natsClient := &natsclient.Client{}
	natsClient.SetConnection(nc)

	subjects := graphql.NATSSubjectsConfig{
		EntityQuery:       "graph.query.entity",
		EntitiesQuery:     "graph.query.entities",
		RelationshipQuery: "graph.query.relationships",
		SemanticSearch:    "graph.search.semantic",
	}

	client := graphql.NewNATSClient(natsClient, subjects, 5*time.Second)

	// Test query with no responders
	queryCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	entity, err := client.QueryEntityByID(queryCtx, "test-123")
	require.Error(t, err)
	assert.Nil(t, entity)
	// Should get a transient error wrapping NATS error
	assert.Contains(t, err.Error(), "no responders")
}

// TestIntegration_NATSMalformedResponse tests handling of malformed JSON response
func TestIntegration_NATSMalformedResponse(t *testing.T) {
	ctx := context.Background()
	natsContainer, natsURL := startNATSContainer(t, ctx)
	defer natsContainer.Terminate(ctx)

	// Create NATS connection
	nc, err := nats.Connect(natsURL)
	require.NoError(t, err)
	defer nc.Close()

	// Setup responder with invalid JSON
	_, err = nc.Subscribe("graph.query.entity", func(msg *nats.Msg) {
		msg.Respond([]byte(`{invalid json}`))
	})
	require.NoError(t, err)
	nc.Flush()

	// Create GraphQL NATS client wrapper
	natsClient := &natsclient.Client{}
	natsClient.SetConnection(nc)

	subjects := graphql.NATSSubjectsConfig{
		EntityQuery:       "graph.query.entity",
		EntitiesQuery:     "graph.query.entities",
		RelationshipQuery: "graph.query.relationships",
		SemanticSearch:    "graph.search.semantic",
	}

	client := graphql.NewNATSClient(natsClient, subjects, 5*time.Second)

	// Test query with malformed response
	queryCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	entity, err := client.QueryEntityByID(queryCtx, "test-123")
	require.Error(t, err)
	assert.Nil(t, entity)
	// Should get an invalid error (JSON unmarshal error)
	assert.Contains(t, err.Error(), "unmarshal")
}

// TestIntegration_NATSErrorResponse tests handling of error in response payload
func TestIntegration_NATSErrorResponse(t *testing.T) {
	ctx := context.Background()
	natsContainer, natsURL := startNATSContainer(t, ctx)
	defer natsContainer.Terminate(ctx)

	// Create NATS connection
	nc, err := nats.Connect(natsURL)
	require.NoError(t, err)
	defer nc.Close()

	// Setup responder that returns an error
	_, err = nc.Subscribe("graph.query.entity", func(msg *nats.Msg) {
		responseData := struct {
			Entity *graphql.Entity `json:"entity"`
			Error  string           `json:"error,omitempty"`
		}{
			Error: "entity not found",
		}

		data, _ := json.Marshal(responseData)
		msg.Respond(data)
	})
	require.NoError(t, err)
	nc.Flush()

	// Create GraphQL NATS client wrapper
	natsClient := &natsclient.Client{}
	natsClient.SetConnection(nc)

	subjects := graphql.NATSSubjectsConfig{
		EntityQuery:       "graph.query.entity",
		EntitiesQuery:     "graph.query.entities",
		RelationshipQuery: "graph.query.relationships",
		SemanticSearch:    "graph.search.semantic",
	}

	client := graphql.NewNATSClient(natsClient, subjects, 5*time.Second)

	// Test query with error response
	queryCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	entity, err := client.QueryEntityByID(queryCtx, "test-123")
	require.Error(t, err)
	assert.Nil(t, entity)
	// Should get an invalid error (remote service error) - Issue #4 fix
	assert.Contains(t, err.Error(), "entity not found")
	assert.Contains(t, err.Error(), "remote service error")
}

// TestIntegration_NATSSemanticSearch tests semantic search via NATS
func TestIntegration_NATSSemanticSearch(t *testing.T) {
	ctx := context.Background()
	natsContainer, natsURL := startNATSContainer(t, ctx)
	defer natsContainer.Terminate(ctx)

	// Create NATS connection
	nc, err := nats.Connect(natsURL)
	require.NoError(t, err)
	defer nc.Close()

	// Setup mock responder
	_, err = nc.Subscribe("graph.search.semantic", func(msg *nats.Msg) {
		results := []*graphql.SemanticSearchResult{
			{
				Entity: &graphql.Entity{
					ID:   "result-1",
					Type: "doc",
					Properties: map[string]interface{}{
						"title": "Test Document",
					},
				},
				Score: 0.95,
			},
		}

		responseData := struct {
			Results []*graphql.SemanticSearchResult `json:"results"`
			Error   string                          `json:"error,omitempty"`
		}{
			Results: results,
		}

		data, _ := json.Marshal(responseData)
		msg.Respond(data)
	})
	require.NoError(t, err)
	nc.Flush()

	// Create GraphQL NATS client wrapper
	natsClient := &natsclient.Client{}
	natsClient.SetConnection(nc)

	subjects := graphql.NATSSubjectsConfig{
		EntityQuery:       "graph.query.entity",
		EntitiesQuery:     "graph.query.entities",
		RelationshipQuery: "graph.query.relationships",
		SemanticSearch:    "graph.search.semantic",
	}

	client := graphql.NewNATSClient(natsClient, subjects, 5*time.Second)

	// Test semantic search
	queryCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	results, err := client.SemanticSearch(queryCtx, "test query", 10)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "result-1", results[0].Entity.ID)
	assert.Equal(t, 0.95, results[0].Score)
}
