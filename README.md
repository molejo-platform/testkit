# Molejo Testkit

[Português (Brasil)](docs/pt-BR/README.md) |
[Español (Argentina)](docs/es-AR/README.md)

> Experimental pre-release project. Molejo Testkit is a disposable test fixture,
> not a production application.

Molejo Testkit is a small container image for proving network reachability,
application delivery, and minimal transport contracts. Run it beside the system
you want to inspect, exercise one of its fixed interfaces, and assert the
observable response.

A single non-root Go binary provides health checks, REST, GraphQL, Server-Sent
Events (SSE), WebSocket, persistent-volume checks, configured peer monitoring,
bounded PostgreSQL diagnostics, and an explicit outbound HTTP/HTTPS probe.

## Choose the image

Published images use this repository:

```text
ghcr.io/molejo-platform/testkit
```

Set the image once before following the examples:

```sh
export TESTKIT_IMAGE=ghcr.io/molejo-platform/testkit:v0.10.0
docker pull "$TESTKIT_IMAGE"
```

`v0.10.0` is the latest published stable image at the time this guide was
written. A release branch may document unreleased capabilities before a matching
image exists. For automated environments, replace the tag with the immutable
digest published by the release:

```sh
export TESTKIT_IMAGE=ghcr.io/molejo-platform/testkit@sha256:PASTE_PUBLISHED_DIGEST_HERE
```

The project does not publish a `latest` tag. Do not assume that the repository
default branch, this README, and an older image tag expose the same capabilities.

## Quick start: incoming transport

### 1. Start the container

The default configuration enables transport smokes on port `8080` and needs no
configuration file:

```sh
docker run --rm \
  --name molejo-testkit \
  --publish 8080:8080 \
  --read-only \
  --cap-drop ALL \
  --security-opt no-new-privileges \
  "$TESTKIT_IMAGE"
```

Expected outcome: the process logs `server.started` and continues running as
UID/GID `65532:65532`.

### 2. Check liveness and readiness

In another terminal:

```sh
curl --fail http://localhost:8080/healthz
curl --fail http://localhost:8080/readyz
curl --fail http://localhost:8080/api/status
```

The status response includes the exact version embedded in the image:

```json
{"status":"ok","version":"v0.10.0"}
```

### 3. Open the browser console

Open `http://localhost:8080/`. Testkit detects the browser language and redirects
to `/en/`, `/pt-BR/`, or `/es-AR/`. The REST, GraphQL, SSE, and WebSocket pages
execute guided, bounded scenarios and show the observed request and response.

### 4. Stop it

Press `Ctrl+C` in the container terminal. Testkit handles `SIGTERM`, stops
accepting work, closes retained resources, and drains HTTP and WebSocket work
within its shutdown boundary.

## What a successful smoke proves

Testkit answers a focused question: can this client reach this disposable
workload or explicitly configured dependency, and can the selected minimal
exchange complete?

| Surface | What success proves |
| --- | --- |
| `/healthz`, `/readyz` | HTTP path reaches the running Testkit process. |
| REST/GraphQL | Request, response, headers, and a bounded application exchange complete. |
| SSE | A streaming HTTP response delivers named events. |
| WebSocket | Upgrade, bidirectional frames, broadcast, and close handling complete. |
| `probe URL` | One explicit outbound HTTP/HTTPS request returns `2xx`. |
| `/api/peers` | A configured Testkit identity was reached from this process. |
| `/api/persistence` | The configured filesystem path can persist and read a bounded marker. |
| PostgreSQL lab | An allowed database destination accepts a fixed connection or catalog operation. |

Success does not validate business rules, production authentication,
authorization, rooms, application-specific schemas, performance, load,
high availability, backup, or unrelated dependencies.

## Available interfaces

| Capability | Interface |
| --- | --- |
| Browser console | `GET /`, `/en/`, `/pt-BR/`, `/es-AR/` |
| Browser aliases | `GET /rest`, `GET /graphql-lab`, `GET /sse`, `GET /websocket` |
| Browser labs | `GET /<locale>/rest`, `/graphql-lab`, `/sse`, `/websocket` |
| Liveness and readiness | `GET /healthz`, `GET /readyz` |
| Intentional unready response | `GET /not-ready` |
| REST | `GET /api/status`, `GET /api/items`, `POST /api/echo` |
| GraphQL | `POST /graphql` |
| SSE | `GET /events` |
| WebSocket | `GET /ws` |
| Explicit outbound probe | `testkit probe URL` |
| Configured peer identity | `GET /api/identity`, `GET /api/peers` |
| Persistent marker | `GET`, `PUT /api/persistence` |
| PostgreSQL capabilities | `GET /api/diagnostics/postgres/capabilities` |
| Ephemeral PostgreSQL operation | `POST /api/diagnostics/postgres` |
| Retained PostgreSQL connection | `POST /api/diagnostics/postgres/connections` |

