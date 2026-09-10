package main

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	maxDiagnosticBodyBytes  = 128 * 1024
	diagnosticTimeout       = 10 * time.Second
	diagnosticConcurrency   = 4
	diagnosticRatePerMinute = 60
	diagnosticAuthHeader    = "Authorization"
)

type destinationPolicyFile struct {
	Destinations []destinationRule `json:"destinations"`
}

type destinationRule struct {
	Host   string   `json:"host,omitempty"`
	CIDR   string   `json:"cidr,omitempty"`
	Ports  []uint16 `json:"ports"`
	prefix netip.Prefix
}

type destinationPolicy struct {
	rules    []destinationRule
	resolver *net.Resolver
}

type validatedTarget struct {
	host string
	port uint16
	ips  []netip.Addr
}

func parseDestinationPolicy(data []byte) (*destinationPolicy, error) {
	var file destinationPolicyFile
	if err := decodeStrictJSON(data, &file); err != nil {
		return nil, fmt.Errorf("invalid PostgreSQL destination policy: %w", err)
	}
	if len(file.Destinations) == 0 {
		return nil, errors.New("PostgreSQL destination policy must contain at least one destination")
	}
	for index := range file.Destinations {
		rule := &file.Destinations[index]
		rule.Host = normalizeHost(rule.Host)
		rule.CIDR = strings.TrimSpace(rule.CIDR)
		if (rule.Host == "") == (rule.CIDR == "") {
			return nil, fmt.Errorf("destination %d must define exactly one of host or cidr", index)
		}
		if len(rule.Ports) == 0 {
			return nil, fmt.Errorf("destination %d must define at least one port", index)
		}
		if rule.CIDR != "" {
			prefix, err := netip.ParsePrefix(rule.CIDR)
			if err != nil {
				return nil, fmt.Errorf("destination %d has invalid cidr", index)
			}
			rule.prefix = prefix.Masked()
		}
	}
	return &destinationPolicy{rules: file.Destinations, resolver: net.DefaultResolver}, nil
}

func normalizeHost(host string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
}

func (policy *destinationPolicy) validate(ctx context.Context, host string, port uint16) (validatedTarget, error) {
	host = normalizeHost(host)
	if host == "" || port == 0 {
		return validatedTarget{}, errors.New("destination_not_allowed")
	}
	var addresses []netip.Addr
	if address, err := netip.ParseAddr(host); err == nil {
		addresses = []netip.Addr{address.Unmap()}
	} else {
		resolved, err := policy.resolver.LookupNetIP(ctx, "ip", host)
		if err != nil || len(resolved) == 0 {
			return validatedTarget{}, errors.New("destination_resolution_failed")
		}
		for _, address := range resolved {
			addresses = append(addresses, address.Unmap())
		}
	}
	for _, address := range addresses {
		if forbiddenDiagnosticAddress(address) {
			if !address.IsLoopback() || !policy.allowsExplicitLoopback(host, port) {
				return validatedTarget{}, errors.New("destination_not_allowed")
			}
			continue
		}
		if !policy.allows(host, address, port) {
			return validatedTarget{}, errors.New("destination_not_allowed")
		}
	}
	return validatedTarget{host: host, port: port, ips: addresses}, nil
}

func (target validatedTarget) dialContext(ctx context.Context, network, _ string) (net.Conn, error) {
	if len(target.ips) == 0 {
		return nil, errors.New("destination_not_allowed")
	}
	address := net.JoinHostPort(target.ips[0].String(), strconv.Itoa(int(target.port)))
	return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, address)
}

func (policy *destinationPolicy) allows(host string, address netip.Addr, port uint16) bool {
	for _, rule := range policy.rules {
		if !rule.allowsPort(port) {
			continue
		}
		if rule.Host == host || (rule.prefix.IsValid() && rule.prefix.Contains(address)) {
			return true
		}
	}
	return false
}

func (policy *destinationPolicy) allowsExplicitLoopback(host string, port uint16) bool {
	explicitLoopback := host == "localhost"
	if address, err := netip.ParseAddr(host); err == nil {
		explicitLoopback = address.Unmap().IsLoopback()
	}
	if !explicitLoopback {
		return false
	}
	for _, rule := range policy.rules {
		if rule.Host == host && rule.allowsPort(port) {
			return true
		}
	}
	return false
}

