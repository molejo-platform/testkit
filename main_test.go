package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gorilla/websocket"
)

const suppliedCorrelationID = "018f47de-1234-7abc-8def-0123456789ab"

var uuidV7Pattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

type lockedBuffer struct {
	mutex sync.Mutex
	bytes.Buffer
}

type failingFlusherWriter struct {
	header     http.Header
	statusCode int
}

type blockingConnectionCloseHandler struct {
	closeStarted chan struct{}
	releaseClose chan struct{}
	closeOnce    sync.Once
}

func (handler *blockingConnectionCloseHandler) Enabled(context.Context, slog.Level) bool {
	return true
}

func (handler *blockingConnectionCloseHandler) Handle(_ context.Context, record slog.Record) error {
	protocol := ""
	record.Attrs(func(attribute slog.Attr) bool {
		if attribute.Key == "protocol" {
			protocol = attribute.Value.String()
		}
		return true
	})
	if record.Message == "connection closed" && protocol == protocolWebSocket {
		handler.closeOnce.Do(func() { close(handler.closeStarted) })
		<-handler.releaseClose
	}
	return nil
}

func (handler *blockingConnectionCloseHandler) WithAttrs([]slog.Attr) slog.Handler {
	return handler
}

func (handler *blockingConnectionCloseHandler) WithGroup(string) slog.Handler {
	return handler
}

func (writer *failingFlusherWriter) Header() http.Header {
	return writer.header
}

func (writer *failingFlusherWriter) WriteHeader(statusCode int) {
	writer.statusCode = statusCode
}

func (*failingFlusherWriter) Write([]byte) (int, error) {
	return 0, errors.New("controlled write failure")
}

func (*failingFlusherWriter) Flush() {}

func (buffer *lockedBuffer) Write(data []byte) (int, error) {
	buffer.mutex.Lock()
	defer buffer.mutex.Unlock()
	return buffer.Buffer.Write(data)
}

func (buffer *lockedBuffer) String() string {
	buffer.mutex.Lock()
	defer buffer.mutex.Unlock()
	return buffer.Buffer.String()
}

func newTestLogger() (*slog.Logger, *lockedBuffer) {
	output := &lockedBuffer{}
	return slog.New(slog.NewJSONHandler(output, nil)), output
}

