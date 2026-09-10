package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	defaultHTTPPort          = 8080
	defaultSSEInterval       = time.Second
	maxConfigurationFileSize = 64 * 1024
)

type runtimeConfig struct {
	listenAddress string
	sseInterval   time.Duration
	peers         *peerMonitor
	persistence   *persistenceStore
	diagnostics   *diagnosticService
}

func loadRuntimeConfig(logger *slog.Logger) (runtimeConfig, error) {
	listenAddress, err := httpListenAddressFromEnv()
	if err != nil {
		return runtimeConfig{}, err
	}
	peers, err := loadPeerMonitorFromEnv(logger)
	if err != nil {
		return runtimeConfig{}, fmt.Errorf("peer configuration: %w", err)
	}
	var persistence *persistenceStore
	if path := strings.TrimSpace(os.Getenv("TESTKIT_PERSISTENCE_FILE")); path != "" {
		persistence, err = newPersistenceStore(path)
		if err != nil {
			return runtimeConfig{}, fmt.Errorf("persistence configuration: %w", err)
		}
	}
	diagnostics, err := loadDiagnosticsFromEnv(logger)
	if err != nil {
		return runtimeConfig{}, fmt.Errorf("diagnostics configuration: %w", err)
	}
	return runtimeConfig{
		listenAddress: listenAddress,
		sseInterval:   sseIntervalFromEnv(),
		peers:         peers,
		persistence:   persistence,
		diagnostics:   diagnostics,
	}, nil
}

func loadDiagnosticsFromEnv(logger *slog.Logger) (*diagnosticService, error) {
	value := strings.TrimSpace(os.Getenv("TESTKIT_SMOKES"))
	if value == "" {
		value = "transport"
	}
	enabled := map[string]bool{}
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		if item == "" || (item != "transport" && item != "postgres") {
			return nil, fmt.Errorf("TESTKIT_SMOKES contains unknown capability %q", item)
		}
		enabled[item] = true
	}
	if !enabled["transport"] {
		return nil, errors.New("TESTKIT_SMOKES must include transport")
	}
	if !enabled["postgres"] {
		return nil, nil
	}

	tokenPath := strings.TrimSpace(os.Getenv("TESTKIT_DIAGNOSTIC_TOKEN_FILE"))
	policyPath := strings.TrimSpace(os.Getenv("TESTKIT_POSTGRES_DESTINATIONS_FILE"))
	if tokenPath == "" || policyPath == "" {
		return nil, errors.New("postgres requires TESTKIT_DIAGNOSTIC_TOKEN_FILE and TESTKIT_POSTGRES_DESTINATIONS_FILE")
	}
	if !filepath.IsAbs(tokenPath) || !filepath.IsAbs(policyPath) {
		return nil, errors.New("diagnostic token and destination policy paths must be absolute")
	}
	token, err := readLimitedFile(tokenPath, 4*1024)
	if err != nil {
		return nil, fmt.Errorf("read diagnostic token: %w", err)
	}
	token = bytes.TrimSpace(token)
	if len(token) < 16 {
		return nil, errors.New("diagnostic token must contain at least 16 bytes")
	}
	policyData, err := readLimitedFile(policyPath, maxConfigurationFileSize)
	if err != nil {
		return nil, fmt.Errorf("read PostgreSQL destination policy: %w", err)
	}
	policy, err := parseDestinationPolicy(policyData)
	if err != nil {
		return nil, err
	}
	return newDiagnosticService(token, policy, logger), nil
}

func readLimitedFile(path string, limit int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("file exceeds %d bytes", limit)
	}
	return data, nil
}

func decodeStrictJSON(data []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("configuration must contain one JSON value")
	}
	return nil
}

func sseIntervalFromEnv() time.Duration {
	interval, err := time.ParseDuration(os.Getenv("SSE_INTERVAL"))
	if err != nil || interval <= 0 {
		return defaultSSEInterval
	}
	return interval
}

func httpListenAddressFromEnv() (string, error) {
	value := strings.TrimSpace(os.Getenv("HTTP_PORT"))
	if value == "" {
		return net.JoinHostPort("", strconv.Itoa(defaultHTTPPort)), nil
	}
	port, err := strconv.Atoi(value)
	if err != nil || port < 1 || port > 65535 {
		return "", fmt.Errorf("HTTP_PORT must be an integer between 1 and 65535, got %q", value)
	}
	return net.JoinHostPort("", strconv.Itoa(port)), nil
}
