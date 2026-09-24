//go:build integration

package main

import (
	"context"
	"net/netip"
	"os"
	"strconv"
	"testing"
	"time"
)

func TestPostgresIntegration(t *testing.T) {
	dsn := os.Getenv("TESTKIT_POSTGRES_INTEGRATION_DSN")
	if dsn == "" {
		t.Fatal("TESTKIT_POSTGRES_INTEGRATION_DSN is required for integration tests")
	}
	base := postgresDiagnosticRequest{Operation: "connect", Connection: postgresConnectionInput{URI: dsn}}
	resolved, err := base.resolve()
	if err != nil {
		t.Fatal(err)
	}
	address, err := netip.ParseAddr(resolved.Target.Host)
	if err != nil {
		t.Fatalf("integration DSN must use an IP address: %v", err)
	}
	target := validatedTarget{host: resolved.Target.Host, port: resolved.Target.Port, ips: []netip.Addr{address}}
	executor := &postgresExecutor{}

	for _, operation := range []string{"connect", "arithmetic_check", "list_databases", "list_schemas"} {
		t.Run(operation, func(t *testing.T) {
			request := base
			request.Operation = operation
			request.resolved = resolved
			result := executor.Execute(context.Background(), request, target)
			if result.Status != "success" {
				t.Fatalf("result = %+v", result)
			}
		})
	}

	t.Run("server-selected database", func(t *testing.T) {
		request := base
		request.Connection.URI = "postgresql://testkit:testkit@" + resolved.Target.Host + ":" + strconv.Itoa(int(resolved.Target.Port)) + "?sslmode=disable"
		request.resolved, err = request.resolve()
		if err != nil {
			t.Fatal(err)
		}
		if request.resolved.Database.Name != "" {
			t.Fatalf("database = %q", request.resolved.Database.Name)
		}
		result := executor.Execute(context.Background(), request, target)
		data, _ := result.Data.(map[string]any)
		if result.Status != "success" || data["database"] != "testkit" {
			t.Fatalf("result = %+v", result)
		}
	})

	t.Run("retained connection reuses backend and closes", func(t *testing.T) {
		retained := resolved
		retained.Lifecycle.Mode = "retained"
		registry := newPostgresConnectionRegistry(executor)
		view, created, err := registry.Create(t.Context(), retained, target)
		if err != nil || created.Status != "success" {
			t.Fatalf("created = %+v, err = %v", created, err)
		}
		reused, ok := registry.Execute(t.Context(), view.ID, "connect")
		createdData, _ := created.Data.(map[string]any)
		reusedData, _ := reused.Data.(map[string]any)
		if !ok || createdData["backend_pid"] != reusedData["backend_pid"] {
			t.Fatalf("created = %+v, reused = %+v", created, reused)
		}
		started := time.Now()
		delayed, ok := registry.Execute(t.Context(), view.ID, "controlled_delay")
		delayedData, _ := delayed.Data.(map[string]any)
		if elapsed := time.Since(started); !ok || delayed.Status != "success" || elapsed < postgresControlledDelay || delayedData["requested_delay_ms"] != postgresControlledDelay.Milliseconds() {
			t.Fatalf("elapsed = %s, delayed = %+v, ok = %v", elapsed, delayed, ok)
		}
		if !registry.Delete(t.Context(), view.ID) {
			t.Fatal("retained connection was not deleted")
		}
		if _, ok := registry.Execute(t.Context(), view.ID, "connect"); ok {
			t.Fatal("deleted connection remained executable")
		}
	})

	t.Run("incorrect password", func(t *testing.T) {
		request := base
		request.resolved = resolved
		request.resolved.Credential = postgresCredential{Type: "password", Secret: "incorrect"}
		result := executor.Execute(context.Background(), request, target)
		if result.Code != "authentication_failed" {
			t.Fatalf("result = %+v", result)
		}
	})
	t.Run("missing database", func(t *testing.T) {
		request := base
		request.resolved = resolved
		request.resolved.Database.Name = "missing_database"
		result := executor.Execute(context.Background(), request, target)
		if result.Code != "database_unavailable" {
			t.Fatalf("result = %+v", result)
		}
	})
	t.Run("TLS required against plaintext server", func(t *testing.T) {
		request := base
		request.resolved = resolved
		request.resolved.TLS.Mode = "verify-full"
		result := executor.Execute(context.Background(), request, target)
		if result.Status == "success" {
			t.Fatalf("result = %+v", result)
		}
	})
}
