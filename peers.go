package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	peerProtocolVersion       = "v1"
	peerSetSchemaVersion      = 1
	peerIdentityPath          = "/api/identity"
	peerCheckHeader           = "X-Testkit-Check-ID"
	maxConfiguredPeers        = 64
	maxPeerIdentityBytes      = 4 * 1024
	maxPeerConfigurationBytes = 256 * 1024
	maxConcurrentPeerChecks   = 4
	peerSnapshotInterval      = 15 * time.Minute
	minPeerCheckInterval      = time.Second
	maxPeerCheckTimeout       = 30 * time.Second
)

const (
	peerReachable   = "reachable"
	peerUnreachable = "unreachable"
	peerUnknown     = "unknown"
)

const (
	peerReasonOK                = "ok"
	peerReasonDNSFailed         = "dns_failed"
	peerReasonRequestTimeout    = "request_timeout"
	peerReasonConnectionRefused = "connection_refused"
	peerReasonConnectFailed     = "connect_failed"
	peerReasonTLSFailed         = "tls_failed"
	peerReasonHTTPFailed        = "http_failed"
	peerReasonIdentityMismatch  = "identity_mismatch"
	peerReasonInvalidResponse   = "invalid_response"
	peerReasonCanceled          = "canceled"
)

var peerIdentifierPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9._-]{0,126}[A-Za-z0-9])?$`)

type peerSet struct {
	SchemaVersion int
	InstanceID    string
	CheckInterval time.Duration
	Timeout       time.Duration
	Peers         []peer
}

type peer struct {
	Name               string `json:"name"`
	Scheme             string `json:"scheme"`
	Host               string `json:"host"`
	Port               int    `json:"port"`
	ExpectedInstanceID string `json:"expected_instance_id"`
}

type peerIdentity struct {
	ProtocolVersion string `json:"protocol_version"`
	InstanceID      string `json:"instance_id"`
	BootID          string `json:"boot_id"`
	Version         string `json:"version"`
}

type peerCheckResult struct {
	Peer               string  `json:"peer"`
	Outcome            string  `json:"outcome"`
	Reason             string  `json:"reason"`
	CheckedAt          string  `json:"checked_at,omitempty"`
	DurationMS         float64 `json:"duration_ms"`
	ObservedInstanceID string  `json:"observed_instance_id,omitempty"`
	ObservedBootID     string  `json:"observed_boot_id,omitempty"`
	CheckID            string  `json:"check_id,omitempty"`
	Detail             string  `json:"-"`
}

type peerMonitor struct {
	configuration peerSet
	bootID        string
	logger        *slog.Logger
	check         func(context.Context, peer, time.Duration) peerCheckResult

	mutex  sync.RWMutex
	states map[string]peerCheckResult
}

type peerSetDocument struct {
	SchemaVersion int    `json:"schema_version"`
	InstanceID    string `json:"instance_id"`
	CheckInterval string `json:"check_interval"`
	Timeout       string `json:"timeout"`
	Peers         []peer `json:"peers"`
}

func loadPeerMonitorFromEnv(logger *slog.Logger) (*peerMonitor, error) {
	path := strings.TrimSpace(os.Getenv("TESTKIT_PEERS_FILE"))
	if path == "" {
		return nil, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open peer configuration: %w", err)
	}
	defer file.Close()
	configuration, err := parsePeerSet(file)
	if err != nil {
		return nil, err
	}
	return newPeerMonitor(configuration, newUUIDv7(), logger), nil
}

func parsePeerSet(reader io.Reader) (peerSet, error) {
	limited := io.LimitReader(reader, maxPeerConfigurationBytes+1)
	decoder := json.NewDecoder(limited)
	decoder.DisallowUnknownFields()
	var document peerSetDocument
	if err := decoder.Decode(&document); err != nil {
		return peerSet{}, fmt.Errorf("decode peer configuration: %w", err)
	}
	var extra interface{}
	if err := decoder.Decode(&extra); err != io.EOF {
		return peerSet{}, errors.New("peer configuration must contain one JSON value")
	}
	if document.SchemaVersion != peerSetSchemaVersion {
		return peerSet{}, fmt.Errorf("unsupported peer configuration schema version %d", document.SchemaVersion)
	}
	if !validPeerIdentifier(document.InstanceID) {
		return peerSet{}, errors.New("instance_id must be a stable identifier")
	}
	checkInterval, err := time.ParseDuration(document.CheckInterval)
	if err != nil || checkInterval < minPeerCheckInterval {
		return peerSet{}, fmt.Errorf("check_interval must be at least %s", minPeerCheckInterval)
	}
	timeout, err := time.ParseDuration(document.Timeout)
	if err != nil || timeout <= 0 || timeout > maxPeerCheckTimeout || timeout >= checkInterval {
		return peerSet{}, fmt.Errorf("timeout must be positive, at most %s, and shorter than check_interval", maxPeerCheckTimeout)
	}
	if len(document.Peers) > maxConfiguredPeers {
		return peerSet{}, fmt.Errorf("peers exceeds the limit of %d", maxConfiguredPeers)
	}
	seen := make(map[string]struct{}, len(document.Peers))
	for index := range document.Peers {
		current := &document.Peers[index]
		if !validPeerIdentifier(current.Name) {
			return peerSet{}, fmt.Errorf("peer %d name must be a stable identifier", index)
		}
		if _, exists := seen[current.Name]; exists {
			return peerSet{}, fmt.Errorf("peer name %q is duplicated", current.Name)
		}
		seen[current.Name] = struct{}{}
		if current.Scheme != "http" && current.Scheme != "https" {
			return peerSet{}, fmt.Errorf("peer %q scheme must be http or https", current.Name)
		}
		if !validPeerHost(current.Host) {
			return peerSet{}, fmt.Errorf("peer %q host is invalid", current.Name)
		}
		if current.Port < 1 || current.Port > 65535 {
			return peerSet{}, fmt.Errorf("peer %q port is invalid", current.Name)
		}
		if !validPeerIdentifier(current.ExpectedInstanceID) {
			return peerSet{}, fmt.Errorf("peer %q expected_instance_id must be a stable identifier", current.Name)
		}
	}
	return peerSet{
		SchemaVersion: document.SchemaVersion,
		InstanceID:    document.InstanceID,
		CheckInterval: checkInterval,
		Timeout:       timeout,
		Peers:         document.Peers,
	}, nil
}

func validPeerIdentifier(value string) bool {
	return peerIdentifierPattern.MatchString(value)
}

func validPeerHost(host string) bool {
	if host == "" || len(host) > 253 {
		return false
	}
	if net.ParseIP(host) != nil {
		return true
	}
	if strings.ContainsAny(host, "/\\@?#%:[] \t\r\n") {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, character := range label {
			if (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') && (character < '0' || character > '9') && character != '-' {
				return false
			}
		}
	}
	return true
}

func newPeerMonitor(configuration peerSet, bootID string, logger *slog.Logger) *peerMonitor {
	return &peerMonitor{
		configuration: configuration,
		bootID:        bootID,
		logger:        logger,
		check:         checkPeer,
		states:        make(map[string]peerCheckResult, len(configuration.Peers)),
	}
}

func (monitor *peerMonitor) identity() peerIdentity {
	return peerIdentity{
		ProtocolVersion: peerProtocolVersion,
		InstanceID:      monitor.configuration.InstanceID,
		BootID:          monitor.bootID,
		Version:         version,
	}
}

func (monitor *peerMonitor) run(ctx context.Context) {
	snapshotTicker := time.NewTicker(peerSnapshotInterval)
	defer snapshotTicker.Stop()

	monitor.checkAll(ctx)
	checkTimer := time.NewTimer(monitor.configuration.CheckInterval)
	defer checkTimer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-checkTimer.C:
			monitor.checkAll(ctx)
			checkTimer.Reset(monitor.configuration.CheckInterval)
		case <-snapshotTicker.C:
			monitor.logSnapshot(ctx)
		}
	}
}

func (monitor *peerMonitor) checkAll(ctx context.Context) {
	var group sync.WaitGroup
	semaphore := make(chan struct{}, maxConcurrentPeerChecks)
	for _, configuredPeer := range monitor.configuration.Peers {
		if ctx.Err() != nil {
			break
		}
		semaphore <- struct{}{}
		group.Add(1)
		go func(target peer) {
			defer group.Done()
			defer func() { <-semaphore }()
			started := time.Now()
			result := monitor.check(ctx, target, monitor.configuration.Timeout)
			if ctx.Err() != nil {
				return
			}
			result.Peer = target.Name
			if result.CheckedAt == "" {
				result.CheckedAt = time.Now().UTC().Format(time.RFC3339Nano)
			}
			if result.DurationMS == 0 {
				result.DurationMS = durationMilliseconds(time.Since(started))
			}
			monitor.record(ctx, result)
		}(configuredPeer)
	}
	group.Wait()
}

func (monitor *peerMonitor) record(ctx context.Context, result peerCheckResult) {
	monitor.mutex.Lock()
	previous, existed := monitor.states[result.Peer]
	stored := result
	stored.Detail = ""
	monitor.states[result.Peer] = stored
	monitor.mutex.Unlock()
	if existed && previous.Outcome == result.Outcome && previous.Reason == result.Reason && previous.ObservedBootID == result.ObservedBootID {
		return
	}
	previousOutcome := peerUnknown
	previousReason := "not_checked"
	if existed {
		previousOutcome = previous.Outcome
		previousReason = previous.Reason
	}
	monitor.logger.InfoContext(ctx, "peer state changed",
		"event", "peer.state.changed",
		"peer", result.Peer,
		"previous_outcome", previousOutcome,
		"previous_reason", previousReason,
		"outcome", result.Outcome,
		"reason", result.Reason,
		"previous_boot_id", previous.ObservedBootID,
		"observed_boot_id", result.ObservedBootID,
		"duration_ms", result.DurationMS,
		"check_id", result.CheckID,
	)
}

func (monitor *peerMonitor) snapshot() []peerCheckResult {
	monitor.mutex.RLock()
	results := make([]peerCheckResult, 0, len(monitor.configuration.Peers))
	for _, configuredPeer := range monitor.configuration.Peers {
		result, exists := monitor.states[configuredPeer.Name]
		if !exists {
			result = peerCheckResult{Peer: configuredPeer.Name, Outcome: peerUnknown, Reason: "not_checked"}
		}
		result.Detail = ""
		results = append(results, result)
	}
	monitor.mutex.RUnlock()
	sort.Slice(results, func(left, right int) bool { return results[left].Peer < results[right].Peer })
	return results
}

func (monitor *peerMonitor) logSnapshot(ctx context.Context) {
	if len(monitor.configuration.Peers) == 0 {
		return
	}
	counts := map[string]int{peerReachable: 0, peerUnreachable: 0, peerUnknown: 0}
	for _, result := range monitor.snapshot() {
		counts[result.Outcome]++
	}
	counts[peerUnknown] += len(monitor.configuration.Peers) - counts[peerReachable] - counts[peerUnreachable] - counts[peerUnknown]
	monitor.logger.InfoContext(ctx, "peer connectivity snapshot",
		"event", "peers.snapshot",
		"configured", len(monitor.configuration.Peers),
		"reachable", counts[peerReachable],
		"unreachable", counts[peerUnreachable],
		"unknown", counts[peerUnknown],
	)
}

func checkPeer(ctx context.Context, target peer, timeout time.Duration) peerCheckResult {
	started := time.Now()
	checkID := newUUIDv7()
	result := peerCheckResult{Peer: target.Name, CheckID: checkID}

	dialer := &net.Dialer{Timeout: timeout, KeepAlive: -1}
	transport := &http.Transport{
		Proxy:                 nil,
		DialContext:           dialer.DialContext,
		DisableKeepAlives:     true,
		TLSHandshakeTimeout:   timeout,
		ResponseHeaderTimeout: timeout,
		ForceAttemptHTTP2:     false,
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	targetURL := url.URL{
		Scheme: target.Scheme,
		Host:   net.JoinHostPort(target.Host, fmt.Sprintf("%d", target.Port)),
		Path:   peerIdentityPath,
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL.String(), nil)
	if err != nil {
		return completedPeerResult(result, peerUnknown, peerReasonInvalidResponse, err, started)
	}
	request.Header.Set("User-Agent", "molejo-testkit/"+version)
	request.Header.Set(peerCheckHeader, checkID)
	responseValue, err := client.Do(request)
	if err != nil {
		outcome, reason := classifyPeerRequestError(ctx, err)
		return completedPeerResult(result, outcome, reason, err, started)
	}
	defer responseValue.Body.Close()
	if responseValue.StatusCode < http.StatusOK || responseValue.StatusCode >= http.StatusMultipleChoices {
		return completedPeerResult(result, peerUnknown, peerReasonHTTPFailed, nil, started)
	}
	body, err := io.ReadAll(io.LimitReader(responseValue.Body, maxPeerIdentityBytes+1))
	if err != nil || len(body) > maxPeerIdentityBytes {
		return completedPeerResult(result, peerUnknown, peerReasonInvalidResponse, err, started)
	}
	var identity peerIdentity
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	if err := decoder.Decode(&identity); err != nil {
		return completedPeerResult(result, peerUnknown, peerReasonInvalidResponse, err, started)
	}
	var extra interface{}
	if err := decoder.Decode(&extra); err != io.EOF {
		return completedPeerResult(result, peerUnknown, peerReasonInvalidResponse, err, started)
	}
	if identity.ProtocolVersion != peerProtocolVersion || !validPeerIdentifier(identity.InstanceID) || !isUUIDv7(identity.BootID) {
		return completedPeerResult(result, peerUnknown, peerReasonInvalidResponse, nil, started)
	}
	result.ObservedInstanceID = identity.InstanceID
	result.ObservedBootID = identity.BootID
	if identity.InstanceID != target.ExpectedInstanceID {
		return completedPeerResult(result, peerUnknown, peerReasonIdentityMismatch, nil, started)
	}
	return completedPeerResult(result, peerReachable, peerReasonOK, nil, started)
}

func completedPeerResult(result peerCheckResult, outcome, reason string, err error, started time.Time) peerCheckResult {
	result.Outcome = outcome
	result.Reason = reason
	if err != nil {
		result.Detail = err.Error()
	}
	result.CheckedAt = time.Now().UTC().Format(time.RFC3339Nano)
	result.DurationMS = durationMilliseconds(time.Since(started))
	return result
}

func classifyPeerRequestError(ctx context.Context, err error) (string, string) {
	if ctx.Err() != nil || errors.Is(err, context.Canceled) {
		return peerUnknown, peerReasonCanceled
	}
	var dnsError *net.DNSError
	if errors.As(err, &dnsError) {
		return peerUnreachable, peerReasonDNSFailed
	}
	var certificateError *tls.CertificateVerificationError
	var unknownAuthority x509.UnknownAuthorityError
	var recordHeaderError tls.RecordHeaderError
	if errors.As(err, &certificateError) || errors.As(err, &unknownAuthority) || errors.As(err, &recordHeaderError) {
		return peerUnknown, peerReasonTLSFailed
	}
	if errors.Is(err, syscall.ECONNREFUSED) {
		return peerUnreachable, peerReasonConnectionRefused
	}
	var networkError net.Error
	if errors.As(err, &networkError) && networkError.Timeout() {
		return peerUnreachable, peerReasonRequestTimeout
	}
	return peerUnreachable, peerReasonConnectFailed
}

func (monitor *peerMonitor) identityHandler(writer http.ResponseWriter, request *http.Request) {
	checkID := resolveCorrelationID(request.Header.Get(peerCheckHeader))
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set(peerCheckHeader, checkID)
	monitor.logger.InfoContext(request.Context(), "peer identity requested",
		"event", "peer.identity.requested",
		"check_id", checkID,
	)
	writeJSON(writer, http.StatusOK, monitor.identity())
}

func (monitor *peerMonitor) stateHandler(writer http.ResponseWriter, _ *http.Request) {
	writer.Header().Set("Cache-Control", "no-store")
	writeJSON(writer, http.StatusOK, struct {
		ProtocolVersion string            `json:"protocol_version"`
		InstanceID      string            `json:"instance_id"`
		BootID          string            `json:"boot_id"`
		Peers           []peerCheckResult `json:"peers"`
	}{
		ProtocolVersion: peerProtocolVersion,
		InstanceID:      monitor.configuration.InstanceID,
		BootID:          monitor.bootID,
		Peers:           monitor.snapshot(),
	})
}
