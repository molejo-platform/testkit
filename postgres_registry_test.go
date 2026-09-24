package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakePostgresHandle struct {
	operations []string
	closed     bool
}

func (handle *fakePostgresHandle) Execute(_ context.Context, operation string, _ postgresConnection) diagnosticResult {
	handle.operations = append(handle.operations, operation)
	return successfulPostgresResult(map[string]any{"backend_pid": 42, "operation": operation})
}

func (handle *fakePostgresHandle) Close(context.Context) error {
	handle.closed = true
	return nil
}

type fakePostgresOpener struct {
	handles []*fakePostgresHandle
	failure diagnosticResult
}

func (opener *fakePostgresOpener) Open(context.Context, postgresConnection, validatedTarget) (postgresConnectionHandle, diagnosticResult) {
	if opener.failure.Status != "" {
		return nil, opener.failure
	}
	handle := &fakePostgresHandle{}
	opener.handles = append(opener.handles, handle)
	return handle, diagnosticResult{}
}

func TestPostgresRegistryBoundsAndClosesRetainedConnections(t *testing.T) {
	opener := &fakePostgresOpener{}
	registry := newPostgresConnectionRegistry(opener)
	resolved, err := structuredPostgresInput("none", "").resolve()
	if err != nil {
		t.Fatal(err)
	}
	resolved.Lifecycle.Mode = "retained"
	for range postgresMaxRetainedConnections {
		if _, result, err := registry.Create(t.Context(), resolved, validatedTarget{}); err != nil || result.Status != "success" {
			t.Fatalf("result = %+v, err = %v", result, err)
		}
	}
	if _, _, err := registry.Create(t.Context(), resolved, validatedTarget{}); err == nil || err.Error() != "connection_capacity_exceeded" {
		t.Fatalf("capacity error = %v", err)
	}
	registry.CloseAll(t.Context())
	for _, handle := range opener.handles {
		if !handle.closed {
			t.Fatal("retained handle remained open")
		}
	}
	if _, _, err := registry.Create(t.Context(), resolved, validatedTarget{}); err == nil || err.Error() != "connection_registry_closed" {
		t.Fatalf("closed registry error = %v", err)
	}
}

func TestPostgresRegistryDoesNotRetainFailedConnection(t *testing.T) {
	registry := newPostgresConnectionRegistry(&fakePostgresOpener{failure: failedPostgresResult("authentication", "authentication_failed")})
	resolved, err := structuredPostgresInput("none", "").resolve()
	if err != nil {
		t.Fatal(err)
	}
	resolved.Lifecycle.Mode = "retained"
	view, result, err := registry.Create(t.Context(), resolved, validatedTarget{})
	if err != nil || result.Code != "authentication_failed" || view.ID != "" {
		t.Fatalf("view = %+v, result = %+v, err = %v", view, result, err)
	}
}

func TestPostgresRegistryRetainsSerializesAndDestroysConnection(t *testing.T) {
	opener := &fakePostgresOpener{}
	registry := newPostgresConnectionRegistry(opener)
	resolved, err := structuredPostgresInput("password", "secret").resolve()
	if err != nil {
		t.Fatal(err)
	}
	resolved.Lifecycle.Mode = "retained"
	view, result, err := registry.Create(t.Context(), resolved, validatedTarget{})
	if err != nil || result.Status != "success" || view.State != "ready" || view.DatabaseSource != "server_selected" {
		t.Fatalf("view = %+v, result = %+v, err = %v", view, result, err)
	}
	for _, operation := range []string{"arithmetic_check", "controlled_delay", "list_schemas"} {
		result, ok := registry.Execute(t.Context(), view.ID, operation)
		if !ok || result.Status != "success" {
			t.Fatalf("operation %s: result = %+v, ok = %v", operation, result, ok)
		}
	}
	if len(opener.handles) != 1 || strings.Join(opener.handles[0].operations, ",") != "connect,arithmetic_check,controlled_delay,list_schemas" {
		t.Fatalf("handles = %+v", opener.handles)
	}
	if !registry.Delete(t.Context(), view.ID) || !opener.handles[0].closed {
		t.Fatal("connection was not closed")
	}
	if _, ok := registry.Get(view.ID); ok {
		t.Fatal("deleted connection remains visible")
	}
}

func TestPostgresCapabilitiesDescribeContract(t *testing.T) {
	service, _ := testDiagnosticService(t)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/diagnostics/postgres/capabilities", nil)
	newHandlerWithConfig(handlerConfig{diagnostics: service}).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
	var capabilities postgresCapabilities
	if err := json.Unmarshal(response.Body.Bytes(), &capabilities); err != nil {
		t.Fatal(err)
	}
	operations := strings.Join(capabilities.Operations, ",")
	if capabilities.DatabaseRequired || capabilities.DefaultPort != 5432 || len(capabilities.CredentialTypes) != 3 || operations != "connect,arithmetic_check,controlled_delay,list_databases,list_schemas" {
		t.Fatalf("capabilities = %+v", capabilities)
	}
}

func TestPostgresRetainedConnectionHTTPContract(t *testing.T) {
	service, _ := testDiagnosticService(t)
	opener := &fakePostgresOpener{}
	service.registry = newPostgresConnectionRegistry(opener)
	handler := newHandlerWithConfig(handlerConfig{diagnostics: service})
	body := `{"connection":{"target":{"host":"203.0.113.10"},"identity":{"user":"operator"},"credential":{"type":"password","secret":"sentinel-secret"},"tls_config":{"mode":"disable"},"lifecycle":{"mode":"retained"}}}`
	create := diagnosticRequestFor(http.MethodPost, "/api/diagnostics/postgres/connections", body)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, create)
	if response.Code != http.StatusCreated || strings.Contains(response.Body.String(), "sentinel-secret") {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var created postgresConnectionCreateResponse
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	operationPath := "/api/diagnostics/postgres/connections/" + created.Connection.ID + "/operations"
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, diagnosticRequestFor(http.MethodPost, operationPath, `{"operation":"controlled_delay"}`))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "controlled_delay") {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, diagnosticRequestFor(http.MethodDelete, "/api/diagnostics/postgres/connections/"+created.Connection.ID, ""))
	if response.Code != http.StatusNoContent || !opener.handles[0].closed {
		t.Fatalf("status = %d, closed = %v", response.Code, opener.handles[0].closed)
	}
}

func TestPostgresLifecycleRoutesRejectWrongContracts(t *testing.T) {
	service, executor := testDiagnosticService(t)
	handler := newHandlerWithConfig(handlerConfig{diagnostics: service})
	structured := `{"target":{"host":"203.0.113.10"},"identity":{"user":"operator"},"credential":{"type":"none"},"tls_config":{"mode":"disable"}}`

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, diagnosticRequestFor(http.MethodPost, "/api/diagnostics/postgres/connections", `{"connection":`+structured+`}`))
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "retained_lifecycle_required") {
		t.Fatalf("create status = %d, body = %s", response.Code, response.Body.String())
	}

	retained := strings.TrimSuffix(structured, "}") + `,"lifecycle":{"mode":"retained"}}`
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, diagnosticRequestFor(http.MethodPost, "/api/diagnostics/postgres", `{"operation":"connect","connection":`+retained+`}`))
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "retained_connection_requires_creation") || executor.calls != 0 {
		t.Fatalf("one-shot status = %d, calls = %d, body = %s", response.Code, executor.calls, response.Body.String())
	}
}

func diagnosticRequestFor(method, path, body string) *http.Request {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer 0123456789abcdef")
	if method == http.MethodPost {
		request.Header.Set("Content-Type", "application/json")
	}
	return request
}
