package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakePostgresExecutor struct{ calls int }

func (executor *fakePostgresExecutor) Execute(_ context.Context, _ postgresDiagnosticRequest, _ validatedTarget) diagnosticResult {
	executor.calls++
	return successfulPostgresResult(map[string]any{"connected": true})
}

func testDiagnosticService(t *testing.T) (*diagnosticService, *fakePostgresExecutor) {
	t.Helper()
	policy, err := parseDestinationPolicy([]byte(`{"destinations":[{"host":"203.0.113.10","ports":[5432]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	executor := &fakePostgresExecutor{}
	service := newDiagnosticService([]byte("0123456789abcdef"), policy, slog.New(slog.NewTextHandler(io.Discard, nil)))
	service.executor = executor
	return service, executor
}

func diagnosticRequest(body string) *http.Request {
	request := httptest.NewRequest(http.MethodPost, "/api/diagnostics/postgres", strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer 0123456789abcdef")
	request.Header.Set("Content-Type", "application/json")
	return request
}

func TestDiagnosticEndpointIsAbsentWhenDisabled(t *testing.T) {
	response := httptest.NewRecorder()
	newHandlerWithConfig(handlerConfig{}).ServeHTTP(response, diagnosticRequest(`{}`))
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d", response.Code)
	}
}

func TestPostgresCapabilityControlsAPIPageAndCatalogTogether(t *testing.T) {
	service, _ := testDiagnosticService(t)
	for _, test := range []struct {
		name        string
		diagnostics *diagnosticService
		pageStatus  int
		card        bool
	}{
		{name: "disabled", pageStatus: http.StatusNotFound},
		{name: "enabled", diagnostics: service, pageStatus: http.StatusOK, card: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			handler := newHandlerWithConfig(handlerConfig{diagnostics: test.diagnostics})
			page := httptest.NewRecorder()
			handler.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/en/postgres", nil))
			if page.Code != test.pageStatus {
				t.Fatalf("page status = %d", page.Code)
			}
			home := httptest.NewRecorder()
			handler.ServeHTTP(home, httptest.NewRequest(http.MethodGet, "/en/", nil))
			if strings.Contains(home.Body.String(), "/en/postgres") != test.card {
				t.Fatalf("card presence = %v", !test.card)
			}
		})
	}
}

func TestDiagnosticEndpointRejectsUnauthorizedAndForbiddenTargetsBeforeExecution(t *testing.T) {
	service, executor := testDiagnosticService(t)
	handler := newHandlerWithConfig(handlerConfig{diagnostics: service})
	body := `{"operation":"connect","connection":{"host":"198.51.100.10","user":"operator","tls":"disable"}}`
	unauthorized := diagnosticRequest(body)
	unauthorized.Header.Del("Authorization")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, unauthorized)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", response.Code)
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, diagnosticRequest(body))
	if response.Code != http.StatusForbidden {
		t.Fatalf("forbidden status = %d", response.Code)
	}
	if executor.calls != 0 {
		t.Fatalf("executor calls = %d", executor.calls)
	}
}

func TestDiagnosticEndpointExecutesAllowedTargetWithoutEchoingSecrets(t *testing.T) {
	service, executor := testDiagnosticService(t)
	response := httptest.NewRecorder()
	body := `{"operation":"connect","connection":{"target":{"host":"203.0.113.10"},"identity":{"user":"operator"},"credential":{"type":"password","secret":"sentinel-secret"},"tls_config":{"mode":"disable"},"lifecycle":{"mode":"ephemeral"}}}`
	newHandlerWithConfig(handlerConfig{diagnostics: service}).ServeHTTP(response, diagnosticRequest(body))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if executor.calls != 1 {
		t.Fatalf("executor calls = %d", executor.calls)
	}
	if strings.Contains(response.Body.String(), "sentinel-secret") {
		t.Fatal("response exposed password")
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("cache control = %q", response.Header().Get("Cache-Control"))
	}
	var result diagnosticResult
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Status != "success" || result.CheckID == "" {
		t.Fatalf("result = %+v", result)
	}
}

func TestDiagnosticEndpointRejectsExtraJSONBeforeExecution(t *testing.T) {
	service, executor := testDiagnosticService(t)
	response := httptest.NewRecorder()
	newHandlerWithConfig(handlerConfig{diagnostics: service}).ServeHTTP(response,
		diagnosticRequest(`{"operation":"connect","connection":{"host":"203.0.113.10","user":"operator"}}{}`))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", response.Code)
	}
	if executor.calls != 0 {
		t.Fatalf("executor calls = %d", executor.calls)
	}
}

func TestDiagnosticEndpointEnforcesRequestLimitsBeforeExecution(t *testing.T) {
	validBody := `{"operation":"connect","connection":{"host":"203.0.113.10","user":"operator","tls":"disable"}}`
	tests := []struct {
		name       string
		prepare    func(*diagnosticService)
		request    func() *http.Request
		wantStatus int
	}{
		{
			name: "content type",
			request: func() *http.Request {
				request := diagnosticRequest(validBody)
				request.Header.Del("Content-Type")
				return request
			},
			wantStatus: http.StatusUnsupportedMediaType,
		},
		{
			name: "body size",
			request: func() *http.Request {
				return diagnosticRequest(strings.Repeat(" ", maxDiagnosticBodyBytes+1))
			},
			wantStatus: http.StatusRequestEntityTooLarge,
		},
		{
			name: "rate limit",
			prepare: func(service *diagnosticService) {
				service.rateWindow = time.Now()
				service.rateCount = diagnosticRatePerMinute
			},
			request:    func() *http.Request { return diagnosticRequest(validBody) },
			wantStatus: http.StatusTooManyRequests,
		},
		{
			name: "concurrency limit",
			prepare: func(service *diagnosticService) {
				for range diagnosticConcurrency {
					service.semaphore <- struct{}{}
				}
			},
			request:    func() *http.Request { return diagnosticRequest(validBody) },
			wantStatus: http.StatusTooManyRequests,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service, executor := testDiagnosticService(t)
			if test.prepare != nil {
				test.prepare(service)
			}
			response := httptest.NewRecorder()
			newHandlerWithConfig(handlerConfig{diagnostics: service}).ServeHTTP(response, test.request())
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
			}
			if executor.calls != 0 {
				t.Fatalf("executor calls = %d", executor.calls)
			}
		})
	}
}

func TestDiagnosticEndpointRejectsFreeSQLAndCrossOriginBeforeExecution(t *testing.T) {
	service, executor := testDiagnosticService(t)
	handler := newHandlerWithConfig(handlerConfig{diagnostics: service})

	withSQL := diagnosticRequest(`{"operation":"connect","sql":"DROP TABLE x","connection":{"host":"203.0.113.10","user":"operator"}}`)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, withSQL)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("SQL status = %d", response.Code)
	}

	crossOrigin := diagnosticRequest(`{"operation":"connect","connection":{"host":"203.0.113.10","user":"operator"}}`)
	crossOrigin.Header.Set("Origin", "https://attacker.example")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, crossOrigin)
	if response.Code != http.StatusForbidden {
		t.Fatalf("origin status = %d", response.Code)
	}
	if executor.calls != 0 {
		t.Fatalf("executor calls = %d", executor.calls)
	}
}

func TestDestinationPolicyAllowsOnlyExplicitLoopbackHostRules(t *testing.T) {
	for _, host := range []string{"localhost", "127.0.0.1", "::1"} {
		t.Run(host, func(t *testing.T) {
			policy, err := parseDestinationPolicy([]byte(fmt.Sprintf(`{"destinations":[{"host":%q,"ports":[5432]}]}`, host)))
			if err != nil {
				t.Fatal(err)
			}
			target, err := policy.validate(context.Background(), host, 5432)
			if err != nil {
				t.Fatal(err)
			}
			for _, address := range target.ips {
				if !address.IsLoopback() {
					t.Fatalf("resolved non-loopback address %s", address)
				}
			}
			if _, err := policy.validate(context.Background(), host, 5433); err == nil {
				t.Fatal("allowed a port absent from the exact host rule")
			}
		})
	}
}

func TestDestinationPolicyBlocksSpecialAddressesFromBroadCIDRs(t *testing.T) {
	policy, err := parseDestinationPolicy([]byte(`{"destinations":[{"cidr":"0.0.0.0/0","ports":[5432]},{"cidr":"::/0","ports":[5432]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, host := range []string{"127.0.0.1", "169.254.169.254", "169.254.170.2", "0.0.0.0", "::1", "::ffff:127.0.0.1"} {
		if _, err := policy.validate(context.Background(), host, 5432); err == nil {
			t.Errorf("allowed %s", host)
		}
	}
}
