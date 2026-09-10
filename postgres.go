package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
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
	postgresDefaultPort     = 5432
	postgresDefaultDatabase = "postgres"
	postgresMaxItems        = 100
	postgresMaxCAPEMBytes   = 64 * 1024
)

type postgresDiagnosticRequest struct {
	Operation  string                  `json:"operation"`
	Connection postgresConnectionInput `json:"connection"`
	resolved   postgresConnection
}

type postgresConnectionInput struct {
	URI      string `json:"uri,omitempty"`
	Host     string `json:"host,omitempty"`
	Port     uint16 `json:"port,omitempty"`
	User     string `json:"user,omitempty"`
	Password string `json:"password,omitempty"`
	Database string `json:"database,omitempty"`
	TLS      string `json:"tls,omitempty"`
	CAPEM    string `json:"ca_pem,omitempty"`
}

type postgresConnection struct {
	Host     string
	Port     uint16
	User     string
	Password string
	Database string
	TLS      string
	CAPEM    string
}

func (request postgresDiagnosticRequest) resolve() (postgresConnection, error) {
	switch request.Operation {
	case "connect", "arithmetic_check", "list_databases", "list_schemas":
	default:
		return postgresConnection{}, errors.New("unsupported_operation")
	}
	input := request.Connection
	if len(input.CAPEM) > postgresMaxCAPEMBytes {
		return postgresConnection{}, errors.New("ca_too_large")
	}
	if strings.TrimSpace(input.URI) != "" {
		if input.Host != "" || input.Port != 0 || input.User != "" || input.Password != "" || input.Database != "" || input.TLS != "" {
			return postgresConnection{}, errors.New("connection_modes_conflict")
		}
		return resolvePostgresURI(input.URI, input.CAPEM)
	}
	connection := postgresConnection{
		Host: normalizeHost(input.Host), Port: input.Port, User: input.User, Password: input.Password,
		Database: input.Database, TLS: input.TLS, CAPEM: input.CAPEM,
	}
	if connection.Port == 0 {
		connection.Port = postgresDefaultPort
	}
	if connection.Database == "" {
		connection.Database = postgresDefaultDatabase
	}
	if connection.TLS == "" {
		connection.TLS = "verify-full"
	}
	if err := validatePostgresConnection(connection); err != nil {
		return postgresConnection{}, err
	}
	return connection, nil
}

func resolvePostgresURI(value, caPEM string) (postgresConnection, error) {
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
	if database == "" {
		database = postgresDefaultDatabase
	}
	connection := postgresConnection{
		Host: normalizeHost(parsed.Hostname()), Port: port, User: parsed.User.Username(), Password: password,
		Database: database, TLS: tlsMode, CAPEM: caPEM,
	}
	if err := validatePostgresConnection(connection); err != nil {
		return postgresConnection{}, err
	}
	return connection, nil
}

func validatePostgresConnection(connection postgresConnection) error {
	if connection.Host == "" {
		return errors.New("host_required")
	}
	if strings.ContainsAny(connection.Host, `/\\`) {
		return errors.New("invalid_host")
	}
	if connection.User == "" {
		return errors.New("user_required")
	}
	if strings.ContainsRune(connection.User, 0) || strings.ContainsRune(connection.Password, 0) || strings.ContainsRune(connection.Database, 0) {
		return errors.New("invalid_connection_value")
	}
	if connection.Database == "" || strings.Contains(connection.Database, "/") {
		return errors.New("invalid_database")
	}
	if connection.TLS != "verify-full" && connection.TLS != "disable" {
		return errors.New("unsupported_tls_mode")
	}
	if connection.TLS == "disable" && connection.CAPEM != "" {
		return errors.New("ca_requires_tls")
	}
	return nil
}

type postgresExecutor struct{}

func (executor *postgresExecutor) Execute(ctx context.Context, request postgresDiagnosticRequest, target validatedTarget) diagnosticResult {
	config, err := postgresConfig(request.resolved, target)
	if err != nil {
		return failedPostgresResult("tls", "invalid_ca")
	}
	connection, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		return classifyPostgresError(ctx, err)
	}
	defer connection.Close(context.Background())

	switch request.Operation {
	case "connect":
		return successfulPostgresResult(map[string]any{"connected": true, "tls": request.resolved.TLS})
	case "arithmetic_check":
		return executeArithmeticCheck(ctx, connection)
	case "list_databases":
		return executeListDatabases(ctx, connection)
	case "list_schemas":
		return executeListSchemas(ctx, connection)
	default:
		return failedPostgresResult("operation", "unsupported_operation")
	}
}

func postgresConfig(connection postgresConnection, target validatedTarget) (*pgx.ConnConfig, error) {
	// Parse a fully explicit TLS-disabled URI first so pgx cannot inherit PG* file paths or credentials.
	parsed := &url.URL{
		Scheme: "postgresql",
		Host:   net.JoinHostPort(connection.Host, strconv.Itoa(int(connection.Port))),
		Path:   "/" + url.PathEscape(connection.Database),
		User:   url.UserPassword(connection.User, connection.Password),
	}
	query := url.Values{"sslmode": {"disable"}}
	parsed.RawQuery = query.Encode()
	config, err := pgx.ParseConfig(parsed.String())
	if err != nil {
		return nil, err
	}
	config.Host = connection.Host
	config.Port = connection.Port
	config.User = connection.User
	config.Password = connection.Password
	config.Database = connection.Database
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
	if connection.TLS == "disable" {
		config.TLSConfig = nil
		return config, nil
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: connection.Host}
	if connection.CAPEM != "" {
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM([]byte(connection.CAPEM)) {
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
