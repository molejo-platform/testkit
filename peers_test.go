package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestParsePeerSet(t *testing.T) {
	valid := `{
		"schema_version": 1,
		"instance_id": "testkit-a",
		"check_interval": "30s",
		"timeout": "3s",
		"peers": [{
			"name": "testkit-b",
			"scheme": "http",
			"host": "testkit-b.namespace-b",
			"port": 8080,
			"expected_instance_id": "testkit-b"
		}]
	}`

	configuration, err := parsePeerSet(strings.NewReader(valid))
	if err != nil {
		t.Fatalf("parse valid peer set: %v", err)
	}
	if configuration.InstanceID != "testkit-a" || configuration.CheckInterval != 30*time.Second || configuration.Timeout != 3*time.Second {
		t.Fatalf("unexpected configuration: %#v", configuration)
	}
	if len(configuration.Peers) != 1 || configuration.Peers[0].Host != "testkit-b.namespace-b" {
		t.Fatalf("unexpected peers: %#v", configuration.Peers)
	}

	for _, test := range []struct {
		name    string
		replace string
		with    string
	}{
		{name: "unknown schema", replace: `"schema_version": 1`, with: `"schema_version": 2`},
		{name: "unknown field", replace: `"schema_version": 1`, with: `"schema_version": 1, "url": "http://attacker.example"`},
		{name: "missing instance identity", replace: `"instance_id": "testkit-a"`, with: `"instance_id": ""`},
		{name: "invalid interval", replace: `"check_interval": "30s"`, with: `"check_interval": "0s"`},
		{name: "invalid timeout", replace: `"timeout": "3s"`, with: `"timeout": "31s"`},
		{name: "unsupported scheme", replace: `"scheme": "http"`, with: `"scheme": "ftp"`},
		{name: "host contains path", replace: `"host": "testkit-b.namespace-b"`, with: `"host": "testkit-b/path"`},
		{name: "invalid port", replace: `"port": 8080`, with: `"port": 0`},
		{name: "missing expected identity", replace: `"expected_instance_id": "testkit-b"`, with: `"expected_instance_id": ""`},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := parsePeerSet(strings.NewReader(strings.Replace(valid, test.replace, test.with, 1)))
			if err == nil {
				t.Fatal("parsePeerSet unexpectedly accepted invalid configuration")
			}
		})
	}
}

func TestParsePeerSetRejectsDuplicateAndExcessPeers(t *testing.T) {
	peer := `{"name":"peer-%d","scheme":"http","host":"peer-%d","port":8080,"expected_instance_id":"peer-%d"}`
	peers := make([]string, maxConfiguredPeers+1)
	for index := range peers {
		peers[index] = fmt.Sprintf(peer, index, index, index)
	}
	payload := fmt.Sprintf(`{"schema_version":1,"instance_id":"source","check_interval":"30s","timeout":"3s","peers":[%s]}`, strings.Join(peers, ","))
	if _, err := parsePeerSet(strings.NewReader(payload)); err == nil {
		t.Fatal("parsePeerSet unexpectedly accepted too many peers")
	}

	duplicate := `{"schema_version":1,"instance_id":"source","check_interval":"30s","timeout":"3s","peers":[{"name":"peer","scheme":"http","host":"one","port":8080,"expected_instance_id":"peer"},{"name":"peer","scheme":"http","host":"two","port":8080,"expected_instance_id":"peer"}]}`
	if _, err := parsePeerSet(strings.NewReader(duplicate)); err == nil {
		t.Fatal("parsePeerSet unexpectedly accepted duplicate peer names")
	}
}

func TestParsePeerSetBoundsTimeout(t *testing.T) {
	payload := func(timeout string) string {
		return fmt.Sprintf(`{
			"schema_version": 1,
			"instance_id": "source",
			"check_interval": "1m",
			"timeout": %q,
			"peers": []
		}`, timeout)
	}
	if _, err := parsePeerSet(strings.NewReader(payload(maxPeerCheckTimeout.String()))); err != nil {
		t.Fatalf("parsePeerSet rejected maximum peer timeout: %v", err)
	}
	if _, err := parsePeerSet(strings.NewReader(payload((maxPeerCheckTimeout + time.Second).String()))); err == nil {
		t.Fatalf("parsePeerSet accepted a timeout above %s", maxPeerCheckTimeout)
	}
}