Optional interfaces exist only when their corresponding configuration is valid.
Unknown paths and methods are not treated as successful smoke results.

## Common protocol checks

REST echo:

```sh
curl --fail --json '{"hello":"world"}' \
  http://localhost:8080/api/echo
```

GraphQL:

```sh
curl --fail --json '{"query":"{ status version echo(message: \"hello\") }"}' \
  http://localhost:8080/graphql
```

SSE:

```sh
curl --no-buffer http://localhost:8080/events
```

WebSocket with `wscat`:

```sh
wscat --connect ws://localhost:8080/ws
> {"message":"hello"}
```

The WebSocket response includes the message and image version. It is a transport
signal, not an implementation of rooms or application routing.

## Explicit outbound probe

The probe command is separate from server mode. It performs one bounded `GET`
without turning a public HTTP endpoint into a general-purpose proxy:

```sh
docker run --rm "$TESTKIT_IMAGE" probe https://example.com/
```

It writes one JSON result and uses these automation-safe exit codes:

| Exit code | Meaning |
| --- | --- |
| `0` | Destination returned HTTP `2xx`. |
| `1` | Network/TLS request failed or response was not `2xx`. |
| `2` | Command arguments or URL are invalid. |

For Kubernetes NetworkPolicy validation, run the image as a short-lived Job in
the source namespace with the labels and ServiceAccount whose network identity
you need to test. Assert the Job exit code and delete the Job afterwards.

## Runtime configuration

All features are configured at process startup. Invalid required values fail the
process instead of silently disabling a requested capability.

| Variable | Default | Required when | Purpose |
| --- | --- | --- | --- |
| `HTTP_PORT` | `8080` | Never | Internal listen port, from `1` to `65535`. |
| `SSE_INTERVAL` | `1s` | Never | Positive Go duration between SSE events. |
| `TESTKIT_SMOKES` | `transport` | Never | Comma-separated capabilities: `transport` or `transport,postgres`. |
| `TESTKIT_PEERS_FILE` | unset | Peer monitoring | Absolute path to the peer JSON file. |
| `TESTKIT_PERSISTENCE_FILE` | unset | Persistent marker | Absolute writable marker file path. |
| `TESTKIT_DIAGNOSTIC_TOKEN_FILE` | unset | PostgreSQL | Absolute path to a read-only token file of at least 16 bytes. |
| `TESTKIT_POSTGRES_DESTINATIONS_FILE` | unset | PostgreSQL | Absolute path to the strict destination-policy JSON file. |

Configuration files are read from the container filesystem. Mount them at an
absolute path and use the same in the environment variable. Diagnostic and peer
files should be mounted read-only. The parent directory of the persistence file
must be writable by UID/GID `65532:65532`.

### Custom HTTP port and SSE interval

Configure the same port inside the container and on the published host mapping:

```sh
docker run --rm \
  --name molejo-testkit \
  --env HTTP_PORT=2020 \
  --env SSE_INTERVAL=2s \
  --publish 2020:2020 \
  --read-only \
  --cap-drop ALL \
  --security-opt no-new-privileges \
  "$TESTKIT_IMAGE"
```

Expected URL: `http://localhost:2020/readyz`.

## Recipe: validate writable persistence

`TESTKIT_PERSISTENCE_FILE` enables a small marker contract. Testkit stores at
most 4096 bytes and returns only existence, size, and SHA-256; it never returns
the marker value.

First create a writable Docker volume owned by the Testkit runtime user:

```sh
docker volume create molejo-testkit-data
docker run --rm \
  --user 0:0 \
  --mount type=volume,source=molejo-testkit-data,target=/data \
  alpine:3.22 chown 65532:65532 /data
```

Then start Testkit:

```sh
docker run --rm \
  --name molejo-testkit \
  --publish 8080:8080 \
  --env TESTKIT_PERSISTENCE_FILE=/var/lib/testkit/marker \
  --mount type=volume,source=molejo-testkit-data,target=/var/lib/testkit \
  --read-only \
  --cap-drop ALL \
  --security-opt no-new-privileges \
  "$TESTKIT_IMAGE"
```

Write and read the marker metadata:

```sh
curl --fail --request PUT \
  --header 'Content-Type: application/json' \
  --data '{"value":"volume-smoke"}' \
  http://localhost:8080/api/persistence

curl --fail http://localhost:8080/api/persistence
```

