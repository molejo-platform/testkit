# Molejo Testkit

[Português (Brasil)](docs/pt-BR/README.md) |
[Español (Argentina)](docs/es-AR/README.md)

> Experimental pre-release project. Molejo Testkit is a test fixture, not a
> production application.

Molejo Testkit is a small, deterministic container image for smoke-testing
network reachability and minimal HTTP transport contracts. It can also exercise
Kubernetes application delivery and network policies. A single static Go binary
provides REST, GraphQL, Server-Sent Events (SSE), WebSocket, health endpoints,
and an explicit outbound probe command.

The public HTTP server and arbitrary network probe are intentionally separate.
Server mode can contact only peers declared in an optional read-only file, so a
public request cannot turn the workload into an outbound proxy. One-off network
diagnostics remain an explicit container command.

## Purpose and scope

Testkit answers a focused question: can a client reach the disposable workload,
and can the selected transport complete its minimal exchange? A successful
WebSocket smoke test confirms the network path, HTTP upgrade, bidirectional frame
exchange, and basic close behavior for that deployment path.

It is not a feature-complete implementation or a substitute for application
tests. A successful smoke test does not validate authentication, authorization,
rooms, routing, business rules, complete message schemas, performance, load,
or the availability of unrelated dependencies.

## Smoke-test surfaces

| Capability | Interface |
| --- | --- |
| Language detection aliases | `GET /`, `GET /websocket`, `GET /rest`, `GET /graphql-lab`, `GET /sse` |
| Localized browser pages | `GET /en/`, `GET /pt-BR/`, `GET /es-AR/` |
| Localized browser labs | `GET /en/rest`, `/en/graphql-lab`, `/en/sse`, `/en/websocket` and matching translated paths |
| Static assets | `GET /static/style.css` and embedded Molejo branding |
| Liveness | `GET /healthz` |
| Readiness | `GET /readyz` |
| Unready response | `GET /not-ready` |
| REST status | `GET /api/status` |
| REST collection | `GET /api/items` |
| REST echo | `POST /api/echo` |
| GraphQL | `POST /graphql` |
| Server-Sent Events | `GET /events` |
| WebSocket echo and broadcast | `GET /ws` |
| HTTP/HTTPS network probe | `testkit probe URL` |
| Configured peer identity and state | `GET /api/identity`, `GET /api/peers` |
| Optional persistent marker metadata | `GET`, `PUT /api/persistence` |

The build version stored in the tracked `VERSION` file is embedded in the Go
binary and exposed in protocol payloads, structured logs, the browser header and
footer, and the `Testkit-Version` header on every HTTP response, including
successful WebSocket handshakes. The footer also includes a small localized
Molejo attribution link with build-version UTM tracking. This makes rollout and
transport tests observable without requiring external build arguments or
changing the logical identity of the workload.

## Prerequisites

- Go 1.26.x.
- Docker Engine or Docker Desktop with Buildx enabled.
- `curl` for the examples.

## Build and run locally

Build the image for the local Docker platform:

```sh
docker buildx build \
  --load \
  --tag molejo-testkit:dev \
  .
```

Run it using the restricted container contract expected by Molejo Platform:

```sh
docker run --rm \
  --publish 8080:8080 \
  --read-only \
  --cap-drop ALL \
  --security-opt no-new-privileges \
  molejo-testkit:dev
```

Verify the REST contract:

```sh
curl --fail http://localhost:8080/api/status
```

Expected response:

```json
{"status":"ok","version":"v0.8.0"}
```

To run the server on another port, set `HTTP_PORT`. It defaults to `8080`:

```sh
HTTP_PORT=2020 go run .
curl --fail http://localhost:2020/api/status
```

For the container, configure the same port inside and outside the container:

```sh
docker run --rm \
  --env HTTP_PORT=2020 \
  --publish 2020:2020 \
  --read-only \
  --cap-drop ALL \
  --security-opt no-new-privileges \
  molejo-testkit:dev
```

## Quick smoke test

After starting the server, check readiness and then exercise the minimal
WebSocket exchange:

