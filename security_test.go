package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
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
