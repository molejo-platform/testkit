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
		if forbiddenDiagnosticAddress(address) || !policy.allows(host, address, port) {
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
		portAllowed := false
		for _, allowedPort := range rule.Ports {
			if port == allowedPort {
				portAllowed = true
				break
			}
		}
		if !portAllowed {
			continue
		}
		if rule.Host == host || (rule.prefix.IsValid() && rule.prefix.Contains(address)) {
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
	service.executor = &postgresExecutor{}
	return service
}

func (service *diagnosticService) handler(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
	writer.Header().Set("X-Frame-Options", "DENY")
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
	validationContext, cancelValidation := context.WithTimeout(request.Context(), 5*time.Second)
	target, err := service.policy.validate(validationContext, resolved.Host, resolved.Port)
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