Expected outcome: `exists` is `true`, `size` is non-zero, and `sha256` remains
the same after restarting the Testkit container with the same volume.

## Recipe: monitor configured Testkit peers

Peer monitoring is a periodic, read-only check between Testkit instances. Every
instance that serves `/api/identity` needs a peer file; an instance that only
receives checks uses an empty `peers` array.

Example `/config/peers.json`:

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

Mount it and set its absolute container path:

```sh
docker run --rm \
  --name testkit-a \
  --env TESTKIT_PEERS_FILE=/etc/testkit/peers.json \
  --mount type=bind,source="$PWD/config/peers.json",target=/etc/testkit/peers.json,readonly \
  --publish 8080:8080 \
  --read-only \
  --cap-drop ALL \
  --security-opt no-new-privileges \
  "$TESTKIT_IMAGE"
```

Inspect the sanitized, process-local state:

```sh
curl --fail http://localhost:8080/api/identity
curl --fail http://localhost:8080/api/peers
```

The first check runs immediately. Later checks use `check_interval`. Testkit does
not follow redirects or HTTP proxy variables, and HTTPS uses the system trust
store without an insecure mode.

## Recipe: run PostgreSQL diagnostics

PostgreSQL diagnostics are opt-in and require two files: an operational API token
and an allowlist of destinations. The deployment token is separate from the
database credential entered for a diagnostic.

This capability is included in `v0.9.0` and remains disabled until the required
token and destination policy are configured explicitly.

### 1. Create local configuration files

Use a disposable token for this example and replace the destination with the
hostname or CIDR Testkit must be allowed to reach:

```sh
mkdir -p config
printf '%s\n' 'replace-with-at-least-16-bytes' > config/diagnostic-token
printf '%s\n' \
  '{"destinations":[{"host":"db.internal.example","ports":[5432]}]}' \
  > config/postgres-destinations.json
chmod 0444 config/diagnostic-token config/postgres-destinations.json
```

Do not commit the token. In shared environments, create the file through the
platform secret manager rather than storing it beside deployment manifests.

### 2. Start Testkit with PostgreSQL enabled

```sh
docker run --rm \
  --name molejo-testkit \
  --publish 8080:8080 \
  --env TESTKIT_SMOKES=transport,postgres \
  --env TESTKIT_DIAGNOSTIC_TOKEN_FILE=/run/testkit/diagnostic-token \
  --env TESTKIT_POSTGRES_DESTINATIONS_FILE=/etc/testkit/postgres-destinations.json \
  --mount type=bind,source="$PWD/config/diagnostic-token",target=/run/testkit/diagnostic-token,readonly \
  --mount type=bind,source="$PWD/config/postgres-destinations.json",target=/etc/testkit/postgres-destinations.json,readonly \
  --read-only \
  --cap-drop ALL \
  --security-opt no-new-privileges \
  "$TESTKIT_IMAGE"
```

Expected outcome: `http://localhost:8080/en/postgres` exists and
`/api/diagnostics/postgres/capabilities` describes the supported fields,
credential types, TLS modes, lifecycles, and fixed operations.

### 3. Run a diagnostic

The browser page is the simplest client. Enter the deployment token, target,
database identity, credential, TLS choice, and lifecycle. The initial database is
optional; when omitted, PostgreSQL selects it using its startup rules.

| Connection part | Contract |
| --- | --- |
| `target` | Required host; port defaults to `5432`. |
| `database` | Optional. Omission delegates initial database selection to PostgreSQL. |
| `identity` | Required database user. |
| `credential` | Required type: `password`, `token`, or `none`. Password/token require `secret`; none forbids it. |
| `tls_config` | Defaults to `verify-full`; optional private CA applies only to verified TLS. |
| `lifecycle` | `ephemeral` closes after one operation; `retained` stays in process memory until deleted or shutdown. |

`token` means a provider-generated token supplied through PostgreSQL's password
field; Testkit does not generate IAM, OAuth, or cloud-provider credentials. A URI
is also accepted as an alternative to the structured target/identity fields, but
the two modes cannot be combined.

For automation, run one ephemeral operation directly:

```sh
curl --fail --json '{
  "operation": "connect",
  "connection": {
    "target": {"host": "db.internal.example", "port": 5432},
    "identity": {"user": "testkit"},
    "credential": {"type": "password", "secret": "replace-me"},
    "tls_config": {"mode": "verify-full"},
    "lifecycle": {"mode": "ephemeral"}
  }
}' \
  --header 'Authorization: Bearer replace-with-at-least-16-bytes' \
  http://localhost:8080/api/diagnostics/postgres
```

