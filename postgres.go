package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	postgresDefaultPort   = 5432
	postgresMaxItems      = 100
	postgresMaxCAPEMBytes = 64 * 1024
)

type postgresDiagnosticRequest struct {
	Operation  string                  `json:"operation"`
	Connection postgresConnectionInput `json:"connection"`
	resolved   postgresConnection
}

type postgresConnectionInput struct {
	URI        string                   `json:"uri,omitempty"`
	Target     *postgresTargetInput     `json:"target,omitempty"`
	Database   *postgresDatabaseInput   `json:"database,omitempty"`
	Identity   *postgresIdentityInput   `json:"identity,omitempty"`
	Credential *postgresCredentialInput `json:"credential,omitempty"`
	TLSConfig  *postgresTLSInput        `json:"tls_config,omitempty"`
	Lifecycle  *postgresLifecycleInput  `json:"lifecycle,omitempty"`

	// Deprecated flat fields are accepted temporarily for API compatibility.
	Host     string `json:"host,omitempty"`
	Port     uint16 `json:"port,omitempty"`
	User     string `json:"user,omitempty"`
	Password string `json:"password,omitempty"`
	TLS      string `json:"tls,omitempty"`
	CAPEM    string `json:"ca_pem,omitempty"`
}

type postgresTargetInput struct {
	Host string `json:"host"`
	Port uint16 `json:"port,omitempty"`
}

type postgresDatabaseInput struct {
	Name string `json:"name"`
}

func (input *postgresDatabaseInput) UnmarshalJSON(data []byte) error {
	var legacy string
	if err := json.Unmarshal(data, &legacy); err == nil {
		input.Name = legacy
		return nil
	}
	type databaseInput postgresDatabaseInput
	var decoded databaseInput
	if err := decodeStrictJSON(data, &decoded); err != nil {
		return err
	}
	*input = postgresDatabaseInput(decoded)
	return nil
}

type postgresIdentityInput struct {
	User string `json:"user"`
}

type postgresCredentialInput struct {
	Type   string `json:"type"`
	Secret string `json:"secret,omitempty"`
}

type postgresTLSInput struct {
	Mode  string `json:"mode,omitempty"`
	CAPEM string `json:"ca_pem,omitempty"`
}

type postgresLifecycleInput struct {
	Mode string `json:"mode,omitempty"`
}

type postgresConnection struct {
	Target     postgresTarget
	Database   postgresDatabase
	Identity   postgresIdentity
	Credential postgresCredential
	TLS        postgresTLS
	Lifecycle  postgresLifecycle
}

type postgresTarget struct {
	Host string
	Port uint16
}

type postgresDatabase struct{ Name string }
type postgresIdentity struct{ User string }
type postgresCredential struct {
	Type   string
	Secret string
}
type postgresTLS struct {
	Mode  string
	CAPEM string
}
type postgresLifecycle struct{ Mode string }

func (request postgresDiagnosticRequest) resolve() (postgresConnection, error) {
	switch request.Operation {
	case "connect", "arithmetic_check", "list_databases", "list_schemas":
	default:
		return postgresConnection{}, errors.New("unsupported_operation")
	}
	return request.Connection.resolve()
}

func (input postgresConnectionInput) resolve() (postgresConnection, error) {
	caPEM := input.CAPEM
	if input.TLSConfig != nil {
		caPEM = input.TLSConfig.CAPEM
	}
	if len(caPEM) > postgresMaxCAPEMBytes {
		return postgresConnection{}, errors.New("ca_too_large")
	}
	if strings.TrimSpace(input.URI) != "" {
		if input.Target != nil || input.Database != nil || input.Identity != nil || input.Credential != nil || input.Host != "" || input.Port != 0 || input.User != "" || input.Password != "" || input.TLS != "" {
			return postgresConnection{}, errors.New("connection_modes_conflict")
		}
		if input.TLSConfig != nil && input.TLSConfig.Mode != "" {
			return postgresConnection{}, errors.New("uri_tls_mode_conflict")
		}
		lifecycle := "ephemeral"
		if input.Lifecycle != nil && input.Lifecycle.Mode != "" {
			lifecycle = input.Lifecycle.Mode
		}
		return resolvePostgresURI(input.URI, caPEM, lifecycle)
	}
	if input.hasStructuredFields() && input.hasFlatFields() {
		return postgresConnection{}, errors.New("connection_modes_conflict")
	}
	if input.hasFlatFields() {
		return resolveLegacyPostgresConnection(input)
	}
	if input.Target == nil {
		return postgresConnection{}, errors.New("target_required")
	}
	if input.Identity == nil {
		return postgresConnection{}, errors.New("identity_required")
	}
	if input.Credential == nil {
		return postgresConnection{}, errors.New("credential_required")
	}
	connection := postgresConnection{
		Target:     postgresTarget{Host: normalizeHost(input.Target.Host), Port: input.Target.Port},
		Identity:   postgresIdentity{User: input.Identity.User},
		Credential: postgresCredential{Type: input.Credential.Type, Secret: input.Credential.Secret},
		TLS:        postgresTLS{Mode: "verify-full"}, Lifecycle: postgresLifecycle{Mode: "ephemeral"},
	}
	if input.Database != nil {
		connection.Database.Name = input.Database.Name
	}
	if input.TLSConfig != nil {
		connection.TLS = postgresTLS{Mode: input.TLSConfig.Mode, CAPEM: input.TLSConfig.CAPEM}
	}
	if input.Lifecycle != nil {
		connection.Lifecycle.Mode = input.Lifecycle.Mode
	}
	applyPostgresDefaults(&connection)
	if err := validatePostgresConnection(connection); err != nil {
		return postgresConnection{}, err
	}
	return connection, nil
}

