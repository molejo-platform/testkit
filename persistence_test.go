package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestPersistenceEndpointIsDisabledWithoutAConfiguredFile(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/persistence", nil)
	response := httptest.NewRecorder()

	newHandlerWithConfig(handlerConfig{}).ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
	}
}

func TestPersistenceEndpointWritesAndReadsOnlyMarkerMetadata(t *testing.T) {
	marker := "stateful-marker-2026"
	path := filepath.Join(t.TempDir(), "marker")
	store, err := newPersistenceStore(path)
	if err != nil {
		t.Fatalf("new persistence store: %v", err)
	}
	handler := newHandlerWithConfig(handlerConfig{persistence: store})

	put := httptest.NewRequest(http.MethodPut, "/api/persistence", strings.NewReader(`{"value":"`+marker+`"}`))
	put.Header.Set("Content-Type", "application/json")
	putResponse := httptest.NewRecorder()
	handler.ServeHTTP(putResponse, put)
	if putResponse.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body = %s", putResponse.Code, putResponse.Body.String())
	}
	if strings.Contains(putResponse.Body.String(), marker) {
		t.Fatal("PUT response exposed marker content")
	}

	get := httptest.NewRequest(http.MethodGet, "/api/persistence", nil)
	getResponse := httptest.NewRecorder()
	newHandlerWithConfig(handlerConfig{persistence: store}).ServeHTTP(getResponse, get)
	if getResponse.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body = %s", getResponse.Code, getResponse.Body.String())
	}
	var state persistenceState
	if err := json.Unmarshal(getResponse.Body.Bytes(), &state); err != nil {
		t.Fatalf("decode state: %v", err)
	}
	expectedDigest := sha256.Sum256([]byte(marker))
	if !state.Exists || state.Size != len(marker) || state.SHA256 != hex.EncodeToString(expectedDigest[:]) {
		t.Fatalf("state = %+v", state)
	}
	if strings.Contains(getResponse.Body.String(), marker) {
		t.Fatal("GET response exposed marker content")
	}
}

func TestPersistenceEndpointRejectsOversizedMarkersWithoutChangingState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "marker")
	store, err := newPersistenceStore(path)
	if err != nil {
		t.Fatalf("new persistence store: %v", err)
	}
	handler := newHandlerWithConfig(handlerConfig{persistence: store})
	payload := `{"value":"` + strings.Repeat("x", maxPersistenceMarkerBytes+1) + `"}`

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPut, "/api/persistence", strings.NewReader(payload)))

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
	state, err := store.read()
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	if state.Exists {
		t.Fatal("oversized marker changed persistent state")
	}
}

func TestPersistenceEndpointAcceptsEscapedMarkerAtTheByteLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "marker")
	store, err := newPersistenceStore(path)
	if err != nil {
		t.Fatalf("new persistence store: %v", err)
	}
	marker := strings.Repeat("\n", maxPersistenceMarkerBytes)
	payload, err := json.Marshal(persistenceInput{Value: marker})
	if err != nil {
		t.Fatalf("encode marker: %v", err)
	}

	response := httptest.NewRecorder()
	newHandlerWithConfig(handlerConfig{persistence: store}).ServeHTTP(
		response,
		httptest.NewRequest(http.MethodPut, "/api/persistence", strings.NewReader(string(payload))),
	)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	state, err := store.read()
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	if state.Size != maxPersistenceMarkerBytes {
		t.Fatalf("size = %d, want %d", state.Size, maxPersistenceMarkerBytes)
	}
}
