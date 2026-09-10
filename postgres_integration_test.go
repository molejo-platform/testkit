package main

import (
	"context"
	"net/netip"
	"os"
	"testing"
)

func TestPostgresIntegration(t *testing.T) {
	dsn := os.Getenv("TESTKIT_POSTGRES_INTEGRATION_DSN")
	if dsn == "" {
		t.Skip("TESTKIT_POSTGRES_INTEGRATION_DSN is not set")
	}
	base := postgresDiagnosticRequest{Operation: "connect", Connection: postgresConnectionInput{URI: dsn}}
	resolved, err := base.resolve()
	if err != nil {
		t.Fatal(err)
	}
	address, err := netip.ParseAddr(resolved.Host)
	if err != nil {
		t.Fatalf("integration DSN must use an IP address: %v", err)
	}
	target := validatedTarget{host: resolved.Host, port: resolved.Port, ips: []netip.Addr{address}}
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

	t.Run("incorrect password", func(t *testing.T) {
		request := base
		request.resolved = resolved
		request.resolved.Password = "incorrect"
		result := executor.Execute(context.Background(), request, target)
		if result.Code != "authentication_failed" {
			t.Fatalf("result = %+v", result)
		}
	})
	t.Run("missing database", func(t *testing.T) {
		request := base
		request.resolved = resolved
		request.resolved.Database = "missing_database"
		result := executor.Execute(context.Background(), request, target)
		if result.Code != "database_unavailable" {
			t.Fatalf("result = %+v", result)
		}
	})
	t.Run("TLS required against plaintext server", func(t *testing.T) {
		request := base
		request.resolved = resolved
		request.resolved.TLS = "verify-full"
		result := executor.Execute(context.Background(), request, target)
		if result.Status == "success" {
			t.Fatalf("result = %+v", result)
		}
	})
}