If `HTTP_PORT` is configured, replace `8080` with its value in both URLs.

```sh
curl --fail http://localhost:8080/readyz
wscat --connect ws://localhost:8080/ws
> {"message":"smoke"}
```

The readiness response, successful WebSocket handshake, and returned JSON
message confirm the basic reachability and transport path. They do not validate
application-specific WebSocket features.

## Browser console

Open `http://localhost:8080/` to let the server detect the browser language, or
use a localized URL directly: `/en/`, `/pt-BR/` or `/es-AR/`. The browser labs
are available at the matching `/rest`, `/graphql-lab`, `/sse` and `/websocket`
paths. The labs are small, deterministic connectivity and contract checks, not
feature suites. REST and GraphQL use guided presets that render request and
response details. The SSE lab displays event IDs, names and data, with explicit
connect, disconnect and reconnect actions. The WebSocket lab shows two
independent same-origin clients, `Client A` and `Client B`, so you can observe a
minimal message exchange and its broadcast signal. Browser clients do not
reconnect automatically after an error or close.

## Minimal protocol examples

REST echo:

```sh
curl --json '{"hello":"world"}' http://localhost:8080/api/echo
```

GraphQL:

```sh
curl --json '{"query":"{ status version echo(message: \"hello\") }"}' \
  http://localhost:8080/graphql
```

SSE:

```sh
curl -N http://localhost:8080/events
```

WebSocket:

```sh
wscat --connect ws://localhost:8080/ws
> {"message":"hello"}
```

The successful handshake and returned message are the WebSocket smoke signal.
The fixture broadcasts the same deterministic message to connected clients so
the transport result is visible; this is not a test of rooms, authentication,
or application-level routing.

```json
{"message":"hello","version":"v0.8.0"}
```

## Network probe

Run network diagnostics as an explicit command of the same image:

```sh
docker run --rm molejo-testkit:dev probe https://example.com/
```

The probe performs one bounded HTTP or HTTPS `GET` request and emits a JSON result.
It is separate from the server's incoming transport smoke tests: it validates
explicit outbound HTTP/HTTPS reachability, not WebSocket connectivity.
Its exit codes form the automation contract:

| Exit code | Meaning |
| --- | --- |
| `0` | The destination returned HTTP `2xx`. |
| `1` | The request failed or returned a non-`2xx` response. |
| `2` | The command arguments or URL are invalid. |

To verify Kubernetes NetworkPolicies, run the probe as a short-lived Job in the
namespace under test. Apply the labels and ServiceAccount whose network identity
you want to validate, then assert the Job exit code. This keeps the source,
destination, and expected allow-or-deny result explicit.

## Configured peer monitoring

Server mode can continuously verify a fixed allowlist of other Testkit
instances. The feature is disabled unless `TESTKIT_PEERS_FILE` points to a
read-only JSON file:

Peer monitoring validates transport reachability and the configured Testkit
identity endpoint. It does not validate application compatibility or business
features between peers.

```json
{
  "schema_version": 1,
  "instance_id": "testkit-a",
  "check_interval": "30s",
  "timeout": "3s",
  "peers": [
    {
      "name": "testkit-b",
      "scheme": "http",
      "host": "testkit-b.namespace-b",
      "port": 8080,
      "expected_instance_id": "testkit-b"
    }
  ]
}
```

Each check opens a fresh direct connection, ignores HTTP proxy environment
variables, does not follow redirects, and requests the fixed `/api/identity`
path. HTTPS uses the system trust store without an insecure mode. Responses are
limited to 4 KiB and at most four peers are checked concurrently. The first
check is immediate and later checks use `check_interval`; `timeout` must be
positive, no greater than 30 seconds, and shorter than that interval.

`GET /api/identity` returns the configured logical identity and a process-local
UUIDv7 `boot_id`. `GET /api/peers` returns only sanitized in-memory facts about
the latest checks. Both endpoints exist only when the peer file is configured.
An instance with no outbound peers can use `"peers": []` to serve its identity.
Neither endpoint accepts a destination or starts an on-demand check, and both
responses use `Cache-Control: no-store`.

