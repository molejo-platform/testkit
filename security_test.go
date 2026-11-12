package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestPublicHandlerDoesNotExposeOutboundProbe(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/outbound?url=http://127.0.0.1", nil)
	responseRecorder := httptest.NewRecorder()

	newHandler().ServeHTTP(responseRecorder, request)

	if responseRecorder.Code != http.StatusNotFound {
		t.Fatalf("status code = %d, want %d", responseRecorder.Code, http.StatusNotFound)
	}
}

func TestHandlerAppliesBrowserSecurityHeaders(t *testing.T) {
	response := httptest.NewRecorder()
	newHandler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/en/", nil))
	for header, expected := range map[string]string{
		"Content-Security-Policy": "frame-ancestors 'none'",
		"Referrer-Policy":         "no-referrer",
		"X-Content-Type-Options":  "nosniff",
		"X-Frame-Options":         "DENY",
	} {
		if !strings.Contains(response.Header().Get(header), expected) {
			t.Errorf("%s = %q", header, response.Header().Get(header))
		}
	}
}

func TestRunProbeRequestsControlledHTTPDestination(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if got := request.Header.Get("User-Agent"); got != "molejo-testkit/"+version {
			t.Errorf("User-Agent = %q, want %q", got, "molejo-testkit/"+version)
		}
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte("controlled-upstream"))
	}))
	t.Cleanup(upstream.Close)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runProbe(context.Background(), []string{upstream.URL}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exit code = %d, want 0: %s", exitCode, stderr.String())
	}

	var payload response
	if err := json.NewDecoder(&stdout).Decode(&payload); err != nil {
		t.Fatalf("decode probe output: %v", err)
	}
	if payload.UpstreamStatus != http.StatusOK || payload.UpstreamBody != "controlled-upstream" {
		t.Fatalf("unexpected probe output: %#v", payload)
	}
	assertProbeLog(t, &stderr, "success", "ok", "INFO", http.StatusOK)
	if strings.Contains(stderr.String(), "controlled-upstream") || strings.Contains(stderr.String(), upstream.URL) {
		t.Fatal("upstream data leaked into probe logs")
	}
}

func TestRunProbeRejectsInvalidArguments(t *testing.T) {
	for _, arguments := range [][]string{nil, {"relative"}, {"ftp://example.com"}, {"https://example.com", "extra"}} {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		if exitCode := runProbe(context.Background(), arguments, &stdout, &stderr); exitCode != 2 {
			t.Fatalf("runProbe(%q) exit code = %d, want 2", arguments, exitCode)
		}
	}
}

func TestRunProbeReturnsFailureForUnhealthyDestination(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		http.Error(writer, "unavailable", http.StatusServiceUnavailable)
	}))
	t.Cleanup(upstream.Close)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if exitCode := runProbe(context.Background(), []string{upstream.URL}, &stdout, &stderr); exitCode != 1 {
		t.Fatalf("exit code = %d, want 1", exitCode)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty output", stdout.String())
	}
	assertProbeLog(t, &stderr, "failed", "upstream_status", "WARN", http.StatusServiceUnavailable)
}

func TestRunProbeLogsTimeoutWithoutURL(t *testing.T) {
	ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer cancel()
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("expired probe reached upstream")
	}))
	t.Cleanup(upstream.Close)

	var stdout, stderr bytes.Buffer
	if exitCode := runProbe(ctx, []string{upstream.URL + "/?token=private-sentinel"}, &stdout, &stderr); exitCode != 1 || stdout.Len() != 0 {
		t.Fatalf("exit code = %d, stdout = %q", exitCode, stdout.String())
	}
	assertProbeLog(t, &stderr, "failed", "deadline_exceeded", "WARN", 0)
	if strings.Contains(stderr.String(), "private-sentinel") || strings.Contains(stderr.String(), upstream.URL) {
		t.Fatal("URL leaked into probe logs")
	}
}

func assertProbeLog(t *testing.T, stderr *bytes.Buffer, status, code, level string, upstreamStatus int) {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(stderr.String()), "\n")
	var record map[string]any
	if len(lines) != 1 || json.Unmarshal([]byte(lines[0]), &record) != nil {
		t.Fatalf("expected a single JSON probe event, got %q", stderr.String())
	}
	id, _ := record["correlation_id"].(string)
	duration, hasDuration := record["duration_ms"].(float64)
	if record["event"] != "probe.completed" || record["status"] != status || record["code"] != code ||
		record["level"] != level || record["service"] != "molejo-testkit" || record["version"] != version ||
		!isUUIDv7(id) || !hasDuration || duration < 0 {
		t.Fatalf("unexpected probe log: %#v", record)
	}
	if got := record["upstream_status_code"]; (upstreamStatus == 0 && got != nil) ||
		(upstreamStatus != 0 && got != float64(upstreamStatus)) {
		t.Fatalf("upstream status code = %#v, want %d when present", got, upstreamStatus)
	}
}

func TestWebSocketAllowsSameOrigin(t *testing.T) {
	server := httptest.NewServer(newHandler())
	t.Cleanup(server.Close)
	header := http.Header{"Origin": []string{server.URL}}

	connection, _, err := websocket.DefaultDialer.Dial(
		"ws"+server.URL[len("http"):]+"/ws",
		header,
	)
	if err != nil {
		t.Fatalf("dial same-origin WebSocket: %v", err)
	}
	_ = connection.Close()
}

func TestWebSocketRejectsCrossOrigin(t *testing.T) {
	server := httptest.NewServer(newHandler())
	t.Cleanup(server.Close)
	header := http.Header{"Origin": []string{"https://attacker.example"}}

	connection, response, err := websocket.DefaultDialer.Dial(
		"ws"+server.URL[len("http"):]+"/ws",
		header,
	)
	if connection != nil {
		_ = connection.Close()
	}
	if err == nil {
		t.Fatal("cross-origin WebSocket connection unexpectedly succeeded")
	}
	if response == nil || response.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %v, want %d", response, http.StatusForbidden)
	}
}

func TestServeStopsOnContextCancellation(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	application := newApplication(handlerConfig{sseInterval: time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- serve(ctx, listener, application)
	}()

	client := &http.Client{Timeout: time.Second}
	var responseValue *http.Response
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); {
		responseValue, err = client.Get("http://" + listener.Addr().String() + "/readyz")
		if err == nil {
			_ = responseValue.Body.Close()
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		cancel()
		t.Fatalf("server did not become ready: %v", err)
	}
	webSocketConnection, _, err := websocket.DefaultDialer.Dial(
		"ws://"+listener.Addr().String()+"/ws",
		nil,
	)
	if err != nil {
		cancel()
		t.Fatalf("dial WebSocket: %v", err)
	}
	t.Cleanup(func() { _ = webSocketConnection.Close() })

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve returned an error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("serve did not stop after context cancellation")
	}
	_ = webSocketConnection.SetReadDeadline(time.Now().Add(time.Second))
	if _, _, err := webSocketConnection.ReadMessage(); err == nil {
		t.Fatal("WebSocket remained open after server shutdown")
	}
}