func (input postgresConnectionInput) hasStructuredFields() bool {
	return input.Target != nil || input.Identity != nil || input.Credential != nil || input.TLSConfig != nil || input.Lifecycle != nil
}

func (input postgresConnectionInput) hasFlatFields() bool {
	return input.Host != "" || input.Port != 0 || input.User != "" || input.Password != "" || input.TLS != "" || input.CAPEM != ""
}

func resolveLegacyPostgresConnection(input postgresConnectionInput) (postgresConnection, error) {
	connection := postgresConnection{
		Target:   postgresTarget{Host: normalizeHost(input.Host), Port: input.Port},
		Database: postgresDatabase{Name: ""}, Identity: postgresIdentity{User: input.User},
		Credential: postgresCredential{Type: "none"}, TLS: postgresTLS{Mode: input.TLS, CAPEM: input.CAPEM},
		Lifecycle: postgresLifecycle{Mode: "ephemeral"},
	}
	if input.Database != nil {
		connection.Database.Name = input.Database.Name
	}
	if input.Password != "" {
		connection.Credential = postgresCredential{Type: "password", Secret: input.Password}
	}
	applyPostgresDefaults(&connection)
	if err := validatePostgresConnection(connection); err != nil {
		return postgresConnection{}, err
	}
	return connection, nil
}

func resolvePostgresURI(value, caPEM, lifecycle string) (postgresConnection, error) {
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") || parsed.Host == "" || parsed.Fragment != "" {
		return postgresConnection{}, errors.New("invalid_connection_uri")
	}
	if parsed.User == nil || parsed.User.Username() == "" {
		return postgresConnection{}, errors.New("user_required")
	}
	if parsed.Path != "" && (parsed.Path == "/" || strings.Contains(strings.TrimPrefix(parsed.Path, "/"), "/")) {
		return postgresConnection{}, errors.New("invalid_database")
	}
	port := uint16(postgresDefaultPort)
	if parsed.Port() != "" {
		parsedPort, err := strconv.ParseUint(parsed.Port(), 10, 16)
		if err != nil || parsedPort == 0 {
			return postgresConnection{}, errors.New("invalid_port")
		}
		port = uint16(parsedPort)
	}
	query := parsed.Query()
	for key := range query {
		if key != "sslmode" || len(query[key]) != 1 {
			return postgresConnection{}, errors.New("unsupported_connection_parameter")
		}
	}
	tlsMode := query.Get("sslmode")
	if tlsMode == "" {
		tlsMode = "verify-full"
	}
	password, _ := parsed.User.Password()
	database, err := url.PathUnescape(strings.TrimPrefix(parsed.EscapedPath(), "/"))
	if err != nil {
		return postgresConnection{}, errors.New("invalid_database")
	}
	connection := postgresConnection{
		Target:   postgresTarget{Host: normalizeHost(parsed.Hostname()), Port: port},
		Database: postgresDatabase{Name: database}, Identity: postgresIdentity{User: parsed.User.Username()},
		Credential: postgresCredential{Type: "none"}, TLS: postgresTLS{Mode: tlsMode, CAPEM: caPEM},
		Lifecycle: postgresLifecycle{Mode: lifecycle},
	}
	if password != "" {
		connection.Credential = postgresCredential{Type: "password", Secret: password}
	}
	applyPostgresDefaults(&connection)
	if err := validatePostgresConnection(connection); err != nil {
		return postgresConnection{}, err
	}
	return connection, nil
}