func TestPeerIdentityAndStateEndpoints(t *testing.T) {
	configuration := peerSet{
		SchemaVersion: 1,
		InstanceID:    "testkit-a",
		CheckInterval: time.Minute,
		Timeout:       time.Second,
		Peers:         []peer{{Name: "testkit-b", Scheme: "http", Host: "testkit-b", Port: 8080, ExpectedInstanceID: "testkit-b"}},
	}
	monitor := newPeerMonitor(configuration, "019047de-1234-7abc-8def-0123456789ab", slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))
	handler := newApplication(handlerConfig{sseInterval: time.Millisecond, peers: monitor}).handler

	identityRecorder := httptest.NewRecorder()
	handler.ServeHTTP(identityRecorder, httptest.NewRequest(http.MethodGet, "/api/identity", nil))
	if identityRecorder.Code != http.StatusOK {
		t.Fatalf("identity status = %d, want %d", identityRecorder.Code, http.StatusOK)
	}
	if got := identityRecorder.Header().Get(peerCheckHeader); !isUUIDv7(got) {
		t.Fatalf("identity check ID = %q, want UUIDv7", got)
	}
	var identity peerIdentity
	if err := json.NewDecoder(identityRecorder.Body).Decode(&identity); err != nil {
		t.Fatalf("decode identity: %v", err)
	}
	if identity.ProtocolVersion != peerProtocolVersion || identity.InstanceID != "testkit-a" || identity.BootID != monitor.bootID {
		t.Fatalf("unexpected identity: %#v", identity)
	}

	var attackerRequests atomic.Int32
	attacker := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		attackerRequests.Add(1)
	}))
	t.Cleanup(attacker.Close)
	stateRecorder := httptest.NewRecorder()
	handler.ServeHTTP(stateRecorder, httptest.NewRequest(http.MethodGet, "/api/peers?url="+url.QueryEscape(attacker.URL), nil))
	if stateRecorder.Code != http.StatusOK {
		t.Fatalf("peers status = %d, want %d", stateRecorder.Code, http.StatusOK)
	}
	if strings.Contains(stateRecorder.Body.String(), "attacker") || strings.Contains(stateRecorder.Body.String(), "testkit-b:8080") {
		t.Fatalf("peer state leaked a URL or accepted an override: %s", stateRecorder.Body.String())
	}
	if attackerRequests.Load() != 0 {
		t.Fatal("peer state endpoint triggered a caller-provided destination")
	}
}

func TestPeerOperationalEndpointsDisableCaching(t *testing.T) {
	configuration := peerSet{
		SchemaVersion: 1,
		InstanceID:    "testkit-a",
		CheckInterval: time.Minute,
		Timeout:       time.Second,
	}
	monitor := newPeerMonitor(configuration, newUUIDv7(), slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))
	handler := newApplication(handlerConfig{peers: monitor}).handler
	missing := make([]string, 0, 2)
	for _, path := range []string{peerIdentityPath, "/api/peers"} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if !strings.Contains(strings.ToLower(recorder.Header().Get("Cache-Control")), "no-store") {
			missing = append(missing, path)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("operational endpoints without Cache-Control no-store: %v", missing)
	}
}

func TestPeerEndpointsAreDisabledWithoutConfiguration(t *testing.T) {
	for _, path := range []string{"/api/identity", "/api/peers"} {
		recorder := httptest.NewRecorder()
		newHandler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusNotFound {
			t.Fatalf("%s status = %d, want %d", path, recorder.Code, http.StatusNotFound)
		}
	}
}

func TestCheckPeerUsesFreshDirectConnections(t *testing.T) {
	var connections atomic.Int32
	targetMonitor := newPeerMonitor(
		peerSet{SchemaVersion: 1, InstanceID: "testkit-b", CheckInterval: time.Minute, Timeout: time.Second},
		newUUIDv7(),
		slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
	)
	server := httptest.NewUnstartedServer(newApplication(handlerConfig{peers: targetMonitor}).handler)
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			connections.Add(1)
		}
	}
	server.Start()
	t.Cleanup(server.Close)

	address := strings.TrimPrefix(server.URL, "http://")
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		t.Fatalf("split server address: %v", err)
	}
	target := peer{Name: "testkit-b", Scheme: "http", Host: host, Port: mustPort(t, port), ExpectedInstanceID: "testkit-b"}
	for range 2 {
		result := checkPeer(context.Background(), target, time.Second)
		if result.Outcome != peerReachable || result.Reason != peerReasonOK {
			t.Fatalf("unexpected check result: %#v", result)
		}
	}
	if got := connections.Load(); got != 2 {
		t.Fatalf("new connection count = %d, want 2", got)
	}
}