The outcomes are `reachable`, `unreachable`, and `unknown`. Reasons distinguish
DNS, connection, TLS, HTTP, response, and identity failures. These are observed
transport facts: Testkit never claims that a failure was caused by a
NetworkPolicy. A Service with multiple replicas proves reachability to the
Service, not to one specific Pod.

## Container contract

- Listens on TCP port `8080` by default; `HTTP_PORT` can override it at runtime.
- Runs as UID/GID `65532:65532`.
- Supports a read-only root filesystem.
- Requires no Linux capabilities or privilege escalation.
- Includes a CA bundle for HTTPS probes.
- Embeds all static assets in the binary.
- Builds reproducibly for BuildKit target platforms, including `linux/amd64` and
  `linux/arm64`.
- Handles `SIGTERM` and drains HTTP, SSE, and WebSocket connections with a bounded
  shutdown.

The HTTP server accepts same-origin WebSocket connections and clients that omit
the `Origin` header, such as command-line test clients. Cross-origin browser
connections are rejected.

## Configuration

| Variable | Default | Purpose |
| --- | --- | --- |
| `HTTP_PORT` | `8080` | TCP port used by the HTTP server; must be an integer from `1` to `65535`. |
| `SSE_INTERVAL` | `1s` | Interval between SSE status events. |
| `TESTKIT_PEERS_FILE` | unset | Read-only peer configuration; unset disables peer monitoring and its HTTP endpoints. |
| `TESTKIT_PERSISTENCE_FILE` | unset | Absolute marker file path; unset disables the persistence endpoint. |
| `TESTKIT_SMOKES` | `transport` | Comma-separated capabilities. The only additional value is `postgres`; unknown values fail startup. |
| `TESTKIT_DIAGNOSTIC_TOKEN_FILE` | unset | Read-only operational token file required when `postgres` is enabled. |
| `TESTKIT_POSTGRES_DESTINATIONS_FILE` | unset | Read-only destination policy required when `postgres` is enabled. |

Unset or empty `HTTP_PORT` uses the default. Invalid values make the server fail
at startup. Invalid or non-positive `SSE_INTERVAL` values fall back to the default.
When persistence is enabled, `PUT /api/persistence` accepts `{"value":"..."}`
with 1–4096 bytes and writes it atomically. `GET` and `PUT` return only whether
the marker exists, its byte size, and its SHA-256 fingerprint; the marker value
is never returned. Mount a writable persistent volume at the configured file's
parent directory when using a read-only root filesystem.

PostgreSQL diagnostics are opt-in. Enable them with
`TESTKIT_SMOKES=transport,postgres` and mount both required files. The policy is
strict JSON, for example:

```json
{"destinations":[{"host":"db.internal.example","ports":[5432]},{"cidr":"10.20.0.0/24","ports":[5432]}]}
```

The browser sends the deployment token in `Authorization: Bearer ...`; this API
token is separate from the PostgreSQL credential. The structured connection
contract separates `target`, optional `database`, `identity`, `credential`,
`tls_config`, and `lifecycle`. Credential types are `password`, `token`, and
`none`; the first two require `secret`, while `none` forbids it. When `database`
is omitted, PostgreSQL selects the database according to its own startup rules.
The capabilities endpoint publishes these enums and defaults for the browser.

The API exposes only `connect`, `arithmetic_check`, `list_databases`, and
`list_schemas`; it accepts no SQL. Ephemeral operations open and close one
connection. Retained connections are created under
`/api/diagnostics/postgres/connections`, kept only in process memory, serialized
per connection, and explicitly destroyed or closed during shutdown. There are at
most eight retained connections and four concurrent diagnostic requests per
process. Every request has a 10-second total deadline and no retry. URI and
structured modes are mutually exclusive. TLS defaults to `verify-full`;
disabling it is explicit.
The API rejects unknown URI parameters, loopback, link-local (including
cloud/container metadata), multicast, and unspecified addresses before
connecting. Private destinations remain available only when the deployment
policy explicitly allows them. Serve this page exclusively over HTTPS.