func applyPostgresDefaults(connection *postgresConnection) {
	if connection.Target.Port == 0 {
		connection.Target.Port = postgresDefaultPort
	}
	if connection.TLS.Mode == "" {
		connection.TLS.Mode = "verify-full"
	}
	if connection.Lifecycle.Mode == "" {
		connection.Lifecycle.Mode = "ephemeral"
	}
}

func validatePostgresConnection(connection postgresConnection) error {
	if connection.Target.Host == "" {
		return errors.New("host_required")
	}
	if strings.ContainsAny(connection.Target.Host, `/\\`) {
		return errors.New("invalid_host")
	}
	if connection.Identity.User == "" {
		return errors.New("user_required")
	}
	if strings.ContainsRune(connection.Identity.User, 0) || strings.ContainsRune(connection.Credential.Secret, 0) || strings.ContainsRune(connection.Database.Name, 0) {
		return errors.New("invalid_connection_value")
	}
	if strings.Contains(connection.Database.Name, "/") {
		return errors.New("invalid_database")
	}
	switch connection.Credential.Type {
	case "password", "token":
		if connection.Credential.Secret == "" {
			return errors.New("credential_secret_required")
		}
	case "none":
		if connection.Credential.Secret != "" {
			return errors.New("credential_secret_forbidden")
		}
	default:
		return errors.New("unsupported_credential_type")
	}
	if connection.TLS.Mode != "verify-full" && connection.TLS.Mode != "disable" {
		return errors.New("unsupported_tls_mode")
	}
	if connection.TLS.Mode == "disable" && connection.TLS.CAPEM != "" {
		return errors.New("ca_requires_tls")
	}
	if connection.Lifecycle.Mode != "ephemeral" && connection.Lifecycle.Mode != "retained" {
		return errors.New("unsupported_lifecycle_mode")
	}
	return nil
}

type postgresConnectionHandle interface {
	Execute(context.Context, string, postgresConnection) diagnosticResult
	Close(context.Context) error
}

type postgresConnectionOpener interface {
	Open(context.Context, postgresConnection, validatedTarget) (postgresConnectionHandle, diagnosticResult)
}

type postgresExecutor struct{}

type pgxPostgresConnection struct{ connection *pgx.Conn }

func (executor *postgresExecutor) Open(ctx context.Context, resolved postgresConnection, target validatedTarget) (postgresConnectionHandle, diagnosticResult) {
	config, err := postgresConfig(resolved, target)
	if err != nil {
		return nil, failedPostgresResult("tls", "invalid_ca")
	}
	connection, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		return nil, classifyPostgresError(ctx, err)
	}
	return &pgxPostgresConnection{connection: connection}, diagnosticResult{}
}

func (executor *postgresExecutor) Execute(ctx context.Context, request postgresDiagnosticRequest, target validatedTarget) diagnosticResult {
	handle, failure := executor.Open(ctx, request.resolved, target)
	if handle == nil {
		return failure
	}
	defer handle.Close(context.Background())
	return handle.Execute(ctx, request.Operation, request.resolved)
}

func (handle *pgxPostgresConnection) Execute(ctx context.Context, operation string, resolved postgresConnection) diagnosticResult {
	switch operation {
	case "connect":
		var database, user string
		var backendPID uint32
		if err := handle.connection.QueryRow(ctx, "SELECT current_database(), current_user, pg_backend_pid()").Scan(&database, &user, &backendPID); err != nil {
			return classifyPostgresError(ctx, err)
		}
		return successfulPostgresResult(map[string]any{"connected": true, "tls": resolved.TLS.Mode, "database": database, "user": user, "backend_pid": backendPID})
	case "arithmetic_check":
		return executeArithmeticCheck(ctx, handle.connection)
	case "list_databases":
		return executeListDatabases(ctx, handle.connection)
	case "list_schemas":
		return executeListSchemas(ctx, handle.connection)
	default:
		return failedPostgresResult("operation", "unsupported_operation")
	}
}

func (handle *pgxPostgresConnection) Close(ctx context.Context) error {
	return handle.connection.Close(ctx)
}

