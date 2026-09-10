package main

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func TestDiagnosticCapabilitiesFailClosed(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	for _, test := range []struct {
		value     string
		wantError bool
	}{
		{value: "", wantError: false},
		{value: "postgres", wantError: true},
		{value: "transport,unknown", wantError: true},
		{value: "transport,postgres", wantError: true},
	} {
		t.Run(test.value, func(t *testing.T) {
			t.Setenv("TESTKIT_SMOKES", test.value)
			t.Setenv("TESTKIT_DIAGNOSTIC_TOKEN_FILE", "")
			t.Setenv("TESTKIT_POSTGRES_DESTINATIONS_FILE", "")
			service, err := loadDiagnosticsFromEnv(logger)
			if (err != nil) != test.wantError {
				t.Fatalf("service = %v, err = %v", service, err)
			}
			if test.value == "" && service != nil {
				t.Fatal("default enabled diagnostics")
			}
		})
	}
}

func TestDiagnosticCapabilitiesLoadReadOnlyInputs(t *testing.T) {
	directory := t.TempDir()
	tokenPath := filepath.Join(directory, "token")
	policyPath := filepath.Join(directory, "destinations.json")
	if err := os.WriteFile(tokenPath, []byte("0123456789abcdef\n"), 0o400); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(policyPath, []byte(`{"destinations":[{"host":"db.example","ports":[5432]}]}`), 0o400); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TESTKIT_SMOKES", "transport,postgres")
	t.Setenv("TESTKIT_DIAGNOSTIC_TOKEN_FILE", tokenPath)
	t.Setenv("TESTKIT_POSTGRES_DESTINATIONS_FILE", policyPath)
	service, err := loadDiagnosticsFromEnv(slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil || service == nil {
		t.Fatalf("service = %v, err = %v", service, err)
	}
}
