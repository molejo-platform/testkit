package main

import (
	"encoding/json"
	"net/http"
)

func writeOK(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, response{Status: "ok", Version: version})
}

func listItems(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, []item{{ID: "item-1", Name: "First item"}, {ID: "item-2", Name: "Second item"}})
}

func apiEcho(writer http.ResponseWriter, request *http.Request) {
	if request.URL.Path != "/api/echo" {
		http.NotFound(writer, request)
		return
	}
	if request.Method != http.MethodPost {
		writer.Header().Set("Allow", http.MethodPost)
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var payload json.RawMessage
	if err := decodeJSONBody(writer, request, maxJSONBodyBytes, &payload); err != nil {
		return
	}
	writeJSON(writer, http.StatusOK, struct {
		Data   json.RawMessage `json:"data"`
		Method string          `json:"method"`
		Path   string          `json:"path"`
	}{Data: payload, Method: request.Method, Path: request.URL.Path})
}
