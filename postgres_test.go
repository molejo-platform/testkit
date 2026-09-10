package main

import (
	"context"
	"errors"
	"net/netip"
	"testing"
)

func TestPostgresConnectionResolution(t *testing.T) {
	t.Setenv("PGPASSWORD", "environment-secret")
	tests := []struct {
		name  string
		input postgresDiagnosticRequest
		code  string
	}{
		{name: "fields", input: postgresDiagnosticRequest{Operation: "connect", Connection: postgresConnectionInput{Host: "db.example", User: "operator"}}},
		{name: "uri", input: postgresDiagnosticRequest{Operation: "connect", Connection: postgresConnectionInput{URI: "postgresql://operator:secret@db.example/app?sslmode=verify-full"}}},
		{name: "conflicting modes", input: postgresDiagnosticRequest{Operation: "connect", Connection: postgresConnectionInput{URI: "postgresql://operator@db.example/app", Host: "other"}}, code: "connection_modes_conflict"},
		{name: "forbidden parameter", input: postgresDiagnosticRequest{Operation: "connect", Connection: postgresConnectionInput{URI: "postgresql://operator@db.example/app?passfile=/tmp/pass"}}, code: "unsupported_connection_parameter"},
		{name: "unknown operation", input: postgresDiagnosticRequest{Operation: "query", Connection: postgresConnectionInput{Host: "db.example", User: "operator"}}, code: "unsupported_operation"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resolved, err := test.input.resolve()
			if test.code != "" {
				if err == nil || err.Error() != test.code {
					t.Fatalf("error = %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if resolved.Password == "environment-secret" {
				t.Fatal("inherited PGPASSWORD")
			}
			if resolved.Port != 5432 || resolved.Database == "" {
				t.Fatalf("resolved = %+v", resolved)
			}
		})
	}
}

func TestPostgresConfigRejectsInvalidCAAndClassifiesCancellation(t *testing.T) {
	connection := postgresConnection{Host: "db.example", Port: 5432, User: "operator", Database: "app", TLS: "verify-full", CAPEM: "not a certificate"}
	target := validatedTarget{host: connection.Host, port: connection.Port, ips: []netip.Addr{netip.MustParseAddr("203.0.113.10")}}
	if _, err := postgresConfig(connection, target); err == nil {
		t.Fatal("invalid CA accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result := classifyPostgresError(ctx, errors.New("driver detail"))
	if result.Status != "cancelled" || result.Code != "cancelled" {
		t.Fatalf("result = %+v", result)
	}
}

func TestPostgresConfigPinsValidatedAddressAndClearsFallbacks(t *testing.T) {
	connection := postgresConnection{Host: "db.example", Port: 5432, User: "operator", Password: "secret", Database: "app", TLS: "verify-full"}
	target := validatedTarget{host: connection.Host, port: connection.Port, ips: []netip.Addr{netip.MustParseAddr("203.0.113.10")}}
	config, err := postgresConfig(connection, target)
	if err != nil {
		t.Fatal(err)
	}
	if len(config.Fallbacks) != 0 || config.TLSConfig == nil || config.TLSConfig.ServerName != "db.example" {
		t.Fatalf("config not constrained")
	}
	addresses, err := config.LookupFunc(t.Context(), "db.example")
	if err != nil || len(addresses) != 1 || addresses[0] != "203.0.113.10" {
		t.Fatalf("addresses = %v, err = %v", addresses, err)
	}
}