## Structured logs

Server mode writes newline-delimited JSON logs to standard output. Every record
includes `service` and `version`. The primary `event` values are:

- `server.started`, `server.stopped`, and server failure events;
- `http.request.completed` for `/api/status`, `/api/items`, and `/api/echo`, with
  method, stable route, status code, duration in milliseconds, and response bytes;
- `connection.opened` and `connection.closed` for WebSocket and SSE, with the
  active protocol and total connection counts and connection duration on close;
- `connections.snapshot` every 15 minutes while at least one connection is active.
- `peer.identity.requested` for configured peer identity requests;
- `peer.state.changed` when a configured peer outcome, reason, or remote
  `boot_id` changes;
- `peers.snapshot` every 15 minutes while at least one peer is configured.

Connection lifecycle events and snapshots include `connection_sequence`, which
increases with every connection state transition in the process. Consumers can
use it to reconstruct transition order when concurrent log records arrive out of
order.

Completed REST requests and accepted WebSocket and SSE connections include a
UUIDv7 `correlation_id`. REST clients may provide it through
`X-Testkit-Correlation-ID`; WebSocket and SSE clients may use the
`correlation_id` query parameter. Missing or invalid values are replaced with a
server-generated ID. REST responses return the effective ID in the same header.
The bundled REST, WebSocket, and SSE labs generate and display these IDs
automatically. Connection snapshots remain aggregate and do not include
correlation IDs.

Connection counts represent accepted connections currently observed by one
server process and reset on restart. Page refreshes, normal closes, and transport
errors decrement the count when the server observes the disconnect. WebSocket
heartbeats bound silent failure detection to about 60 seconds; SSE detection is
best effort when a network path disappears without closing the HTTP stream.
Graceful shutdown waits for accepted WebSocket handlers to emit their closing
facts. A crash or forced process termination cannot emit closing facts, so
consumers must treat each server start as a new process-local epoch.

Logs do not include client addresses, raw headers, raw query strings, request
payloads, WebSocket messages, or SSE data. They are observable test facts, not
durable or global metrics.

Peer facts use logical peer names and stable reasons. They do not log configured
hosts, resolved IP addresses, proxy settings, response bodies, or raw errors.
Peer state resets when the process restarts; `boot_id` identifies the local
epoch and `observed_boot_id` identifies the latest remote epoch. Checks canceled
by server shutdown do not replace the last observed peer state.

## Image distribution

Tagged releases publish multi-platform images to GitHub Container Registry:

```text
ghcr.io/molejo-platform/testkit:v0.8.0
```

Tags are provided for discovery. Automated tests should consume the immutable
digest reported by the release workflow:

```text
ghcr.io/molejo-platform/testkit@sha256:<digest>
```

The project does not publish a `latest` tag. Signing, SBOMs, and additional
provenance attestations are outside the current release contract.

## Development

Run the essential local gate, which needs no Docker or external service:

```sh
npm ci
make test-local
```

Use `make test-browser` for browser contracts, `make test-postgres` for a
disposable real PostgreSQL 18 instance, and `make test-full` for all three
layers. The PostgreSQL integration tests carry the `integration` build tag and
fail, rather than skip, when their DSN is missing.

Build and exercise the final container whenever runtime, embedded assets, probes,
or the Dockerfile changes.

## Documentation

English is the canonical documentation language. Available translations:

- [Português (Brasil)](docs/pt-BR/README.md)
- [Español (Argentina)](docs/es-AR/README.md)

Translations preserve commands, paths, endpoint names, fields, and protocol
identifiers in English. If translated content diverges, the English version
defines the current contract.

The visual identity rules and asset provenance are documented in the
[Molejo Testkit branding contract](docs/BRANDING.md).

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) before proposing a change.

## Security

Do not report vulnerabilities through public issues. Follow
[SECURITY.md](SECURITY.md).

## License

Licensed under the [Apache License 2.0](LICENSE).