Available operations are `connect`, `arithmetic_check`, `list_databases`, and
`list_schemas`. Arbitrary SQL is not accepted.

The browser reports both total duration and diagnostic duration. Total duration
is measured in the browser and includes the HTTP exchange and response reading.
The response `duration_ms` is measured by the server around the diagnostic; for
an ephemeral connection it includes opening, checking, and closing the database
connection rather than only the fixed SQL operation.

All connection, operation, inspection, and deletion requests require the bearer
deployment token. The capabilities endpoint is read-only and does not. A handled
diagnostic can return HTTP `200` with `status: "failed"`; automation must assert
the JSON `status` and `code`, not only the HTTP status.

Use lifecycle `retained` with
`POST /api/diagnostics/postgres/connections` to keep one physical connection in
process memory. Run fixed operations at
`POST /api/diagnostics/postgres/connections/<id>/operations`, inspect it with
`GET /api/diagnostics/postgres/connections/<id>`, and always finish with
`DELETE /api/diagnostics/postgres/connections/<id>`.

The server limits retained connections to eight and concurrent diagnostic
requests to four. Operations have a 10-second deadline and no retry. Secrets are
not returned or logged. Loopback is allowed only when `localhost` or a literal
loopback IP appears as an exact `host` rule with the requested port; a CIDR rule
cannot allow it. Link-local, multicast, unspecified, and cloud metadata
addresses remain blocked. The policy is loaded once at startup, so restart
Testkit after editing its file.

## Kubernetes: minimal transport fixture

This example deploys the default transport-only image. Pin `image` to the digest
validated by your release process:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: molejo-testkit
spec:
  replicas: 1
  selector:
    matchLabels:
      app: molejo-testkit
  template:
    metadata:
      labels:
        app: molejo-testkit
    spec:
      securityContext:
        runAsNonRoot: true
        runAsUser: 65532
        runAsGroup: 65532
        seccompProfile:
          type: RuntimeDefault
      containers:
        - name: testkit
          image: ghcr.io/molejo-platform/testkit:v0.10.0
          ports:
            - name: http
              containerPort: 8080
          securityContext:
            allowPrivilegeEscalation: false
            readOnlyRootFilesystem: true
            capabilities:
              drop: ["ALL"]
          livenessProbe:
            httpGet:
              path: /healthz
              port: http
          readinessProbe:
            httpGet:
              path: /readyz
              port: http
---
apiVersion: v1
kind: Service
metadata:
  name: molejo-testkit
spec:
  selector:
    app: molejo-testkit
  ports:
    - name: http
      port: 8080
      targetPort: http
```

Use ConfigMaps for non-secret peer and destination policy files, Secrets for the
diagnostic token, and a writable volume plus `fsGroup: 65532` for persistence.

## Operational and security behavior

- The image requires no Linux capabilities or privilege escalation and supports
  a read-only root filesystem.
- All browser assets are embedded in the binary; no asset volume is required.
- Cross-origin browser requests are rejected for sensitive diagnostics.
- Public HTTP routes cannot select arbitrary outbound destinations.
- PostgreSQL and peer destinations are constrained before connecting.
- Logs exclude raw headers, query strings, payloads, credentials, WebSocket
  messages, and SSE data.
- Process-local peer facts, connection counts, boot IDs, and retained database
  connections reset when the process restarts.

The embedded version is exposed through responses, logs, browser pages, and the
`Testkit-Version` header. Use it to distinguish the image actually running from
the manifests or source checkout you expected to deploy.

## Troubleshooting startup

| Symptom | Check |
| --- | --- |
| Process exits immediately | Read the structured `server.configuration_failed` log. |
| Published port is unreachable | Ensure host and container ports match `HTTP_PORT`. |
| Optional endpoint returns `404` | Confirm its environment variable and mounted file were present at startup. |
| Configuration path is rejected | Paths inside the container must be absolute. |
| PostgreSQL target is forbidden | Confirm hostname/CIDR, port, DNS result, and special-address restrictions. |
| Persistence returns `500` | Confirm the parent directory exists and is writable by UID/GID `65532`. |
| Peer remains `unknown` | Check DNS, timeout, expected instance ID, and the remote peer file. |

## Contributing and development

This README is the consumer guide for running the image. Building the source,
running local quality gates, changing contracts, and preparing pull requests are
documented separately in [CONTRIBUTING.md](CONTRIBUTING.md).

Security issues must not be reported through public issues. Follow
[SECURITY.md](SECURITY.md). Visual identity and asset provenance are documented
in [docs/BRANDING.md](docs/BRANDING.md).

Licensed under the [Apache License 2.0](LICENSE).
