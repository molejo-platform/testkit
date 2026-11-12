package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"
)

const maxUpstreamBodyBytes = 64 * 1024

func runProbe(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	if len(arguments) != 1 {
		fmt.Fprintln(stderr, "usage: testkit probe URL")
		return 2
	}
	upstream, err := url.ParseRequestURI(arguments[0])
	if err != nil || upstream.Host == "" || (upstream.Scheme != "http" && upstream.Scheme != "https") {
		fmt.Fprintln(stderr, "probe URL must be absolute HTTP or HTTPS")
		return 2
	}

	started := time.Now()
	payload, code := probeHTTP(ctx, upstream)
	duration := durationMilliseconds(time.Since(started))
	level, status, exitCode := slog.LevelInfo, "success", 0
	if code != "ok" {
		level, status, exitCode = slog.LevelWarn, "failed", 1
	} else if err := json.NewEncoder(stdout).Encode(payload); err != nil {
		level, status, exitCode = slog.LevelError, "failed", 1
		code = "write_result_failed"
	}
	attributes := []slog.Attr{
		slog.String("event", "probe.completed"),
		slog.String("status", status),
		slog.String("code", code),
		slog.Float64("duration_ms", duration),
		slog.String("correlation_id", newUUIDv7()),
	}
	if payload.UpstreamStatus != 0 {
		attributes = append(attributes, slog.Int("upstream_status_code", payload.UpstreamStatus))
	}
	newLogger(stderr).LogAttrs(ctx, level, "probe completed", attributes...)
	return exitCode
}

func probeHTTP(ctx context.Context, upstream *url.URL) (response, string) {
	client := &http.Client{
		Timeout: 5 * time.Second,
		CheckRedirect: func(redirected *http.Request, _ []*http.Request) error {
			if redirected.URL.Scheme != "http" && redirected.URL.Scheme != "https" {
				return fmt.Errorf("unsupported redirect scheme %q", redirected.URL.Scheme)
			}
			return nil
		},
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, upstream.String(), nil)
	if err != nil {
		return response{}, "request_creation_failed"
	}
	request.Header.Set("User-Agent", "molejo-testkit/"+version)

	upstreamResponse, err := client.Do(request)
	var result response
	if upstreamResponse != nil {
		result.UpstreamStatus = upstreamResponse.StatusCode
	}
	if err != nil {
		return result, probeErrorCode(err, "request_failed")
	}
	defer upstreamResponse.Body.Close()
	body, err := io.ReadAll(io.LimitReader(upstreamResponse.Body, maxUpstreamBodyBytes+1))
	if err != nil {
		return result, probeErrorCode(err, "read_failed")
	}
	if len(body) > maxUpstreamBodyBytes {
		return result, "response_too_large"
	}
	if upstreamResponse.StatusCode < http.StatusOK || upstreamResponse.StatusCode >= http.StatusMultipleChoices {
		return result, "upstream_status"
	}

	return response{
		Status:         "ok",
		Version:        version,
		UpstreamStatus: upstreamResponse.StatusCode,
		UpstreamBody:   string(body),
	}, "ok"
}

func probeErrorCode(err error, fallback string) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline_exceeded"
	case errors.Is(err, context.Canceled):
		return "cancelled"
	default:
		return fallback
	}
}