func (rule destinationRule) allowsPort(port uint16) bool {
	for _, allowedPort := range rule.Ports {
		if port == allowedPort {
			return true
		}
	}
	return false
}

func forbiddenDiagnosticAddress(address netip.Addr) bool {
	address = address.Unmap()
	return !address.IsValid() || address.IsUnspecified() || address.IsLoopback() || address.IsLinkLocalUnicast() ||
		address.IsLinkLocalMulticast() || address.IsMulticast()
}

type diagnosticResult struct {
	SchemaVersion int     `json:"schema_version"`
	CheckID       string  `json:"check_id"`
	Provider      string  `json:"provider"`
	Operation     string  `json:"operation"`
	Status        string  `json:"status"`
	Stage         string  `json:"stage,omitempty"`
	Code          string  `json:"code"`
	DurationMS    float64 `json:"duration_ms"`
	Data          any     `json:"data"`
	Truncated     bool    `json:"truncated,omitempty"`
}

type diagnosticError struct {
	Code string `json:"code"`
}

type postgresDiagnosticExecutor interface {
	Execute(context.Context, postgresDiagnosticRequest, validatedTarget) diagnosticResult
}

type diagnosticService struct {
	tokenHash  [32]byte
	policy     *destinationPolicy
	logger     *slog.Logger
	executor   postgresDiagnosticExecutor
	registry   *postgresConnectionRegistry
	semaphore  chan struct{}
	rateMutex  sync.Mutex
	rateWindow time.Time
	rateCount  int
}

func newDiagnosticService(token []byte, policy *destinationPolicy, logger *slog.Logger) *diagnosticService {
	if logger == nil {
		logger = slog.Default()
	}
	service := &diagnosticService{
		tokenHash: sha256.Sum256(token),
		policy:    policy,
		logger:    logger,
		semaphore: make(chan struct{}, diagnosticConcurrency),
	}
	executor := &postgresExecutor{}
	service.executor = executor
	service.registry = newPostgresConnectionRegistry(executor)
	return service
}

func (service *diagnosticService) handler(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
	writer.Header().Set("X-Frame-Options", "DENY")
	if request.URL.Path == "/api/diagnostics/postgres/capabilities" {
		service.capabilitiesHandler(writer, request)
		return
	}
	if request.URL.Path == "/api/diagnostics/postgres/connections" {
		service.connectionsHandler(writer, request)
		return
	}
	if strings.HasPrefix(request.URL.Path, "/api/diagnostics/postgres/connections/") {
		service.connectionHandler(writer, request)
		return
	}
	if request.URL.Path != "/api/diagnostics/postgres" {
		http.NotFound(writer, request)
		return
	}
	if request.Method != http.MethodPost {
		writer.Header().Set("Allow", http.MethodPost)
		writeJSON(writer, http.StatusMethodNotAllowed, diagnosticError{Code: "method_not_allowed"})
		return
	}
	if strings.TrimSpace(strings.Split(request.Header.Get("Content-Type"), ";")[0]) != "application/json" {
		writeJSON(writer, http.StatusUnsupportedMediaType, diagnosticError{Code: "content_type_required"})
		return
	}
	if !sameRequestOrigin(request) {
		writeJSON(writer, http.StatusForbidden, diagnosticError{Code: "origin_not_allowed"})
		return
	}
	if !service.authorized(request.Header.Get(diagnosticAuthHeader)) {
		writeJSON(writer, http.StatusUnauthorized, diagnosticError{Code: "unauthorized"})
		return
	}
	if !service.allowRate(time.Now()) {
		writer.Header().Set("Retry-After", "60")
		writeJSON(writer, http.StatusTooManyRequests, diagnosticError{Code: "rate_limited"})
		return
	}
	select {
	case service.semaphore <- struct{}{}:
		defer func() { <-service.semaphore }()
	default:
		writeJSON(writer, http.StatusTooManyRequests, diagnosticError{Code: "capacity_exceeded"})
		return
	}

	var input postgresDiagnosticRequest
	if err := decodeStrictRequestJSON(writer, request, maxDiagnosticBodyBytes, &input); err != nil {
		return
	}
	resolved, err := input.resolve()
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, diagnosticError{Code: err.Error()})
		return
	}
	if resolved.Lifecycle.Mode != "ephemeral" {
		writeJSON(writer, http.StatusBadRequest, diagnosticError{Code: "retained_connection_requires_creation"})
		return
	}
	validationContext, cancelValidation := context.WithTimeout(request.Context(), 5*time.Second)
	target, err := service.policy.validate(validationContext, resolved.Target.Host, resolved.Target.Port)
	cancelValidation()
	if err != nil {
		writeJSON(writer, http.StatusForbidden, diagnosticError{Code: err.Error()})
		return
	}
	input.resolved = resolved
	ctx, cancel := context.WithTimeout(request.Context(), diagnosticTimeout)
	defer cancel()
	started := time.Now()
	result := service.executor.Execute(ctx, input, target)
	result.SchemaVersion = 1
	result.CheckID = resolveCorrelationID(request.Header.Get(correlationHeader))
	result.Provider = "postgres"
	result.Operation = input.Operation
	result.DurationMS = durationMilliseconds(time.Since(started))
	service.logger.InfoContext(request.Context(), "diagnostic completed",
		"event", "diagnostic.completed", "provider", result.Provider, "operation", result.Operation,
		"status", result.Status, "code", result.Code, "duration_ms", result.DurationMS, "check_id", result.CheckID)
	writer.Header().Set(correlationHeader, result.CheckID)
	writeJSON(writer, http.StatusOK, result)
}

