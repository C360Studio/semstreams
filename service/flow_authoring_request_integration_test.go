//go:build integration

package service_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/c360studio/semstreams/flowstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func validFlowAuthoringBody() map[string]any {
	return map[string]any{
		"name":        "Strict diagram",
		"description": "authoring only",
		"nodes":       []any{},
		"connections": []any{},
	}
}

func TestFlowAuthoringRequestsRejectRetiredLifecycleFields(t *testing.T) {
	mux, _, _ := createTestFlowService(t)
	retired := []string{
		"desired_state",
		"desired_components",
		"desired_changed_at",
		"effective_state",
		"desired_provenance",
		"boot_applied_provenance",
		"restart_required",
		// Historical aliases must fail too; accepting them would recreate an
		// undocumented lifecycle lane through permissive JSON decoding.
		"runtime_state",
		"deployment_state",
		"activation_state",
		"lifecycle_state",
		"flow_state",
	}
	endpoints := []struct {
		name   string
		method string
		path   string
		base   func() map[string]any
	}{
		{name: "create", method: http.MethodPost, path: "/flowbuilder/flows", base: validFlowAuthoringBody},
		{name: "update", method: http.MethodPut, path: "/flowbuilder/flows/strict-flow", base: func() map[string]any {
			body := validFlowAuthoringBody()
			body["expected_version"] = 1
			return body
		}},
		{name: "validate", method: http.MethodPost, path: "/flowbuilder/flows/strict-flow/validate", base: validFlowAuthoringBody},
	}

	for _, endpoint := range endpoints {
		for _, field := range retired {
			t.Run(endpoint.name+"/"+field, func(t *testing.T) {
				body := endpoint.base()
				body[field] = "retired"
				raw, err := json.Marshal(body)
				require.NoError(t, err)
				req := httptest.NewRequest(endpoint.method, endpoint.path, bytes.NewReader(raw))
				if endpoint.name == "validate" {
					req.ContentLength = -1
				}
				recorder := httptest.NewRecorder()
				mux.ServeHTTP(recorder, req)
				assert.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
				assert.Contains(t, recorder.Body.String(), fmt.Sprintf(`unknown field \"%s\"`, field))
			})
		}
	}
}

func TestFlowAuthoringRequestsRejectServerOwnedFieldsAndTrailingJSON(t *testing.T) {
	mux, _, _ := createTestFlowService(t)
	serverOwned := []string{"id", "version", "created_at", "updated_at", "created_by", "last_modified"}
	endpoints := []struct {
		name   string
		method string
		path   string
		base   func() map[string]any
	}{
		{name: "create", method: http.MethodPost, path: "/flowbuilder/flows", base: validFlowAuthoringBody},
		{name: "update", method: http.MethodPut, path: "/flowbuilder/flows/strict-flow", base: func() map[string]any {
			body := validFlowAuthoringBody()
			body["expected_version"] = 1
			return body
		}},
		{name: "validate", method: http.MethodPost, path: "/flowbuilder/flows/strict-flow/validate", base: validFlowAuthoringBody},
	}

	for _, endpoint := range endpoints {
		for _, field := range serverOwned {
			t.Run(endpoint.name+"/"+field, func(t *testing.T) {
				body := endpoint.base()
				body[field] = "caller-owned"
				raw, err := json.Marshal(body)
				require.NoError(t, err)
				req := httptest.NewRequest(endpoint.method, endpoint.path, bytes.NewReader(raw))
				if endpoint.name == "validate" {
					req.ContentLength = -1
				}
				recorder := httptest.NewRecorder()
				mux.ServeHTTP(recorder, req)
				assert.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
				assert.Contains(t, recorder.Body.String(), fmt.Sprintf(`unknown field \"%s\"`, field))
			})
		}
		t.Run(endpoint.name+"/trailing-json", func(t *testing.T) {
			raw, err := json.Marshal(endpoint.base())
			require.NoError(t, err)
			raw = append(raw, []byte(` {"second":true}`)...)
			req := httptest.NewRequest(endpoint.method, endpoint.path, bytes.NewReader(raw))
			if endpoint.name == "validate" {
				req.ContentLength = -1
			}
			recorder := httptest.NewRecorder()
			mux.ServeHTTP(recorder, req)
			assert.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
			assert.Contains(t, recorder.Body.String(), "multiple values")
		})
	}
}

func TestFlowCreateAndUpdateOwnIdentityVersionAndAudit(t *testing.T) {
	mux, store, _ := createTestFlowService(t)

	createRaw, err := json.Marshal(validFlowAuthoringBody())
	require.NoError(t, err)
	createReq := httptest.NewRequest(http.MethodPost, "/flowbuilder/flows", bytes.NewReader(createRaw))
	createRecorder := httptest.NewRecorder()
	mux.ServeHTTP(createRecorder, createReq)
	require.Equal(t, http.StatusCreated, createRecorder.Code, createRecorder.Body.String())
	var created flowstore.Flow
	require.NoError(t, json.NewDecoder(createRecorder.Body).Decode(&created))
	require.NotEmpty(t, created.ID)
	require.Equal(t, int64(1), created.Version)
	require.False(t, created.CreatedAt.IsZero())
	require.False(t, created.UpdatedAt.IsZero())
	require.False(t, created.LastModified.IsZero())

	updateBody := validFlowAuthoringBody()
	updateBody["name"] = "Updated strict diagram"
	updateBody["expected_version"] = created.Version
	updateRaw, err := json.Marshal(updateBody)
	require.NoError(t, err)
	updateReq := httptest.NewRequest(http.MethodPut, "/flowbuilder/flows/"+created.ID, bytes.NewReader(updateRaw))
	updateRecorder := httptest.NewRecorder()
	mux.ServeHTTP(updateRecorder, updateReq)
	require.Equal(t, http.StatusOK, updateRecorder.Code, updateRecorder.Body.String())
	var updated flowstore.Flow
	require.NoError(t, json.NewDecoder(updateRecorder.Body).Decode(&updated))
	assert.Equal(t, created.ID, updated.ID)
	assert.Equal(t, int64(2), updated.Version)
	assert.Equal(t, created.CreatedAt, updated.CreatedAt)
	assert.Equal(t, created.CreatedBy, updated.CreatedBy)
	assert.Equal(t, "Updated strict diagram", updated.Name)

	persisted, err := store.Get(t.Context(), created.ID)
	require.NoError(t, err)
	assert.Equal(t, updated, *persisted)
}
