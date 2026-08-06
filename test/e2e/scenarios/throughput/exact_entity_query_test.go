package throughput

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestBuildQueryPoolEntityQueriesSelectExactEntity(t *testing.T) {
	t.Parallel()

	found := 0
	for _, query := range buildQueryPool() {
		if query.Name != "entity" {
			continue
		}
		found++
		if !strings.Contains(query.Query, "entity { id triples") ||
			!strings.Contains(query.Query, "kvRevision") {
			t.Fatalf("entity load query does not select ExactEntity fields: %s", query.Query)
		}
	}
	if found != 30 {
		t.Fatalf("entity query count = %d, want 30", found)
	}
}

func TestProbeEntitiesSelectsExactEntity(t *testing.T) {
	t.Parallel()

	const entityID = "c360.logistics.environmental.sensor.temperature.temp-sensor-001"
	var observedQuery string
	httpClient := &http.Client{Transport: probeTransport(func(r *http.Request) (*http.Response, error) {
		var request struct {
			Query string `json:"query"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			return nil, err
		}
		observedQuery = request.Query
		return probeResponse(`{"data":{"entity":{"entity":{"id":"` + entityID + `"},"kvRevision":1}}}`), nil
	})}

	scenario := &Scenario{config: &Config{GraphQLURL: "http://graphql.test/query"}}
	missing := scenario.probeEntities(context.Background(), httpClient, []string{entityID})
	if len(missing) != 0 {
		t.Fatalf("probe reported existing exact entity missing: %v", missing)
	}
	if !strings.Contains(observedQuery, "entity { id }") ||
		!strings.Contains(observedQuery, "kvRevision") {
		t.Fatalf("probe query does not select ExactEntity fields: %s", observedQuery)
	}
}

func TestProbeEntitiesRequiresCompleteExactEntityEvidence(t *testing.T) {
	t.Parallel()

	const entityID = "c360.logistics.environmental.sensor.temperature.temp-sensor-001"
	tests := map[string]string{
		"null exact result":       `{"data":{"entity":null}}`,
		"missing nested entity":   `{"data":{"entity":{"entity":null,"kvRevision":1}}}`,
		"zero authority revision": `{"data":{"entity":{"entity":{"id":"` + entityID + `"},"kvRevision":0}}}`,
		"wrong entity":            `{"data":{"entity":{"entity":{"id":"c360.other.entity.id.value.001"},"kvRevision":1}}}`,
		"graphql error":           `{"errors":[{"message":"not found"}],"data":{"entity":null}}`,
	}
	for name, body := range tests {
		name, body := name, body
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			client := &http.Client{Transport: probeTransport(func(*http.Request) (*http.Response, error) {
				return probeResponse(body), nil
			})}
			scenario := &Scenario{config: &Config{GraphQLURL: "http://graphql.test/query"}}
			missing := scenario.probeEntities(context.Background(), client, []string{entityID})
			if len(missing) != 1 || missing[0] != entityID {
				t.Fatalf("missing = %v, want [%s]", missing, entityID)
			}
		})
	}
}

func TestProbeEntitiesWithoutConfigReportsAllMissing(t *testing.T) {
	t.Parallel()

	entityIDs := []string{"c360.logistics.environmental.sensor.temperature.temp-sensor-001"}
	missing := (&Scenario{}).probeEntities(context.Background(), http.DefaultClient, entityIDs)
	if len(missing) != 1 || missing[0] != entityIDs[0] {
		t.Fatalf("missing = %v, want %v", missing, entityIDs)
	}
}

type probeTransport func(*http.Request) (*http.Response, error)

func (transport probeTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func probeResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