type postgresCapabilities struct {
	SchemaVersion    int      `json:"schema_version"`
	ConnectionModes  []string `json:"connection_modes"`
	CredentialTypes  []string `json:"credential_types"`
	TLSModes         []string `json:"tls_modes"`
	LifecycleModes   []string `json:"lifecycle_modes"`
	Operations       []string `json:"operations"`
	DatabaseRequired bool     `json:"database_required"`
	DefaultPort      uint16   `json:"default_port"`
	DefaultTLSMode   string   `json:"default_tls_mode"`
	DefaultLifecycle string   `json:"default_lifecycle"`
	MaxRetained      int      `json:"max_retained_connections"`
}

type postgresConnectionCreateRequest struct {
	Connection postgresConnectionInput `json:"connection"`
}

type postgresOperationRequest struct {
	Operation string `json:"operation"`
}

type postgresConnectionCreateResponse struct {
	Connection postgresConnectionView `json:"connection,omitempty"`
	Result     diagnosticResult       `json:"result"`
}

func (service *diagnosticService) capabilitiesHandler(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writer.Header().Set("Allow", http.MethodGet)
		writeJSON(writer, http.StatusMethodNotAllowed, diagnosticError{Code: "method_not_allowed"})
		return
	}
	if !sameRequestOrigin(request) {
		writeJSON(writer, http.StatusForbidden, diagnosticError{Code: "origin_not_allowed"})
		return
	}
	writeJSON(writer, http.StatusOK, postgresCapabilities{
		SchemaVersion: 1, ConnectionModes: []string{"structured", "uri"},
		CredentialTypes: []string{"password", "token", "none"}, TLSModes: []string{"verify-full", "disable"},
		LifecycleModes: []string{"ephemeral", "retained"}, Operations: []string{"connect", "arithmetic_check", "list_databases", "list_schemas"},
		DatabaseRequired: false, DefaultPort: postgresDefaultPort, DefaultTLSMode: "verify-full", DefaultLifecycle: "ephemeral",
		MaxRetained: postgresMaxRetainedConnections,
	})
}