func TestVersionMatchesRepositoryMetadata(t *testing.T) {
	versionFile, err := os.ReadFile("VERSION")
	if err != nil {
		t.Fatalf("read VERSION: %v", err)
	}
	sourceVersion := strings.TrimSpace(string(versionFile))
	if !regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`).MatchString(sourceVersion) {
		t.Fatalf("VERSION = %q, want vMAJOR.MINOR.PATCH", sourceVersion)
	}
	if version != sourceVersion {
		t.Fatalf("binary version = %q, want repository version %q", version, sourceVersion)
	}

	packageFile, err := os.ReadFile("package.json")
	if err != nil {
		t.Fatalf("read package.json: %v", err)
	}
	var metadata struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(packageFile, &metadata); err != nil {
		t.Fatalf("decode package.json: %v", err)
	}
	if sourceVersion != "v"+metadata.Version {
		t.Fatalf("VERSION = %q, package.json version = %q", sourceVersion, metadata.Version)
	}
}

func logRecords(t *testing.T, output *lockedBuffer) []map[string]interface{} {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil
	}
	records := make([]map[string]interface{}, 0, len(lines))
	for _, line := range lines {
		var record map[string]interface{}
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("decode log record %q: %v", line, err)
		}
		records = append(records, record)
	}
	return records
}

func waitForLogEvent(t *testing.T, output *lockedBuffer, event string, count int) []map[string]interface{} {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		records := logRecords(t, output)
		matches := 0
		for _, record := range records {
			if record["event"] == event {
				matches++
			}
		}
		if matches >= count {
			return records
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d %q log records: %s", count, event, output.String())
	return nil
}

func recordsForEvent(records []map[string]interface{}, event string) []map[string]interface{} {
	matches := make([]map[string]interface{}, 0, len(records))
	for _, record := range records {
		if record["event"] == event {
			matches = append(matches, record)
		}
	}
	return matches
}

func requireUUIDv7(t *testing.T, value string) {
	t.Helper()
	if !uuidV7Pattern.MatchString(value) {
		t.Fatalf("correlation ID = %q, want a canonical UUIDv7", value)
	}
}

func TestCorrelationIDValidation(t *testing.T) {
	for _, test := range []struct {
		name  string
		value string
		valid bool
	}{
		{name: "canonical UUIDv7", value: suppliedCorrelationID, valid: true},
		{name: "uppercase", value: strings.ToUpper(suppliedCorrelationID), valid: true},
		{name: "mixed case", value: "018F47de-1234-7Abc-8deF-0123456789aB", valid: true},
		{name: "UUIDv4", value: "018f47de-1234-4abc-8def-0123456789ab"},
		{name: "invalid variant", value: "018f47de-1234-7abc-7def-0123456789ab"},
		{name: "arbitrary value", value: "do-not-log"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := isUUIDv7(test.value); got != test.valid {
				t.Fatalf("isUUIDv7(%q) = %t, want %t", test.value, got, test.valid)
			}
		})
	}
}

func TestGeneratedUUIDv7CarriesCurrentUnixTime(t *testing.T) {
	before := time.Now().UnixMilli()
	correlationID := newUUIDv7()
	after := time.Now().UnixMilli()

	requireUUIDv7(t, correlationID)
	timestamp, err := strconv.ParseInt(correlationID[:8]+correlationID[9:13], 16, 64)
	if err != nil {
		t.Fatalf("parse UUIDv7 timestamp: %v", err)
	}
	if timestamp < before || timestamp > after {
		t.Fatalf("UUIDv7 timestamp = %d, want between %d and %d", timestamp, before, after)
	}
}

func TestHandlerRoutes(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		path       string
		statusCode int
	}{
		{name: "root redirects", method: http.MethodGet, path: "/", statusCode: http.StatusFound},
		{name: "websocket page redirects", method: http.MethodGet, path: "/websocket", statusCode: http.StatusFound},
		{name: "rest lab redirects", method: http.MethodGet, path: "/rest", statusCode: http.StatusFound},
		{name: "graphql lab redirects", method: http.MethodGet, path: "/graphql-lab", statusCode: http.StatusFound},
		{name: "sse lab redirects", method: http.MethodGet, path: "/sse", statusCode: http.StatusFound},
		{name: "english home", method: http.MethodGet, path: "/en/", statusCode: http.StatusOK},
		{name: "english websocket page", method: http.MethodGet, path: "/en/websocket", statusCode: http.StatusOK},
		{name: "english rest lab", method: http.MethodGet, path: "/en/rest", statusCode: http.StatusOK},
		{name: "english graphql lab", method: http.MethodGet, path: "/en/graphql-lab", statusCode: http.StatusOK},
		{name: "english sse lab", method: http.MethodGet, path: "/en/sse", statusCode: http.StatusOK},
		{name: "portuguese home", method: http.MethodGet, path: "/pt-BR/", statusCode: http.StatusOK},
		{name: "spanish websocket page", method: http.MethodGet, path: "/es-AR/websocket", statusCode: http.StatusOK},
		{name: "liveness", method: http.MethodGet, path: "/healthz", statusCode: http.StatusOK},
		{name: "readiness", method: http.MethodGet, path: "/readyz", statusCode: http.StatusOK},
		{name: "not ready", method: http.MethodGet, path: "/not-ready", statusCode: http.StatusServiceUnavailable},
		{name: "rest status", method: http.MethodGet, path: "/api/status", statusCode: http.StatusOK},
		{name: "rest items", method: http.MethodGet, path: "/api/items", statusCode: http.StatusOK},
		{name: "unknown", method: http.MethodGet, path: "/unknown", statusCode: http.StatusNotFound},
		{name: "unknown locale", method: http.MethodGet, path: "/fr/", statusCode: http.StatusNotFound},
		{name: "method not allowed", method: http.MethodPost, path: "/readyz", statusCode: http.StatusMethodNotAllowed},
		{name: "websocket page method not allowed", method: http.MethodPost, path: "/websocket", statusCode: http.StatusMethodNotAllowed},
		{name: "localized page method not allowed", method: http.MethodPost, path: "/pt-BR/", statusCode: http.StatusMethodNotAllowed},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, test.path, nil)
			responseRecorder := httptest.NewRecorder()

			newHandlerWithConfig(handlerConfig{sseInterval: time.Millisecond}).ServeHTTP(responseRecorder, request)

			if responseRecorder.Code != test.statusCode {
				t.Fatalf("status code = %d, want %d", responseRecorder.Code, test.statusCode)
			}
		})
	}
}

func TestRESTRequestLog(t *testing.T) {
	logger, output := newTestLogger()
	request := httptest.NewRequest(http.MethodGet, "/api/status?ignored=value", nil)
	responseRecorder := httptest.NewRecorder()

	newHandlerWithConfig(handlerConfig{logger: logger}).ServeHTTP(responseRecorder, request)

	records := logRecords(t, output)
	if len(records) != 1 {
		t.Fatalf("log record count = %d, want 1: %s", len(records), output.String())
	}
	record := records[0]
	for key, want := range map[string]interface{}{
		"event":          "http.request.completed",
		"http_method":    http.MethodGet,
		"route":          "/api/status",
		"status_code":    float64(http.StatusOK),
		"response_bytes": float64(responseRecorder.Body.Len()),
	} {
		if got := record[key]; got != want {
			t.Errorf("log attribute %q = %#v, want %#v", key, got, want)
		}
	}
	if duration, ok := record["duration_ms"].(float64); !ok || duration < 0 {
		t.Errorf("duration_ms = %#v, want a non-negative number", record["duration_ms"])
	}
	if strings.Contains(output.String(), "ignored") {
		t.Fatalf("request query leaked into log: %s", output.String())
	}
}

func TestRESTCorrelationID(t *testing.T) {
	t.Run("uses valid client value", func(t *testing.T) {
		logger, output := newTestLogger()
		request := httptest.NewRequest(http.MethodGet, "/api/status", nil)
		request.Header.Set("X-Testkit-Correlation-ID", suppliedCorrelationID)
		responseRecorder := httptest.NewRecorder()

		newHandlerWithConfig(handlerConfig{logger: logger}).ServeHTTP(responseRecorder, request)

		if got := responseRecorder.Header().Get("X-Testkit-Correlation-ID"); got != suppliedCorrelationID {
			t.Fatalf("response correlation ID = %q, want %q", got, suppliedCorrelationID)
		}
		records := logRecords(t, output)
		if len(records) != 1 || records[0]["correlation_id"] != suppliedCorrelationID {
			t.Fatalf("request facts = %#v, want supplied correlation ID", records)
		}
	})

	t.Run("normalizes valid uppercase value", func(t *testing.T) {
		logger, output := newTestLogger()
		request := httptest.NewRequest(http.MethodGet, "/api/status", nil)
		request.Header.Set("X-Testkit-Correlation-ID", strings.ToUpper(suppliedCorrelationID))
		responseRecorder := httptest.NewRecorder()

		newHandlerWithConfig(handlerConfig{logger: logger}).ServeHTTP(responseRecorder, request)

		if got := responseRecorder.Header().Get("X-Testkit-Correlation-ID"); got != suppliedCorrelationID {
			t.Fatalf("response correlation ID = %q, want normalized %q", got, suppliedCorrelationID)
		}
		records := logRecords(t, output)
		if len(records) != 1 || records[0]["correlation_id"] != suppliedCorrelationID {
			t.Fatalf("request facts = %#v, want normalized correlation ID", records)
		}
	})

	t.Run("generates distinct values when absent", func(t *testing.T) {
		logger, output := newTestLogger()
		seen := make(map[string]bool)
		for range 2 {
			request := httptest.NewRequest(http.MethodGet, "/api/status", nil)
			responseRecorder := httptest.NewRecorder()

			newHandlerWithConfig(handlerConfig{logger: logger}).ServeHTTP(responseRecorder, request)

			correlationID := responseRecorder.Header().Get("X-Testkit-Correlation-ID")
			requireUUIDv7(t, correlationID)
			if seen[correlationID] {
				t.Fatalf("generated duplicate correlation ID %q", correlationID)
			}
			seen[correlationID] = true
		}
		for _, record := range logRecords(t, output) {
			correlationID, ok := record["correlation_id"].(string)
			if !ok || !seen[correlationID] {
				t.Fatalf("request correlation ID = %#v, want one returned to the client", record["correlation_id"])
			}
		}
	})

	t.Run("replaces invalid client value", func(t *testing.T) {
		logger, output := newTestLogger()
		request := httptest.NewRequest(http.MethodGet, "/api/status", nil)
		request.Header.Set("X-Testkit-Correlation-ID", "do-not-log")
		responseRecorder := httptest.NewRecorder()

		newHandlerWithConfig(handlerConfig{logger: logger}).ServeHTTP(responseRecorder, request)

		correlationID := responseRecorder.Header().Get("X-Testkit-Correlation-ID")
		requireUUIDv7(t, correlationID)
		if strings.Contains(output.String(), "do-not-log") {
			t.Fatalf("invalid client correlation ID leaked into log: %s", output.String())
		}
		records := logRecords(t, output)
		if len(records) != 1 || records[0]["correlation_id"] != correlationID {
			t.Fatalf("request facts = %#v, want generated correlation ID %q", records, correlationID)
		}
	})
}

func TestRESTRequestFactsCoverFailuresAndRedaction(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		body       string
		statusCode int
	}{
		{name: "success", method: http.MethodPost, body: `{"secret":"do-not-log"}`, statusCode: http.StatusOK},
		{name: "invalid JSON", method: http.MethodPost, body: `{"secret":"do-not-log"`, statusCode: http.StatusBadRequest},
		{name: "method not allowed", method: http.MethodGet, statusCode: http.StatusMethodNotAllowed},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			logger, output := newTestLogger()
			request := httptest.NewRequest(test.method, "/api/echo?token=do-not-log", strings.NewReader(test.body))
			responseRecorder := httptest.NewRecorder()

			newHandlerWithConfig(handlerConfig{logger: logger}).ServeHTTP(responseRecorder, request)

			records := logRecords(t, output)
			if len(records) != 1 {
				t.Fatalf("log record count = %d, want 1: %s", len(records), output.String())
			}
			record := records[0]
			if record["status_code"] != float64(test.statusCode) {
				t.Errorf("status_code = %#v, want %d", record["status_code"], test.statusCode)
			}
			if record["response_bytes"] != float64(responseRecorder.Body.Len()) {
				t.Errorf("response_bytes = %#v, want %d", record["response_bytes"], responseRecorder.Body.Len())
			}
			if strings.Contains(output.String(), "do-not-log") || strings.Contains(output.String(), "token") {
				t.Fatalf("request data leaked into log: %s", output.String())
			}
		})
	}
}

func TestConnectionStatsTracksBothProtocols(t *testing.T) {
	var stats connectionStats

	protocolActive, totalActive, sequence := stats.change(protocolWebSocket, 1)
	if protocolActive != 1 || totalActive != 1 || sequence != 1 {
		t.Fatalf("first change = (%d, %d, %d), want (1, 1, 1)", protocolActive, totalActive, sequence)
	}
	protocolActive, totalActive, sequence = stats.change(protocolSSE, 1)
	if protocolActive != 1 || totalActive != 2 || sequence != 2 {
		t.Fatalf("second change = (%d, %d, %d), want (1, 2, 2)", protocolActive, totalActive, sequence)
	}
	protocolActive, totalActive, sequence = stats.change(protocolWebSocket, -1)
	if protocolActive != 0 || totalActive != 1 || sequence != 3 {
		t.Fatalf("third change = (%d, %d, %d), want (0, 1, 3)", protocolActive, totalActive, sequence)
	}
	protocolActive, totalActive, sequence = stats.change(protocolSSE, -1)
	if protocolActive != 0 || totalActive != 0 || sequence != 4 {
		t.Fatalf("last change = (%d, %d, %d), want (0, 0, 4)", protocolActive, totalActive, sequence)
	}
}

func TestConcurrentConnectionFactsCarryTransitionOrder(t *testing.T) {
	// The logger may emit concurrent records out of order, so each fact must carry
	// the sequence assigned atomically with its state transition.
	const connectionCount = 256
	logger, output := newTestLogger()
	var stats connectionStats
	start := make(chan struct{})
	connections := make(chan func(), connectionCount)
	var waitGroup sync.WaitGroup
	for range connectionCount {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			<-start
			connections <- observeConnection(context.Background(), logger, &stats, protocolWebSocket, "/ws", newUUIDv7())
		}()
	}
	close(start)
	waitGroup.Wait()
	close(connections)
	closeConnections := make([]func(), 0, connectionCount)
	for closeConnection := range connections {
		closeConnections = append(closeConnections, closeConnection)
	}
	t.Cleanup(func() {
		for _, closeConnection := range closeConnections {
			closeConnection()
		}
	})

	opened := recordsForEvent(logRecords(t, output), "connection.opened")
	if len(opened) != connectionCount {
		t.Fatalf("opened log count = %d, want %d", len(opened), connectionCount)
	}
	seenSequences := make([]bool, connectionCount+1)
	for _, record := range opened {
		sequence, ok := record["connection_sequence"].(float64)
		if !ok || sequence < 1 || sequence > connectionCount || sequence != float64(int(sequence)) {
			t.Fatalf("connection_sequence = %#v, want an integer between 1 and %d", record["connection_sequence"], connectionCount)
		}
		index := int(sequence)
		if seenSequences[index] {
			t.Fatalf("connection_sequence %d was emitted more than once", index)
		}
		seenSequences[index] = true
		if record["protocol_active"] != sequence || record["total_active"] != sequence {
			t.Fatalf("sequence %v has counts (%v, %v), want (%v, %v)", sequence, record["protocol_active"], record["total_active"], sequence, sequence)
		}
	}
}

func TestActiveConnectionSnapshotSkipsEmptyState(t *testing.T) {
	logger, output := newTestLogger()
	app := newApplication(handlerConfig{logger: logger})

	app.logActiveConnections(context.Background())
	if records := logRecords(t, output); len(records) != 0 {
		t.Fatalf("empty snapshot emitted logs: %s", output.String())
	}
	app.connections.change(protocolSSE, 1)
	app.logActiveConnections(context.Background())
	records := logRecords(t, output)
	record := records[len(records)-1]
	if record["websocket_active"] != float64(0) || record["sse_active"] != float64(1) || record["total_active"] != float64(1) || record["connection_sequence"] != float64(1) {
		t.Fatalf("snapshot log = %#v, want one active SSE", record)
	}
	if _, ok := record["correlation_id"]; ok {
		t.Fatalf("aggregate snapshot contains a connection correlation ID: %#v", record)
	}
}

func TestActiveConnectionReporterLogsOnTickAndStopsWhenTicksClose(t *testing.T) {
	logger, output := newTestLogger()
	app := newApplication(handlerConfig{logger: logger})
	app.connections.change(protocolWebSocket, 1)
	ticks := make(chan time.Time)
	done := make(chan struct{})
	go func() {
		defer close(done)
		app.reportActiveConnections(context.Background(), ticks)
	}()

	ticks <- time.Now()
	waitForLogEvent(t, output, "connections.snapshot", 1)
	close(ticks)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("connection reporter did not stop after its tick channel closed")
	}
}

func TestServeWaitsForWebSocketCloseFactDuringShutdown(t *testing.T) {
	handler := &blockingConnectionCloseHandler{
		closeStarted: make(chan struct{}),
		releaseClose: make(chan struct{}),
	}
	var releaseOnce sync.Once
	releaseClose := func() {
		releaseOnce.Do(func() { close(handler.releaseClose) })
	}
	t.Cleanup(releaseClose)

	app := newApplication(handlerConfig{logger: slog.New(handler)})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	serveResult := make(chan error, 1)
	go func() { serveResult <- serve(ctx, listener, app) }()

	sseResponse, err := http.Get("http://" + listener.Addr().String() + "/events")
	if err != nil {
		t.Fatalf("open SSE stream: %v", err)
	}
	t.Cleanup(func() { _ = sseResponse.Body.Close() })
	connection, _, err := websocket.DefaultDialer.Dial("ws://"+listener.Addr().String()+"/ws", nil)
	if err != nil {
		t.Fatalf("dial WebSocket: %v", err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	deadline := time.Now().Add(time.Second)
	for {
		webSocketActive, sseActive, _ := app.connections.snapshot()
		if webSocketActive == 1 && sseActive == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("active connections = (%d, %d), want (1, 1)", webSocketActive, sseActive)
		}
		time.Sleep(time.Millisecond)
	}

	cancel()
	select {
	case <-handler.closeStarted:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for WebSocket close fact")
	}
	select {
	case err := <-serveResult:
		t.Fatalf("serve returned before the WebSocket close fact completed: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	releaseClose()
	select {
	case err := <-serveResult:
		if err != nil {
			t.Fatalf("serve: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("serve did not return after the WebSocket close fact completed")
	}
	webSocketActive, sseActive, _ := app.connections.snapshot()
	if webSocketActive != 0 || sseActive != 0 {
		t.Fatalf("active connections = (%d, %d), want (0, 0)", webSocketActive, sseActive)
	}
}

func TestRejectedTransportsDoNotBecomeActiveConnections(t *testing.T) {
	t.Run("SSE without flushing", func(t *testing.T) {
		logger, output := newTestLogger()
		var stats connectionStats
		responseRecorder := httptest.NewRecorder()
		writer := &responseLogWriter{ResponseWriter: responseRecorder}
		request := httptest.NewRequest(http.MethodGet, "/events", nil)

		events(writer, request, time.Millisecond, logger, &stats)

		if responseRecorder.Code != http.StatusInternalServerError {
			t.Fatalf("status code = %d, want %d", responseRecorder.Code, http.StatusInternalServerError)
		}
		webSocketActive, sseActive, _ := stats.snapshot()
		if webSocketActive != 0 || sseActive != 0 {
			t.Fatalf("active connections = (%d, %d), want (0, 0)", webSocketActive, sseActive)
		}
		if records := logRecords(t, output); len(records) != 0 {
			t.Fatalf("rejected SSE emitted connection facts: %s", output.String())
		}
	})

	t.Run("invalid WebSocket upgrade", func(t *testing.T) {
		logger, output := newTestLogger()
		app := newApplication(handlerConfig{logger: logger})
		request := httptest.NewRequest(http.MethodGet, "/ws", nil)
		responseRecorder := httptest.NewRecorder()

		app.handler.ServeHTTP(responseRecorder, request)

		if responseRecorder.Code != http.StatusBadRequest {
			t.Fatalf("status code = %d, want %d", responseRecorder.Code, http.StatusBadRequest)
		}
		webSocketActive, sseActive, _ := app.connections.snapshot()
		if webSocketActive != 0 || sseActive != 0 {
			t.Fatalf("active connections = (%d, %d), want (0, 0)", webSocketActive, sseActive)
		}
		if records := logRecords(t, output); len(records) != 0 {
			t.Fatalf("rejected WebSocket emitted connection facts: %s", output.String())
		}
	})

	t.Run("WebSocket after hub shutdown", func(t *testing.T) {
		logger, output := newTestLogger()
		app := newApplication(handlerConfig{logger: logger})
		app.hub.close()
		server := httptest.NewServer(app.handler)
		t.Cleanup(server.Close)

		connection, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/ws", nil)
		if err != nil {
			t.Fatalf("dial WebSocket: %v", err)
		}
		_ = connection.Close()
		webSocketActive, sseActive, _ := app.connections.snapshot()
		if webSocketActive != 0 || sseActive != 0 {
			t.Fatalf("active connections = (%d, %d), want (0, 0)", webSocketActive, sseActive)
		}
		if records := logRecords(t, output); len(records) != 0 {
			t.Fatalf("rejected WebSocket emitted connection facts: %s", output.String())
		}
	})
}

func TestSSEWriteFailureClosesOperationalFact(t *testing.T) {
	logger, output := newTestLogger()
	var stats connectionStats
	writer := &failingFlusherWriter{header: make(http.Header)}
	request := httptest.NewRequest(http.MethodGet, "/events", nil)

	events(writer, request, time.Millisecond, logger, &stats)

	if writer.statusCode != http.StatusOK {
		t.Fatalf("status code = %d, want %d", writer.statusCode, http.StatusOK)
	}
	opened := recordsForEvent(logRecords(t, output), "connection.opened")
	closed := recordsForEvent(logRecords(t, output), "connection.closed")
	if len(opened) != 1 || opened[0]["protocol_active"] != float64(1) || opened[0]["connection_sequence"] != float64(1) {
		t.Fatalf("opened facts = %#v, want one active SSE", opened)
	}
	if len(closed) != 1 || closed[0]["protocol_active"] != float64(0) || closed[0]["total_active"] != float64(0) || closed[0]["connection_sequence"] != float64(2) {
		t.Fatalf("closed facts = %#v, want no active connections", closed)
	}
	webSocketActive, sseActive, _ := stats.snapshot()
	if webSocketActive != 0 || sseActive != 0 {
		t.Fatalf("active connections = (%d, %d), want (0, 0)", webSocketActive, sseActive)
	}
}

func TestStaticAssets(t *testing.T) {
	for _, test := range []struct {
		path        string
		contentType string
		contains    string
	}{
		{path: "/static/style.css", contentType: "text/css; charset=utf-8", contains: "font-family"},
		{path: "/static/app.js", contentType: "text/javascript; charset=utf-8", contains: "mountWebSocketClient"},
			{path: "/static/correlation-id.js", contentType: "text/javascript; charset=utf-8", contains: "createCorrelationID"},
			{path: "/static/brand/molejo-horizontal-on-dark.webp", contentType: "image/webp", contains: "RIFF"},
			{path: "/static/brand/molejo-symbol-on-dark.webp", contentType: "image/webp", contains: "RIFF"},
	} {
		t.Run(test.path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, test.path, nil)
			responseRecorder := httptest.NewRecorder()

			newHandler().ServeHTTP(responseRecorder, request)

			if responseRecorder.Code != http.StatusOK {
				t.Fatalf("status code = %d, want %d", responseRecorder.Code, http.StatusOK)
			}
			if got := responseRecorder.Header().Get("Content-Type"); got != test.contentType {
				t.Fatalf("Content-Type = %q, want %q", got, test.contentType)
			}
			if !strings.Contains(responseRecorder.Body.String(), test.contains) {
				t.Fatalf("body does not contain %q: %s", test.contains, responseRecorder.Body.String())
			}
			if got := responseRecorder.Header().Get("Cache-Control"); got != "no-store" {
				t.Fatalf("Cache-Control = %q, want no-store", got)
			}
		})
	}
}

func TestStaticAssetVersionChangesWithContent(t *testing.T) {
	first := fstest.MapFS{
		"static/app.js":    {Data: []byte("first")},
		"static/style.css": {Data: []byte("style")},
	}
	changed := fstest.MapFS{
		"static/app.js":    {Data: []byte("second")},
		"static/style.css": {Data: []byte("style")},
	}

	firstVersion, err := staticAssetVersionFromFS(first)
	if err != nil {
		t.Fatalf("calculate first static asset version: %v", err)
	}
	changedVersion, err := staticAssetVersionFromFS(changed)
	if err != nil {
		t.Fatalf("calculate changed static asset version: %v", err)
	}
	if firstVersion == changedVersion {
		t.Fatalf("static asset version did not change: %q", firstVersion)
	}
	if matched, _ := regexp.MatchString(`^[0-9a-f]{32}$`, firstVersion); !matched {
		t.Fatalf("static asset version = %q, want 128-bit hexadecimal digest", firstVersion)
	}
}

func TestVersionedStaticAssetsUseImmutableCache(t *testing.T) {
	for _, test := range []struct {
		name     string
		contains string
	}{
		{name: "app.js", contains: "mountWebSocketClient"},
		{name: "rest-ui.js", contains: "data-rest-send"},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/static/"+staticAssetVersion+"/"+test.name, nil)
			responseRecorder := httptest.NewRecorder()

			newHandler().ServeHTTP(responseRecorder, request)

			if responseRecorder.Code != http.StatusOK {
				t.Fatalf("status code = %d, want %d", responseRecorder.Code, http.StatusOK)
			}
			if got := responseRecorder.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
				t.Fatalf("Cache-Control = %q, want immutable cache", got)
			}
			if !strings.Contains(responseRecorder.Body.String(), test.contains) {
				t.Fatalf("versioned asset does not contain %q", test.contains)
			}
		})
	}
}

func TestUnknownStaticAssetVersionIsNotCached(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/static/00000000000000000000000000000000/app.js", nil)
	responseRecorder := httptest.NewRecorder()

	newHandler().ServeHTTP(responseRecorder, request)

	if responseRecorder.Code != http.StatusNotFound {
		t.Fatalf("status code = %d, want %d", responseRecorder.Code, http.StatusNotFound)
	}
	if got := responseRecorder.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
}

func TestDashboardRendersWebSocketConsole(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/en/", nil)
	responseRecorder := httptest.NewRecorder()

	newHandler().ServeHTTP(responseRecorder, request)

	if responseRecorder.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", responseRecorder.Code, http.StatusOK)
	}
	if got := responseRecorder.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Fatalf("Content-Type = %q, want text/html; charset=utf-8", got)
	}
	if got := responseRecorder.Header().Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("Cache-Control = %q, want no-cache", got)
	}
	for _, want := range []string{
		`<html lang="en">`,
		`alt="Molejo"`,
		"Testkit",
		"Transport console",
		"build " + version,
		"Open lab",
		"/en/rest",
		"/en/graphql-lab",
		"/en/sse",
		"/en/websocket",
		"4 active",
		`data-locale="en"`,
		"/static/" + staticAssetVersion + "/style.css",
		"/static/" + staticAssetVersion + "/app.js",
	} {
		if !strings.Contains(responseRecorder.Body.String(), want) {
			t.Fatalf("dashboard does not contain %q", want)
		}
	}
	if got := strings.Count(responseRecorder.Body.String(), "data-ws-client"); got != 0 {
		t.Fatalf("home contains %d WebSocket clients, want 0", got)
	}
}

func TestProtocolLabPagesRenderLocalizedContracts(t *testing.T) {
	tests := []struct {
		path     string
		locale   string
		content  string
		endpoint string
		marker   string
		back     string
	}{
		{path: "/en/rest", locale: "en", content: "REST Lab", endpoint: "/api/*", marker: "data-rest-lab", back: "Back to surface map"},
		{path: "/pt-BR/graphql-lab", locale: "pt-BR", content: "Laboratório GraphQL", endpoint: "/graphql", marker: "data-graphql-lab", back: "Voltar ao mapa de superfícies"},
		{path: "/es-AR/sse", locale: "es-AR", content: "Laboratorio SSE", endpoint: "/events", marker: "data-sse-lab", back: "Volver al mapa de superficies"},
	}

	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, test.path, nil)
			responseRecorder := httptest.NewRecorder()
			newHandler().ServeHTTP(responseRecorder, request)

			body := responseRecorder.Body.String()
			if responseRecorder.Code != http.StatusOK {
				t.Fatalf("status code = %d, want %d", responseRecorder.Code, http.StatusOK)
			}
			for _, want := range []string{
				`<html lang="` + test.locale + `">`,
				test.content,
				test.endpoint,
				test.marker,
				test.back,
			} {
				if !strings.Contains(body, want) {
					t.Fatalf("response does not contain %q", want)
				}
			}
		})
	}
}

func TestGraphQLLabFooterHasExpectedSeparators(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/en/graphql-lab", nil)
	responseRecorder := httptest.NewRecorder()

	newHandler().ServeHTTP(responseRecorder, request)

	if got := strings.Count(responseRecorder.Body.String(), `<span class="footer-separator" aria-hidden="true">/</span>`); got != 2 {
		t.Fatalf("GraphQL lab footer separators = %d, want 2", got)
	}
}

func TestPageFootersIncludeVersion(t *testing.T) {
	for _, path := range []string{"/en/", "/en/websocket", "/en/rest", "/en/graphql-lab", "/en/sse"} {
		t.Run(path, func(t *testing.T) {
			responseRecorder := httptest.NewRecorder()
			newHandler().ServeHTTP(responseRecorder, httptest.NewRequest(http.MethodGet, path, nil))

			body := responseRecorder.Body.String()
			footerStart := strings.Index(body, `<footer class="page-footer">`)
			if footerStart == -1 {
				t.Fatal("response does not contain page footer")
			}
			footerEnd := strings.Index(body[footerStart:], `</footer>`)
			if footerEnd == -1 {
				t.Fatal("page footer is not closed")
			}
			footer := body[footerStart : footerStart+footerEnd]
			if want := "Same-origin browser fixture · build " + version; !strings.Contains(footer, want) {
				t.Fatalf("page footer does not contain %q", want)
			}
		})
	}
}

func TestPageFootersLinkToMolejoWithBuildAttribution(t *testing.T) {
	for _, test := range []struct {
		path  string
		label string
	}{
		{path: "/en/", label: "Powered by Molejo"},
		{path: "/pt-BR/", label: "Desenvolvido por Molejo"},
		{path: "/es-AR/", label: "Desarrollado por Molejo"},
	} {
		t.Run(test.path, func(t *testing.T) {
			responseRecorder := httptest.NewRecorder()
			newHandler().ServeHTTP(responseRecorder, httptest.NewRequest(http.MethodGet, test.path, nil))

			body := responseRecorder.Body.String()
			if !strings.Contains(body, test.label) {
				t.Fatalf("page footer does not contain %q", test.label)
			}
			wantURL := `href="https://molejo.dev/?utm_campaign=testkit&amp;utm_content=footer-build-` + version + `&amp;utm_medium=referral&amp;utm_source=molejo-testkit"`
			if !strings.Contains(body, wantURL) {
				t.Fatalf("page footer does not contain %q", wantURL)
			}
		})
	}
}

func TestResponsesIncludeVersionHeader(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		path       string
		cancel     bool
		statusCode int
	}{
		{name: "REST", method: http.MethodGet, path: "/api/items", statusCode: http.StatusOK},
		{name: "not found", method: http.MethodGet, path: "/missing", statusCode: http.StatusNotFound},
		{name: "method not allowed", method: http.MethodPost, path: "/healthz", statusCode: http.StatusMethodNotAllowed},
		{name: "localized redirect", method: http.MethodGet, path: "/", statusCode: http.StatusFound},
		{name: "static asset", method: http.MethodGet, path: "/static/app.js", statusCode: http.StatusOK},
		{name: "SSE", method: http.MethodGet, path: "/events", cancel: true, statusCode: http.StatusOK},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, test.path, nil)
			if test.cancel {
				ctx, cancel := context.WithCancel(request.Context())
				cancel()
				request = request.WithContext(ctx)
			}
			responseRecorder := httptest.NewRecorder()

			newHandler().ServeHTTP(responseRecorder, request)

			if responseRecorder.Code != test.statusCode {
				t.Fatalf("status code = %d, want %d", responseRecorder.Code, test.statusCode)
			}
			if got := responseRecorder.Header().Get("Testkit-Version"); got != version {
				t.Fatalf("Testkit-Version = %q, want %q", got, version)
			}
		})
	}
}

func TestWebSocketHandshakeIncludesVersionHeader(t *testing.T) {
	server := httptest.NewServer(newHandler())
	t.Cleanup(server.Close)

	connection, response, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/ws", nil)
	if err != nil {
		t.Fatalf("dial WebSocket: %v", err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	if got := response.Header.Get("Testkit-Version"); got != version {
		t.Fatalf("Testkit-Version = %q, want %q", got, version)
	}
}

func TestDocumentationListsBrowserLabAliases(t *testing.T) {
	for _, path := range []string{"README.md", "docs/pt-BR/README.md", "docs/es-AR/README.md"} {
		t.Run(path, func(t *testing.T) {
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read documentation: %v", err)
			}
			for _, alias := range []string{"GET /rest", "GET /graphql-lab", "GET /sse"} {
				if !strings.Contains(string(content), alias) {
					t.Fatalf("documentation does not list browser alias %q", alias)
				}
			}
		})
	}
}

func TestWebSocketPageRendersBreadcrumbsAndConsole(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/en/websocket", nil)
	responseRecorder := httptest.NewRecorder()

	newHandler().ServeHTTP(responseRecorder, request)

	if responseRecorder.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", responseRecorder.Code, http.StatusOK)
	}
	for _, want := range []string{
		"WebSocket",
		"lab.",
		"aria-label=\"Breadcrumb\"",
		"aria-current=\"page\">WebSocket",
		"href=\"/en/\">Home",
		"Back to surface map",
		"build " + version,
		"Client A",
		"Client B",
		"/static/" + staticAssetVersion + "/app.js",
	} {
		if !strings.Contains(responseRecorder.Body.String(), want) {
			t.Fatalf("WebSocket page does not contain %q", want)
		}
	}
	if got := strings.Count(responseRecorder.Body.String(), "data-ws-client"); got != 2 {
		t.Fatalf("WebSocket page contains %d clients, want 2", got)
	}
}

func TestLocaleAliasRedirectsByPreference(t *testing.T) {
	tests := []struct {
		name           string
		path           string
		acceptLanguage string
		cookie         string
		query          string
		location       string
	}{
		{name: "accept language", path: "/", acceptLanguage: "pt-BR, en;q=0.8", location: "/pt-BR/"},
		{name: "cookie wins over header", path: "/websocket", acceptLanguage: "en", cookie: "es-AR", location: "/es-AR/websocket"},
		{name: "lab alias uses preference", path: "/rest", acceptLanguage: "pt-BR", location: "/pt-BR/rest"},
		{name: "query wins over cookie", path: "/", acceptLanguage: "en", cookie: "pt-BR", query: "?lang=es-AR", location: "/es-AR/"},
		{name: "fallback", path: "/", acceptLanguage: "de-DE", location: "/en/"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, test.path+test.query, nil)
			request.Header.Set("Accept-Language", test.acceptLanguage)
			if test.cookie != "" {
				request.AddCookie(&http.Cookie{Name: "testkit_locale", Value: test.cookie})
			}
			responseRecorder := httptest.NewRecorder()

			newHandler().ServeHTTP(responseRecorder, request)

			if responseRecorder.Code != http.StatusFound {
				t.Fatalf("status code = %d, want %d", responseRecorder.Code, http.StatusFound)
			}
			if got := responseRecorder.Header().Get("Location"); got != test.location {
				t.Fatalf("Location = %q, want %q", got, test.location)
			}
			if got := responseRecorder.Header().Values("Vary"); len(got) != 2 {
				t.Fatalf("Vary = %q, want Accept-Language and Cookie", got)
			}
		})
	}
}

func TestLocalizedPagesExposeLocaleAndLanguageLinks(t *testing.T) {
	tests := []struct {
		path       string
		locale     string
		content    string
		otherRoute string
		breadcrumb string
	}{
		{path: "/pt-BR/", locale: "pt-BR", content: "Console de transporte", otherRoute: "/pt-BR/websocket"},
		{path: "/es-AR/websocket", locale: "es-AR", content: "Laboratorio WebSocket", otherRoute: "/es-AR/", breadcrumb: "Ruta de navegación"},
	}

	for _, test := range tests {
		t.Run(test.locale, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, test.path, nil)
			responseRecorder := httptest.NewRecorder()

			newHandler().ServeHTTP(responseRecorder, request)

			body := responseRecorder.Body.String()
			if responseRecorder.Code != http.StatusOK {
				t.Fatalf("status code = %d, want %d", responseRecorder.Code, http.StatusOK)
			}
			if responseRecorder.Header().Get("Content-Language") != test.locale {
				t.Fatalf("Content-Language = %q, want %q", responseRecorder.Header().Get("Content-Language"), test.locale)
			}
			for _, want := range []string{
				`<html lang="` + test.locale + `">`,
				test.content,
				`data-locale="` + test.locale + `"`,
				test.otherRoute,
			} {
				if !strings.Contains(body, want) {
					t.Fatalf("response does not contain %q", want)
				}
			}
			if test.breadcrumb != "" && !strings.Contains(body, `aria-label="`+test.breadcrumb+`"`) {
				t.Fatalf("localized breadcrumb label %q not found", test.breadcrumb)
			}
			if cookie := responseRecorder.Header().Get("Set-Cookie"); !strings.Contains(cookie, "testkit_locale="+test.locale) {
				t.Fatalf("Set-Cookie = %q, want locale cookie", cookie)
			}
		})
	}
}

func TestTranslationCatalogsHaveSameKeys(t *testing.T) {
	english := pageTranslationCatalog.values[localeEN]
	for _, currentLocale := range supportedLocales {
		translations := pageTranslationCatalog.values[currentLocale]
		if len(translations) != len(english) {
			t.Fatalf("locale %s has %d keys, English has %d", currentLocale, len(translations), len(english))
		}
		for key := range english {
			if _, ok := translations[key]; !ok {
				t.Fatalf("locale %s is missing key %q", currentLocale, key)
			}
		}
	}
}

func TestFrontendTestsAreNotPublicAssets(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/static/ws-client.test.mjs", nil)
	responseRecorder := httptest.NewRecorder()

	newHandler().ServeHTTP(responseRecorder, request)

	if responseRecorder.Code != http.StatusNotFound {
		t.Fatalf("frontend test asset status code = %d, want %d", responseRecorder.Code, http.StatusNotFound)
	}
}

func TestRESTEcho(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/echo", strings.NewReader(`{"hello":"world"}`))
	request.Header.Set("Content-Type", "application/json")
	responseRecorder := httptest.NewRecorder()

	newHandler().ServeHTTP(responseRecorder, request)

	if responseRecorder.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d: %s", responseRecorder.Code, http.StatusOK, responseRecorder.Body.String())
	}
	var payload struct {
		Data   map[string]string `json:"data"`
		Method string            `json:"method"`
		Path   string            `json:"path"`
	}
	if err := json.NewDecoder(responseRecorder.Body).Decode(&payload); err != nil {
		t.Fatalf("decode REST response: %v", err)
	}
	if payload.Data["hello"] != "world" || payload.Method != http.MethodPost || payload.Path != "/api/echo" {
		t.Fatalf("unexpected REST payload: %#v", payload)
	}
}

func TestRESTEchoRejectsInvalidAndOversizedJSON(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		statusCode int
	}{
		{name: "invalid", body: "{", statusCode: http.StatusBadRequest},
		{name: "oversized", body: `{"data":"` + strings.Repeat("a", maxJSONBodyBytes) + `"}`, statusCode: http.StatusRequestEntityTooLarge},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/api/echo", strings.NewReader(test.body))
			responseRecorder := httptest.NewRecorder()

			newHandler().ServeHTTP(responseRecorder, request)

			if responseRecorder.Code != test.statusCode {
				t.Fatalf("status code = %d, want %d", responseRecorder.Code, test.statusCode)
			}
		})
	}
}

func TestGraphQLTransportAndVariables(t *testing.T) {
	tests := []struct {
		name      string
		request   string
		wantData  map[string]interface{}
		wantError bool
	}{
		{
			name:     "fields",
			request:  `{"query":"{ status version echo(message: \"hello\") }"}`,
			wantData: map[string]interface{}{"status": "ok", "version": version, "echo": "hello"},
		},
		{
			name:     "variables",
			request:  `{"query":"query Echo($message: String!) { echo(message: $message) }","variables":{"message":"variable"}}`,
			wantData: map[string]interface{}{"echo": "variable"},
		},
		{
			name:      "invalid query",
			request:   `{"query":"{ unknown }"}`,
			wantError: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/graphql", strings.NewReader(test.request))
			request.Header.Set("Content-Type", "application/json")
			responseRecorder := httptest.NewRecorder()

			newHandler().ServeHTTP(responseRecorder, request)

			if responseRecorder.Code != http.StatusOK {
				t.Fatalf("status code = %d, want %d: %s", responseRecorder.Code, http.StatusOK, responseRecorder.Body.String())
			}
			var payload struct {
				Data   map[string]interface{} `json:"data"`
				Errors []interface{}          `json:"errors"`
			}
			if err := json.NewDecoder(responseRecorder.Body).Decode(&payload); err != nil {
				t.Fatalf("decode GraphQL response: %v", err)
			}
			if test.wantError {
				if len(payload.Errors) == 0 {
					t.Fatalf("expected GraphQL errors, got %#v", payload)
				}
				return
			}
			for key, want := range test.wantData {
				if payload.Data[key] != want {
					t.Fatalf("data[%q] = %#v, want %#v", key, payload.Data[key], want)
				}
			}
		})
	}
}

func TestGraphQLMethodAndBodyLimits(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/graphql", nil)
	responseRecorder := httptest.NewRecorder()
	newHandler().ServeHTTP(responseRecorder, request)
	if responseRecorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET /graphql status code = %d, want %d", responseRecorder.Code, http.StatusMethodNotAllowed)
	}

	request = httptest.NewRequest(http.MethodPost, "/graphql", strings.NewReader(`{"query":"`+strings.Repeat("a", maxGraphQLBodyBytes)+`"}`))
	responseRecorder = httptest.NewRecorder()
	newHandler().ServeHTTP(responseRecorder, request)
	if responseRecorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized GraphQL status code = %d, want %d", responseRecorder.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestSSETransportFlushesAndRemainsOpen(t *testing.T) {
	server := httptest.NewServer(newHandlerWithConfig(handlerConfig{sseInterval: 10 * time.Millisecond}))
	t.Cleanup(server.Close)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/events", nil)
	if err != nil {
		t.Fatalf("create SSE request: %v", err)
	}
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatalf("open SSE stream: %v", err)
	}
	t.Cleanup(func() { _ = response.Body.Close() })
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status code = %d, want %d", response.StatusCode, http.StatusOK)
	}
	if got := response.Header.Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", got)
	}

	lines := make(chan string, 8)
	go func() {
		scanner := bufio.NewScanner(response.Body)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
		close(lines)
	}()

	for _, want := range []string{"id: 1", "event: status"} {
		select {
		case line := <-lines:
			if line != want {
				t.Fatalf("SSE line = %q, want %q", line, want)
			}
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for SSE line %q", want)
		}
	}
	select {
	case line := <-lines:
		if line != "data: {\"status\":\"ok\",\"version\":\""+version+"\"}" {
			t.Fatalf("SSE data line = %q", line)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for SSE data")
	}

	select {
	case <-lines:
	case <-time.After(time.Second):
		t.Fatal("SSE stream did not deliver an incremental event")
	}
}

func TestSSEConnectionLogsLifecycle(t *testing.T) {
	logger, output := newTestLogger()
	server := httptest.NewServer(newHandlerWithConfig(handlerConfig{
		logger:      logger,
		sseInterval: time.Millisecond,
	}))
	t.Cleanup(server.Close)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/events?correlation_id="+suppliedCorrelationID, nil)
	if err != nil {
		t.Fatalf("create SSE request: %v", err)
	}
	responseValue, err := server.Client().Do(request)
	if err != nil {
		t.Fatalf("open SSE stream: %v", err)
	}
	t.Cleanup(func() { _ = responseValue.Body.Close() })

	records := waitForLogEvent(t, output, "connection.opened", 1)
	opened := records[len(records)-1]
	if opened["protocol"] != "sse" || opened["protocol_active"] != float64(1) || opened["connection_sequence"] != float64(1) || opened["correlation_id"] != suppliedCorrelationID {
		t.Fatalf("opened SSE log = %#v", opened)
	}
	cancel()
	_ = responseValue.Body.Close()
	records = waitForLogEvent(t, output, "connection.closed", 1)
	closed := records[len(records)-1]
	if closed["protocol"] != "sse" || closed["protocol_active"] != float64(0) || closed["connection_sequence"] != float64(2) || closed["correlation_id"] != suppliedCorrelationID {
		t.Fatalf("closed SSE log = %#v", closed)
	}
	if duration, ok := closed["duration_ms"].(float64); !ok || duration < 0 {
		t.Fatalf("SSE duration_ms = %#v, want a non-negative number", closed["duration_ms"])
	}
}

func TestConnectionFactsCombineWebSocketAndSSECounts(t *testing.T) {
	logger, output := newTestLogger()
	app := newApplication(handlerConfig{logger: logger, sseInterval: time.Millisecond})
	server := httptest.NewServer(app.handler)
	t.Cleanup(server.Close)

	sseCtx, cancelSSE := context.WithCancel(context.Background())
	t.Cleanup(cancelSSE)
	sseRequest, err := http.NewRequestWithContext(sseCtx, http.MethodGet, server.URL+"/events", nil)
	if err != nil {
		t.Fatalf("create SSE request: %v", err)
	}
	sseResponse, err := server.Client().Do(sseRequest)
	if err != nil {
		t.Fatalf("open SSE stream: %v", err)
	}
	t.Cleanup(func() { _ = sseResponse.Body.Close() })
	waitForLogEvent(t, output, "connection.opened", 1)

	webSocketConnection, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/ws", nil)
	if err != nil {
		cancelSSE()
		_ = sseResponse.Body.Close()
		t.Fatalf("dial WebSocket: %v", err)
	}
	t.Cleanup(func() { _ = webSocketConnection.Close() })
	opened := recordsForEvent(waitForLogEvent(t, output, "connection.opened", 2), "connection.opened")
	if opened[1]["protocol"] != "websocket" || opened[1]["protocol_active"] != float64(1) || opened[1]["total_active"] != float64(2) || opened[1]["connection_sequence"] != float64(2) {
		t.Fatalf("combined open log = %#v, want first WebSocket and two total connections", opened[1])
	}
	sseCorrelationID, sseOK := opened[0]["correlation_id"].(string)
	webSocketCorrelationID, webSocketOK := opened[1]["correlation_id"].(string)
	if !sseOK || !webSocketOK {
		t.Fatalf("connection facts lack correlation IDs: %#v", opened)
	}
	requireUUIDv7(t, sseCorrelationID)
	requireUUIDv7(t, webSocketCorrelationID)
	if sseCorrelationID == webSocketCorrelationID {
		t.Fatalf("SSE and WebSocket share correlation ID %q", sseCorrelationID)
	}

	cancelSSE()
	_ = sseResponse.Body.Close()
	closed := recordsForEvent(waitForLogEvent(t, output, "connection.closed", 1), "connection.closed")
	if closed[0]["protocol"] != "sse" || closed[0]["protocol_active"] != float64(0) || closed[0]["total_active"] != float64(1) || closed[0]["connection_sequence"] != float64(3) {
		t.Fatalf("SSE close log = %#v, want one WebSocket remaining", closed[0])
	}
	if err := webSocketConnection.Close(); err != nil {
		t.Fatalf("close WebSocket: %v", err)
	}
	closed = recordsForEvent(waitForLogEvent(t, output, "connection.closed", 2), "connection.closed")
	if closed[1]["protocol"] != "websocket" || closed[1]["protocol_active"] != float64(0) || closed[1]["total_active"] != float64(0) || closed[1]["connection_sequence"] != float64(4) {
		t.Fatalf("WebSocket close log = %#v, want no active connections", closed[1])
	}
}

func TestWebSocketEchoAndBroadcast(t *testing.T) {
	server := httptest.NewServer(newHandler())
	t.Cleanup(server.Close)
	websocketURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"

	first, _, err := websocket.DefaultDialer.Dial(websocketURL, nil)
	if err != nil {
		t.Fatalf("dial first WebSocket: %v", err)
	}
	t.Cleanup(func() { _ = first.Close() })
	second, _, err := websocket.DefaultDialer.Dial(websocketURL, nil)
	if err != nil {
		t.Fatalf("dial second WebSocket: %v", err)
	}
	t.Cleanup(func() { _ = second.Close() })

	if err := first.WriteJSON(map[string]string{"message": "hello"}); err != nil {
		t.Fatalf("write WebSocket message: %v", err)
	}
	for name, connection := range map[string]*websocket.Conn{"first": first, "second": second} {
		t.Run(name, func(t *testing.T) {
			_ = connection.SetReadDeadline(time.Now().Add(time.Second))
			var message wsMessage
			if err := connection.ReadJSON(&message); err != nil {
				t.Fatalf("read WebSocket message: %v", err)
			}
			if message.Message != "hello" || message.Version != version {
				t.Fatalf("unexpected WebSocket message: %#v", message)
			}
		})
	}
}

func TestWebSocketConnectionLogsLifecycle(t *testing.T) {
	logger, output := newTestLogger()
	server := httptest.NewServer(newHandlerWithConfig(handlerConfig{logger: logger}))
	t.Cleanup(server.Close)
	connection, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/ws?correlation_id="+suppliedCorrelationID, nil)
	if err != nil {
		t.Fatalf("dial WebSocket: %v", err)
	}

	records := waitForLogEvent(t, output, "connection.opened", 1)
	opened := records[len(records)-1]
	if opened["protocol"] != "websocket" || opened["protocol_active"] != float64(1) || opened["connection_sequence"] != float64(1) || opened["correlation_id"] != suppliedCorrelationID {
		t.Fatalf("opened WebSocket log = %#v", opened)
	}
	if err := connection.Close(); err != nil {
		t.Fatalf("close WebSocket: %v", err)
	}
	records = waitForLogEvent(t, output, "connection.closed", 1)
	closed := records[len(records)-1]
	if closed["protocol"] != "websocket" || closed["protocol_active"] != float64(0) || closed["connection_sequence"] != float64(2) || closed["correlation_id"] != suppliedCorrelationID {
		t.Fatalf("closed WebSocket log = %#v", closed)
	}
	if duration, ok := closed["duration_ms"].(float64); !ok || duration < 0 {
		t.Fatalf("WebSocket duration_ms = %#v, want a non-negative number", closed["duration_ms"])
	}
}

func TestDecodeJSONRejectsTrailingValues(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/echo", strings.NewReader(`{"first":1}{"second":2}`))
	responseRecorder := httptest.NewRecorder()

	newHandler().ServeHTTP(responseRecorder, request)

	if responseRecorder.Code != http.StatusBadRequest {
		t.Fatalf("status code = %d, want %d", responseRecorder.Code, http.StatusBadRequest)
	}
}

func TestSSEIntervalFromEnv(t *testing.T) {
	t.Setenv("SSE_INTERVAL", "25ms")
	if got := sseIntervalFromEnv(); got != 25*time.Millisecond {
		t.Fatalf("SSE interval = %s, want 25ms", got)
	}
	t.Setenv("SSE_INTERVAL", "invalid")
	if got := sseIntervalFromEnv(); got != defaultSSEInterval {
		t.Fatalf("invalid SSE interval = %s, want %s", got, defaultSSEInterval)
	}
}

func TestHTTPListenAddressFromEnv(t *testing.T) {
	for _, test := range []struct {
		name    string
		value   string
		want    string
		wantErr bool
	}{
		{name: "empty", value: "", want: ":8080"},
		{name: "custom port", value: "2020", want: ":2020"},
		{name: "trimmed custom port", value: " 2020 ", want: ":2020"},
		{name: "minimum port", value: "1", want: ":1"},
		{name: "maximum port", value: "65535", want: ":65535"},
		{name: "not a number", value: "invalid", wantErr: true},
		{name: "zero", value: "0", wantErr: true},
		{name: "negative", value: "-1", wantErr: true},
		{name: "above maximum", value: "65536", wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("HTTP_PORT", test.value)

			got, err := httpListenAddressFromEnv()
			if test.wantErr {
				if err == nil {
					t.Fatalf("http listen address = %q, want an error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("httpListenAddressFromEnv() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("http listen address = %q, want %q", got, test.want)
			}
		})
	}
}

func TestGraphQLRejectsInvalidJSON(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/graphql", strings.NewReader("{"))
	responseRecorder := httptest.NewRecorder()

	newHandler().ServeHTTP(responseRecorder, request)

	if responseRecorder.Code != http.StatusBadRequest {
		t.Fatalf("status code = %d, want %d", responseRecorder.Code, http.StatusBadRequest)
	}
}

func TestStaticPathTraversalIsNotServed(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/static/../go.mod", nil)
	responseRecorder := httptest.NewRecorder()

	newHandler().ServeHTTP(responseRecorder, request)

	if responseRecorder.Code != http.StatusTemporaryRedirect {
		t.Fatalf("status code = %d, want %d", responseRecorder.Code, http.StatusTemporaryRedirect)
	}
	if got := responseRecorder.Header().Get("Location"); got != "/go.mod" {
		t.Fatalf("Location = %q, want /go.mod", got)
	}
	body, err := io.ReadAll(responseRecorder.Result().Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if strings.Contains(string(body), "module ") {
		t.Fatal("path traversal exposed repository content")
	}
}