func TestCheckPeerDoesNotFollowRedirects(t *testing.T) {
	var redirected atomic.Bool
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		redirected.Store(true)
	}))
	t.Cleanup(destination.Close)
	source := httptest.NewServer(http.RedirectHandler(destination.URL, http.StatusFound))
	t.Cleanup(source.Close)

	host, port, err := net.SplitHostPort(strings.TrimPrefix(source.URL, "http://"))
	if err != nil {
		t.Fatalf("split server address: %v", err)
	}
	result := checkPeer(context.Background(), peer{Name: "peer", Scheme: "http", Host: host, Port: mustPort(t, port), ExpectedInstanceID: "peer"}, time.Second)
	if result.Outcome != peerUnknown || result.Reason != peerReasonHTTPFailed {
		t.Fatalf("unexpected redirect result: %#v", result)
	}
	if redirected.Load() {
		t.Fatal("peer check followed a redirect")
	}
}

func TestCheckPeerClassifiesIdentityMismatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writeJSON(writer, http.StatusOK, peerIdentity{ProtocolVersion: peerProtocolVersion, InstanceID: "unexpected", BootID: newUUIDv7(), Version: version})
	}))
	t.Cleanup(server.Close)
	host, port, err := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatalf("split server address: %v", err)
	}
	result := checkPeer(context.Background(), peer{Name: "peer", Scheme: "http", Host: host, Port: mustPort(t, port), ExpectedInstanceID: "expected"}, time.Second)
	if result.Outcome != peerUnknown || result.Reason != peerReasonIdentityMismatch || result.ObservedInstanceID != "unexpected" {
		t.Fatalf("unexpected mismatch result: %#v", result)
	}
}

func TestCheckPeerIgnoresProxyEnvironment(t *testing.T) {
	var proxyRequests atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		proxyRequests.Add(1)
		writeJSON(writer, http.StatusOK, peerIdentity{ProtocolVersion: peerProtocolVersion, InstanceID: "target", BootID: newUUIDv7(), Version: version})
	}))
	t.Cleanup(proxy.Close)
	t.Setenv("HTTP_PROXY", proxy.URL)
	t.Setenv("HTTPS_PROXY", proxy.URL)
	t.Setenv("NO_PROXY", "")

	result := checkPeer(context.Background(), peer{Name: "target", Scheme: "http", Host: "does-not-exist.invalid", Port: 8080, ExpectedInstanceID: "target"}, 100*time.Millisecond)
	if result.Reason != peerReasonDNSFailed {
		t.Fatalf("peer check reason = %q, want %q", result.Reason, peerReasonDNSFailed)
	}
	if proxyRequests.Load() != 0 {
		t.Fatal("peer check used the proxy configured in the environment")
	}
}

func TestCheckPeerUsesNeutralReasonWhenServerAcceptsButDoesNotRespond(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		<-request.Context().Done()
	}))
	t.Cleanup(server.Close)
	host, port, err := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	result := checkPeer(context.Background(), peer{Name: "target", Scheme: "http", Host: host, Port: mustPort(t, port), ExpectedInstanceID: "target"}, 50*time.Millisecond)
	if result.Reason != peerReasonRequestTimeout {
		t.Fatalf("reason = %q", result.Reason)
	}
}

func TestCheckPeerRejectsInvalidTLSAndOversizedIdentity(t *testing.T) {
	t.Run("invalid TLS certificate", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			writeJSON(writer, http.StatusOK, peerIdentity{ProtocolVersion: peerProtocolVersion, InstanceID: "target", BootID: newUUIDv7(), Version: version})
		}))
		t.Cleanup(server.Close)
		host, port, err := net.SplitHostPort(strings.TrimPrefix(server.URL, "https://"))
		if err != nil {
			t.Fatalf("split TLS server address: %v", err)
		}
		result := checkPeer(context.Background(), peer{Name: "target", Scheme: "https", Host: host, Port: mustPort(t, port), ExpectedInstanceID: "target"}, time.Second)
		if result.Outcome != peerUnknown || result.Reason != peerReasonTLSFailed {
			t.Fatalf("unexpected TLS result: %#v", result)
		}
	})

	t.Run("oversized identity", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			_, _ = writer.Write(bytes.Repeat([]byte("x"), maxPeerIdentityBytes+1))
		}))
		t.Cleanup(server.Close)
		host, port, err := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
		if err != nil {
			t.Fatalf("split server address: %v", err)
		}
		result := checkPeer(context.Background(), peer{Name: "target", Scheme: "http", Host: host, Port: mustPort(t, port), ExpectedInstanceID: "target"}, time.Second)
		if result.Outcome != peerUnknown || result.Reason != peerReasonInvalidResponse {
			t.Fatalf("unexpected oversized response result: %#v", result)
		}
	})
}