func (service *diagnosticService) connectionsHandler(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		writer.Header().Set("Allow", http.MethodPost)
		writeJSON(writer, http.StatusMethodNotAllowed, diagnosticError{Code: "method_not_allowed"})
		return
	}
	if !service.prepareProtectedRequest(writer, request) {
		return
	}
	if !service.acquireCapacity(writer) {
		return
	}
	defer service.releaseCapacity()
	var input postgresConnectionCreateRequest
	if err := decodeStrictRequestJSON(writer, request, maxDiagnosticBodyBytes, &input); err != nil {
		return
	}
	resolved, err := input.Connection.resolve()
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, diagnosticError{Code: err.Error()})
		return
	}
	if resolved.Lifecycle.Mode != "retained" {
		writeJSON(writer, http.StatusBadRequest, diagnosticError{Code: "retained_lifecycle_required"})
		return
	}
	target, ok := service.validatePostgresTarget(writer, request, resolved)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), diagnosticTimeout)
	defer cancel()
	started := time.Now()
	view, result, err := service.registry.Create(ctx, resolved, target)
	if err != nil {
		status := http.StatusInternalServerError
		if err.Error() == "connection_capacity_exceeded" {
			status = http.StatusTooManyRequests
		} else if err.Error() == "connection_registry_closed" {
			status = http.StatusServiceUnavailable
		}
		writeJSON(writer, status, diagnosticError{Code: err.Error()})
		return
	}
	service.completeResult(request, &result, "connect", started)
	writer.Header().Set(correlationHeader, result.CheckID)
	status := http.StatusCreated
	if result.Status != "success" {
		status = http.StatusOK
	}
	writeJSON(writer, status, postgresConnectionCreateResponse{Connection: view, Result: result})
}

func (service *diagnosticService) connectionHandler(writer http.ResponseWriter, request *http.Request) {
	if !service.prepareProtectedRequest(writer, request) {
		return
	}
	if !service.acquireCapacity(writer) {
		return
	}
	defer service.releaseCapacity()
	remainder := strings.TrimPrefix(request.URL.Path, "/api/diagnostics/postgres/connections/")
	parts := strings.Split(remainder, "/")
	if len(parts) == 1 && parts[0] != "" {
		switch request.Method {
		case http.MethodGet:
			view, ok := service.registry.Get(parts[0])
			if !ok {
				writeJSON(writer, http.StatusNotFound, diagnosticError{Code: "connection_not_found"})
				return
			}
			writeJSON(writer, http.StatusOK, view)
		case http.MethodDelete:
			ctx, cancel := context.WithTimeout(request.Context(), diagnosticTimeout)
			defer cancel()
			if !service.registry.Delete(ctx, parts[0]) {
				writeJSON(writer, http.StatusNotFound, diagnosticError{Code: "connection_not_found"})
				return
			}
			writer.WriteHeader(http.StatusNoContent)
		default:
			writer.Header().Set("Allow", http.MethodGet+", "+http.MethodDelete)
			writeJSON(writer, http.StatusMethodNotAllowed, diagnosticError{Code: "method_not_allowed"})
		}
		return
	}
	if len(parts) == 2 && parts[0] != "" && parts[1] == "operations" && request.Method == http.MethodPost {
		var input postgresOperationRequest
		if err := decodeStrictRequestJSON(writer, request, maxDiagnosticBodyBytes, &input); err != nil {
			return
		}
		if !supportedPostgresOperation(input.Operation) {
			writeJSON(writer, http.StatusBadRequest, diagnosticError{Code: "unsupported_operation"})
			return
		}
		ctx, cancel := context.WithTimeout(request.Context(), diagnosticTimeout)
		defer cancel()
		started := time.Now()
		result, ok := service.registry.Execute(ctx, parts[0], input.Operation)
		if !ok {
			writeJSON(writer, http.StatusNotFound, diagnosticError{Code: "connection_not_found"})
			return
		}
		service.completeResult(request, &result, input.Operation, started)
		writer.Header().Set(correlationHeader, result.CheckID)
		writeJSON(writer, http.StatusOK, result)
		return
	}
	http.NotFound(writer, request)
}

