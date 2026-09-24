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
		{name: "structured", input: postgresDiagnosticRequest{Operation: "connect", Connection: structuredPostgresInput("none", "")}},
		{name: "controlled delay", input: postgresDiagnosticRequest{Operation: "controlled_delay", Connection: structuredPostgresInput("none", "")}},
		{name: "legacy fields", input: postgresDiagnosticRequest{Operation: "connect", Connection: postgresConnectionInput{Host: "db.example", User: "operator"}}},
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
			if resolved.Credential.Secret == "environment-secret" {
				t.Fatal("inherited PGPASSWORD")
			}
			if resolved.Target.Port != 5432 {
				t.Fatalf("resolved = %+v", resolved)
			}
		})
	}
}

func TestPostgresStructuredContractBoundaries(t *testing.T) {
	tests := []struct {
		name  string
		input postgresConnectionInput
		code  string
	}{
		{name: "database omitted is server selected", input: structuredPostgresInput("none", "")},
		{name: "password", input: structuredPostgresInput("password", "secret")},
		{name: "token", input: structuredPostgresInput("token", "secret")},
		{name: "missing target", input: postgresConnectionInput{Identity: &postgresIdentityInput{User: "operator"}, Credential: &postgresCredentialInput{Type: "none"}}, code: "target_required"},
		{name: "missing identity", input: postgresConnectionInput{Target: &postgresTargetInput{Host: "db.example"}, Credential: &postgresCredentialInput{Type: "none"}}, code: "identity_required"},
		{name: "missing credential", input: postgresConnectionInput{Target: &postgresTargetInput{Host: "db.example"}, Identity: &postgresIdentityInput{User: "operator"}}, code: "credential_required"},
		{name: "password missing secret", input: structuredPostgresInput("password", ""), code: "credential_secret_required"},
		{name: "none with secret", input: structuredPostgresInput("none", "secret"), code: "credential_secret_forbidden"},
		{name: "unsupported credential", input: structuredPostgresInput("iam", "secret"), code: "unsupported_credential_type"},
		{name: "invalid lifecycle", input: func() postgresConnectionInput {
			input := structuredPostgresInput("none", "")
			input.Lifecycle = &postgresLifecycleInput{Mode: "pool"}
			return input
		}(), code: "unsupported_lifecycle_mode"},
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
			if resolved.Database.Name != "" {
				t.Fatalf("database = %q, want server-selected", resolved.Database.Name)
			}
		})
	}
}

func TestPostgresLegacyDatabaseStringRemainsDecodable(t *testing.T) {
	var request postgresDiagnosticRequest
	if err := decodeStrictJSON([]byte(`{"operation":"connect","connection":{"host":"db.example","user":"operator","database":"app"}}`), &request); err != nil {
		t.Fatal(err)
	}
	resolved, err := request.resolve()
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Database.Name != "app" {
		t.Fatalf("database = %q", resolved.Database.Name)
	}
	if err := decodeStrictJSON([]byte(`{"operation":"connect","connection":{"target":{"host":"db.example"},"database":{"name":"app","unknown":true},"identity":{"user":"operator"},"credential":{"type":"none"}}}`), &request); err == nil {
		t.Fatal("unknown nested database field accepted")
	}
}

func structuredPostgresInput(credentialType, secret string) postgresConnectionInput {
	return postgresConnectionInput{
		Target: &postgresTargetInput{Host: "db.example"}, Identity: &postgresIdentityInput{User: "operator"},
		Credential: &postgresCredentialInput{Type: credentialType, Secret: secret},
		TLSConfig:  &postgresTLSInput{Mode: "verify-full"}, Lifecycle: &postgresLifecycleInput{Mode: "ephemeral"},
	}
}

func TestPostgresConfigRejectsInvalidCAAndClassifiesCancellation(t *testing.T) {
	connection, err := structuredPostgresInput("none", "").resolve()
	if err != nil {
		t.Fatal(err)
	}
	connection.Database.Name = "app"
	connection.TLS.CAPEM = "not a certificate"
	target := validatedTarget{host: connection.Target.Host, port: connection.Target.Port, ips: []netip.Addr{netip.MustParseAddr("203.0.113.10")}}
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
	connection, err := structuredPostgresInput("password", "secret").resolve()
	if err != nil {
		t.Fatal(err)
	}
	connection.Database.Name = "app"
	target := validatedTarget{host: connection.Target.Host, port: connection.Target.Port, ips: []netip.Addr{netip.MustParseAddr("203.0.113.10")}}
	config, err := postgresConfig(connection, target)
	if err != nil {
		t.Fatal(err)
	}
	if len(config.Fallbacks) != 0 || config.TLSConfig == nil || config.TLSConfig.ServerName != "db.example" || config.RuntimeParams["statement_timeout"] != "8000" {
		t.Fatalf("config not constrained")
	}
	addresses, err := config.LookupFunc(t.Context(), "db.example")
	if err != nil || len(addresses) != 1 || addresses[0] != "203.0.113.10" {
		t.Fatalf("addresses = %v, err = %v", addresses, err)
	}
}