func TestPeerMonitorLogsOnlyStateChangesAndSanitizesFailures(t *testing.T) {
	logger, output := newTestLogger()
	configuration := peerSet{SchemaVersion: 1, InstanceID: "source", CheckInterval: time.Minute, Timeout: time.Second, Peers: []peer{{Name: "target", Scheme: "http", Host: "secret.internal", Port: 8080, ExpectedInstanceID: "target"}}}
	monitor := newPeerMonitor(configuration, newUUIDv7(), logger)
	var mutex sync.Mutex
	results := []peerCheckResult{
		{Outcome: peerUnreachable, Reason: peerReasonRequestTimeout, Detail: "dial secret.internal:8080: token=sensitive"},
		{Outcome: peerUnreachable, Reason: peerReasonRequestTimeout, Detail: "different raw error"},
		{Outcome: peerReachable, Reason: peerReasonOK, ObservedInstanceID: "target"},
	}
	monitor.check = func(context.Context, peer, time.Duration) peerCheckResult {
		mutex.Lock()
		defer mutex.Unlock()
		result := results[0]
		results = results[1:]
		return result
	}

	monitor.checkAll(context.Background())
	monitor.checkAll(context.Background())
	monitor.checkAll(context.Background())
	records := recordsForEvent(logRecords(t, output), "peer.state.changed")
	if len(records) != 2 {
		t.Fatalf("state change log count = %d, want 2: %s", len(records), output.String())
	}
	if strings.Contains(output.String(), "secret.internal") || strings.Contains(output.String(), "sensitive") || strings.Contains(output.String(), "raw error") {
		t.Fatalf("logs leaked a target or raw error: %s", output.String())
	}
	state := monitor.snapshot()
	if len(state) != 1 || state[0].Outcome != peerReachable || state[0].Detail != "" {
		t.Fatalf("unexpected public state: %#v", state)
	}
}

func TestPeerMonitorStopsOnCancellation(t *testing.T) {
	configuration := peerSet{SchemaVersion: 1, InstanceID: "source", CheckInterval: time.Hour, Timeout: time.Second}
	monitor := newPeerMonitor(configuration, newUUIDv7(), slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		monitor.run(ctx)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("peer monitor did not stop after cancellation")
	}
}