func (service *diagnosticService) prepareProtectedRequest(writer http.ResponseWriter, request *http.Request) bool {
	if request.Method == http.MethodPost && strings.TrimSpace(strings.Split(request.Header.Get("Content-Type"), ";")[0]) != "application/json" {
		writeJSON(writer, http.StatusUnsupportedMediaType, diagnosticError{Code: "content_type_required"})
		return false
	}
	if !sameRequestOrigin(request) {
		writeJSON(writer, http.StatusForbidden, diagnosticError{Code: "origin_not_allowed"})
		return false
	}
	if !service.authorized(request.Header.Get(diagnosticAuthHeader)) {
		writeJSON(writer, http.StatusUnauthorized, diagnosticError{Code: "unauthorized"})
		return false
	}
	if !service.allowRate(time.Now()) {
		writer.Header().Set("Retry-After", "60")
		writeJSON(writer, http.StatusTooManyRequests, diagnosticError{Code: "rate_limited"})
		return false
	}
	return true
}

func (service *diagnosticService) acquireCapacity(writer http.ResponseWriter) bool {
	select {
	case service.semaphore <- struct{}{}:
		return true
	default:
		writeJSON(writer, http.StatusTooManyRequests, diagnosticError{Code: "capacity_exceeded"})
		return false
	}
}

func (service *diagnosticService) releaseCapacity() {
	<-service.semaphore
}

func (service *diagnosticService) validatePostgresTarget(writer http.ResponseWriter, request *http.Request, resolved postgresConnection) (validatedTarget, bool) {
	ctx, cancel := context.WithTimeout(request.Context(), 5*time.Second)
	defer cancel()
	target, err := service.policy.validate(ctx, resolved.Target.Host, resolved.Target.Port)
	if err != nil {
		writeJSON(writer, http.StatusForbidden, diagnosticError{Code: err.Error()})
		return validatedTarget{}, false
	}
	return target, true
}

func (service *diagnosticService) completeResult(request *http.Request, result *diagnosticResult, operation string, started time.Time) {
	result.SchemaVersion = 1
	result.CheckID = resolveCorrelationID(request.Header.Get(correlationHeader))
	result.Provider = "postgres"
	result.Operation = operation
	result.DurationMS = durationMilliseconds(time.Since(started))
	service.logger.InfoContext(request.Context(), "diagnostic completed",
		"event", "diagnostic.completed", "provider", result.Provider, "operation", result.Operation,
		"status", result.Status, "code", result.Code, "duration_ms", result.DurationMS, "check_id", result.CheckID)
}

func supportedPostgresOperation(operation string) bool {
	switch operation {
	case "connect", "arithmetic_check", "list_databases", "list_schemas":
		return true
	default:
		return false
	}
}

func (service *diagnosticService) Close(ctx context.Context) {
	service.registry.CloseAll(ctx)
}

func (service *diagnosticService) authorized(value string) bool {
	const prefix = "Bearer "
	if !strings.HasPrefix(value, prefix) {
		return false
	}
	provided := sha256.Sum256([]byte(strings.TrimSpace(strings.TrimPrefix(value, prefix))))
	return subtle.ConstantTimeCompare(service.tokenHash[:], provided[:]) == 1
}

func (service *diagnosticService) allowRate(now time.Time) bool {
	service.rateMutex.Lock()
	defer service.rateMutex.Unlock()
	if service.rateWindow.IsZero() || now.Sub(service.rateWindow) >= time.Minute {
		service.rateWindow = now
		service.rateCount = 0
	}
	if service.rateCount >= diagnosticRatePerMinute {
		return false
	}
	service.rateCount++
	return true
}

func sameRequestOrigin(request *http.Request) bool {
	origin := request.Header.Get("Origin")
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && strings.EqualFold(parsed.Host, request.Host)
}

func decodeStrictRequestJSON(writer http.ResponseWriter, request *http.Request, limit int64, destination any) error {
	request.Body = http.MaxBytesReader(writer, request.Body, limit)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			writeJSON(writer, http.StatusRequestEntityTooLarge, diagnosticError{Code: "request_too_large"})
			return err
		}
		writeJSON(writer, http.StatusBadRequest, diagnosticError{Code: "invalid_request"})
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		writeJSON(writer, http.StatusBadRequest, diagnosticError{Code: "invalid_request"})
		return errors.New("request must contain one JSON value")
	}
	return nil
}
