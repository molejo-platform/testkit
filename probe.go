package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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

	payload, err := probeHTTP(ctx, upstream)
	if err != nil {
		fmt.Fprintf(stderr, "probe failed: %v\n", err)
		return 1
	}
	if err := json.NewEncoder(stdout).Encode(payload); err != nil {
		fmt.Fprintf(stderr, "write probe result: %v\n", err)
		return 1
	}
	return 0
}

func probeHTTP(ctx context.Context, upstream *url.URL) (response, error) {
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
		return response{}, fmt.Errorf("create request: %w", err)
	}
	request.Header.Set("User-Agent", "molejo-testkit/"+version)

	upstreamResponse, err := client.Do(request)
	if err != nil {
		return response{}, fmt.Errorf("request upstream: %w", err)
	}
	defer upstreamResponse.Body.Close()
	body, err := io.ReadAll(io.LimitReader(upstreamResponse.Body, maxUpstreamBodyBytes+1))
	if err != nil {
		return response{}, fmt.Errorf("read upstream response: %w", err)
	}
	if len(body) > maxUpstreamBodyBytes {
		return response{}, fmt.Errorf("upstream response exceeds %d bytes", maxUpstreamBodyBytes)
	}
	if upstreamResponse.StatusCode < http.StatusOK || upstreamResponse.StatusCode >= http.StatusMultipleChoices {
		return response{}, fmt.Errorf("upstream returned %s", upstreamResponse.Status)
	}

	return response{
		Status:         "ok",
		Version:        version,
		UpstreamStatus: upstreamResponse.StatusCode,
		UpstreamBody:   string(body),
	}, nil
}