func TestPeerMonitorShutdownCancellationKeepsLastObservedState(t *testing.T) {
	logger, output := newTestLogger()
	configuration := peerSet{
		SchemaVersion: 1,
		InstanceID:    "source",
		CheckInterval: time.Hour,
		Timeout:       time.Second,
		Peers:         []peer{{Name: "target", Scheme: "http", Host: "target", Port: 8080, ExpectedInstanceID: "target"}},
	}
	monitor := newPeerMonitor(configuration, newUUIDv7(), logger)
	monitor.states["target"] = peerCheckResult{Peer: "target", Outcome: peerReachable, Reason: peerReasonOK, CheckedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	started := make(chan struct{})
	monitor.check = func(ctx context.Context, _ peer, _ time.Duration) peerCheckResult {
		close(started)
		<-ctx.Done()
		return peerCheckResult{Outcome: peerUnknown, Reason: peerReasonCanceled, CheckID: newUUIDv7()}
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		monitor.checkAll(ctx)
		close(done)
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("peer check did not start")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("peer check did not stop after cancellation")
	}

	state := monitor.snapshot()
	if len(state) != 1 || state[0].Outcome != peerReachable || state[0].Reason != peerReasonOK {
		t.Errorf("state after shutdown cancellation = %#v, want last reachable state", state)
	}
	if records := recordsForEvent(logRecords(t, output), "peer.state.changed"); len(records) != 0 {
		t.Errorf("shutdown cancellation emitted %d peer state changes, want 0: %s", len(records), output.String())
	}
}

func TestPeerMonitorReportsRemoteBootEpochChanges(t *testing.T) {
	firstBootID := newUUIDv7()
	targetMonitor := newPeerMonitor(
		peerSet{SchemaVersion: 1, InstanceID: "target", CheckInterval: time.Minute, Timeout: time.Second},
		firstBootID,
		slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
	)
	targetServer := httptest.NewServer(newApplication(handlerConfig{peers: targetMonitor}).handler)
	t.Cleanup(targetServer.Close)
	host, port, err := net.SplitHostPort(strings.TrimPrefix(targetServer.URL, "http://"))
	if err != nil {
		t.Fatalf("split target server address: %v", err)
	}
	logger, output := newTestLogger()
	sourceMonitor := newPeerMonitor(peerSet{
		SchemaVersion: 1,
		InstanceID:    "source",
		CheckInterval: time.Minute,
		Timeout:       time.Second,
		Peers:         []peer{{Name: "target", Scheme: "http", Host: host, Port: mustPort(t, port), ExpectedInstanceID: "target"}},
	}, newUUIDv7(), logger)

	sourceMonitor.checkAll(context.Background())
	secondBootID := newUUIDv7()
	targetMonitor.bootID = secondBootID
	sourceMonitor.checkAll(context.Background())

	state := sourceMonitor.snapshot()
	if len(state) != 1 || state[0].Outcome != peerReachable {
		t.Fatalf("unexpected peer state: %#v", state)
	}
	encoded, err := json.Marshal(state[0])
	if err != nil {
		t.Fatalf("encode peer state: %v", err)
	}
	var publicState map[string]interface{}
	if err := json.Unmarshal(encoded, &publicState); err != nil {
		t.Fatalf("decode peer state: %v", err)
	}
	if got, ok := publicState["observed_boot_id"].(string); !ok || got != secondBootID {
		t.Errorf("observed_boot_id = %#v, want %q", publicState["observed_boot_id"], secondBootID)
	}
	if records := recordsForEvent(logRecords(t, output), "peer.state.changed"); len(records) != 2 {
		t.Errorf("state change log count after remote restart = %d, want 2: %s", len(records), output.String())
	} else {
		if got := records[1]["previous_boot_id"]; got != firstBootID {
			t.Errorf("previous_boot_id = %#v, want %q", got, firstBootID)
		}
		if got := records[1]["observed_boot_id"]; got != secondBootID {
			t.Errorf("observed_boot_id log value = %#v, want %q", got, secondBootID)
		}
	}
}

func TestPeerMonitorChecksImmediatelyAndBoundsConcurrency(t *testing.T) {
	peers := make([]peer, maxConcurrentPeerChecks+2)
	for index := range peers {
		peers[index] = peer{Name: fmt.Sprintf("peer-%d", index), Scheme: "http", Host: fmt.Sprintf("peer-%d", index), Port: 8080, ExpectedInstanceID: fmt.Sprintf("peer-%d", index)}
	}
	configuration := peerSet{SchemaVersion: 1, InstanceID: "source", CheckInterval: time.Hour, Timeout: time.Second, Peers: peers}
	monitor := newPeerMonitor(configuration, newUUIDv7(), slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))
	started := make(chan struct{}, len(peers))
	release := make(chan struct{})
	monitor.check = func(ctx context.Context, _ peer, _ time.Duration) peerCheckResult {
		started <- struct{}{}
		select {
		case <-release:
			return peerCheckResult{Outcome: peerReachable, Reason: peerReasonOK}
		case <-ctx.Done():
			return peerCheckResult{Outcome: peerUnknown, Reason: peerReasonCanceled}
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		monitor.run(ctx)
		close(done)
	}()
	for range maxConcurrentPeerChecks {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("peer monitor did not start its immediate checks")
		}
	}
	select {
	case <-started:
		t.Fatalf("peer monitor exceeded concurrency limit %d", maxConcurrentPeerChecks)
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	for range len(peers) - maxConcurrentPeerChecks {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("peer monitor did not complete the remaining checks")
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("peer monitor did not stop after completing checks")
	}
}

func mustPort(t *testing.T, value string) int {
	t.Helper()
	var port int
	if _, err := fmt.Sscanf(value, "%d", &port); err != nil {
		t.Fatalf("parse port %q: %v", value, err)
	}
	return port
}