func postgresConfig(connection postgresConnection, target validatedTarget) (*pgx.ConnConfig, error) {
	// Parse a fully explicit TLS-disabled URI first so pgx cannot inherit PG* file paths or credentials.
	parsed := &url.URL{
		Scheme: "postgresql",
		Host:   net.JoinHostPort(connection.Target.Host, strconv.Itoa(int(connection.Target.Port))),
		User:   url.UserPassword(connection.Identity.User, connection.Credential.Secret),
	}
	if connection.Database.Name != "" {
		parsed.Path = "/" + url.PathEscape(connection.Database.Name)
	}
	query := url.Values{"sslmode": {"disable"}}
	parsed.RawQuery = query.Encode()
	config, err := pgx.ParseConfig(parsed.String())
	if err != nil {
		return nil, err
	}
	config.Host = connection.Target.Host
	config.Port = connection.Target.Port
	config.User = connection.Identity.User
	config.Password = connection.Credential.Secret
	config.Database = connection.Database.Name
	config.ConnectTimeout = 5 * time.Second
	config.Fallbacks = nil
	config.RuntimeParams = map[string]string{"application_name": "molejo-testkit", "statement_timeout": "8000"}
	config.LookupFunc = func(context.Context, string) ([]string, error) {
		addresses := make([]string, 0, len(target.ips))
		for _, address := range target.ips {
			addresses = append(addresses, address.String())
		}
		return addresses, nil
	}
	config.DialFunc = target.dialContext
	if connection.TLS.Mode == "disable" {
		config.TLSConfig = nil
		return config, nil
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: connection.Target.Host}
	if connection.TLS.CAPEM != "" {
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM([]byte(connection.TLS.CAPEM)) {
			return nil, errors.New("invalid CA")
		}
		tlsConfig.RootCAs = roots
	}
	config.TLSConfig = tlsConfig
	return config, nil
}

func executeArithmeticCheck(ctx context.Context, connection *pgx.Conn) diagnosticResult {
	transaction, err := connection.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return classifyPostgresError(ctx, err)
	}
	defer transaction.Rollback(context.Background())
	var value int
	if err := transaction.QueryRow(ctx, "SELECT 1 + 1").Scan(&value); err != nil {
		return classifyPostgresError(ctx, err)
	}
	if value != 2 {
		return failedPostgresResult("operation", "unexpected_result")
	}
	return successfulPostgresResult(map[string]any{"value": value})
}

func executeListDatabases(ctx context.Context, connection *pgx.Conn) diagnosticResult {
	const query = `SELECT datname FROM pg_catalog.pg_database WHERE datallowconn AND pg_catalog.has_database_privilege(current_user, datname, 'CONNECT') ORDER BY datname LIMIT 101`
	return queryNames(ctx, connection, query, "databases")
}

func executeListSchemas(ctx context.Context, connection *pgx.Conn) diagnosticResult {
	const query = `SELECT nspname FROM pg_catalog.pg_namespace WHERE pg_catalog.has_schema_privilege(current_user, nspname, 'USAGE') AND nspname NOT LIKE 'pg_temp_%' ORDER BY nspname LIMIT 101`
	return queryNames(ctx, connection, query, "schemas")
}

func queryNames(ctx context.Context, connection *pgx.Conn, query, key string) diagnosticResult {
	transaction, err := connection.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return classifyPostgresError(ctx, err)
	}
	defer transaction.Rollback(context.Background())
	rows, err := transaction.Query(ctx, query)
	if err != nil {
		return classifyPostgresError(ctx, err)
	}
	defer rows.Close()
	names := make([]string, 0, postgresMaxItems)
	truncated := false
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return classifyPostgresError(ctx, err)
		}
		if len(names) == postgresMaxItems {
			truncated = true
			break
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		return classifyPostgresError(ctx, err)
	}
	result := successfulPostgresResult(map[string]any{key: names})
	result.Truncated = truncated
	return result
}

func successfulPostgresResult(data any) diagnosticResult {
	return diagnosticResult{Status: "success", Stage: "operation", Code: "ok", Data: data}
}

func failedPostgresResult(stage, code string) diagnosticResult {
	return diagnosticResult{Status: "failed", Stage: stage, Code: code, Data: nil}
}

func classifyPostgresError(ctx context.Context, err error) diagnosticResult {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return diagnosticResult{Status: "timeout", Stage: "unknown", Code: "deadline_exceeded", Data: nil}
	}
	if errors.Is(ctx.Err(), context.Canceled) || errors.Is(err, context.Canceled) {
		return diagnosticResult{Status: "cancelled", Stage: "unknown", Code: "cancelled", Data: nil}
	}
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "28P01", "28000":
			return failedPostgresResult("authentication", "authentication_failed")
		case "3D000":
			return failedPostgresResult("authentication", "database_unavailable")
		case "42501":
			return failedPostgresResult("operation", "insufficient_privilege")
		default:
			return failedPostgresResult("operation", "postgres_error")
		}
	}
	var netError net.Error
	if errors.As(err, &netError) && netError.Timeout() {
		return diagnosticResult{Status: "timeout", Stage: "unknown", Code: "network_timeout", Data: nil}
	}
	return failedPostgresResult("unknown", "connection_failed")
}
