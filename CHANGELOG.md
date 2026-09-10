# Changelog

All notable changes to Molejo Testkit are documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and versions follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

Candidate `0.9.0-rc.1` is prepared on its release branch for validation. It has
not been tagged or published, and the repository version remains at the latest
stable release until promotion.

### Added

- Added an opt-in PostgreSQL diagnostic laboratory with fixed read-only
  operations for connectivity, arithmetic, database listing, and schema
  listing.
- Added backend-controlled capability discovery, token-file authentication,
  and a read-only destination policy for diagnostic targets.
- Added separated PostgreSQL target, database, identity, credential, TLS, and
  lifecycle contracts, including process-local retained connections.
- Added local `test-local`, `test-browser`, `test-postgres`, and `test-full`
  gates; the PostgreSQL gate provisions and removes a disposable real server.
- Added browser-level Playwright coverage and a pull-request workflow covering
  Go, frontend, browser, and vulnerability checks.

### Changed

- REST and GraphQL laboratories now use explicit finite executions with
  deadlines, cancellation, duplicate-submit protection, and stale-response
  disposal.
- GraphQL presets now require explicit execution and distinguish expected
  GraphQL errors from transport failures.
- Split runtime configuration, REST, GraphQL, and shared HTTP helpers out of
  the main server module without changing the existing public routes.
- Expanded localized documentation and browser content for the PostgreSQL
  diagnostic workflow and its trust boundary.
- PostgreSQL now delegates database selection to the server when `database` is
  omitted and reports the effective database and backend process identifier.
- Reorganized the localized READMEs as consumer guides with image selection,
  runtime configuration, Docker and Kubernetes recipes, expected outcomes, and
  troubleshooting; contributor setup and local gates now live in CONTRIBUTING.

### Fixed

- JSON request parsing now rejects trailing values before persistence or other
  observable effects occur.
- Persistence reads are bounded, and peer request timeouts use a neutral reason
  that does not misclassify the failure stage.

### Security

- Diagnostic destinations are re-resolved for every attempt, checked against
  the configured allowlist, rejected for special local/link addresses, and
  pinned to the validated IP while preserving TLS hostname verification.
- Arbitrary SQL, inherited PostgreSQL credential files, redirects, cross-origin
  browser requests, oversized inputs, secret echoing, and unbounded diagnostic
  concurrency are rejected.
- Added non-cacheable diagnostic responses, browser security headers, request
  rate limits, execution deadlines, result limits, and local source/binary
  vulnerability gates.

## [0.8.0] - 2026-09-10

### Added

- Added an optional persistent marker fixture, configured through
  `TESTKIT_PERSISTENCE_FILE`, for storage connectivity smoke tests.

### Changed

- Completed the Molejo rebrand across localized browser copy, the probe
  User-Agent, official logo asset, dark palette, and typography.
- Changed the Go module path from `github.com/fruto-platform/testkit` to
  `github.com/molejo-platform/testkit`; Go consumers must update their imports.
- Added the local branding contract so future browser changes preserve the
  approved Molejo assets and semantic color roles.
- The HTTP server keeps port `8080` as its default and now accepts a validated
  `HTTP_PORT` environment variable for local and containerized runs.
- Added a localized footer attribution link to `molejo.dev` with build-version
  UTM tracking and responsive link styling.
- Clarified in the documentation and localized browser copy that Testkit is a
  connectivity smoke-test fixture, not a feature-complete protocol suite.

## [0.7.1] - 2026-08-29

### Fixed

- Browser pages now reference embedded CSS and JavaScript through
  content-addressed paths, revalidate HTML, and prevent legacy asset responses
  from being cached, avoiding mismatched frontend code across releases.

## [0.7.0] - 2026-08-29

### Changed

- The REST browser lab now separates preset selection from request execution,
  provides an explicit send action with duplicate-submit protection, and shows
  response duration, size, content type, version, and correlation ID.

### Fixed

- Local request setup failures now restore the REST lab controls and allow a
  retry without reloading the page.

## [0.6.1] - 2026-08-29

### Fixed

- The binary now embeds its version from the tracked `VERSION` file, so plain
  Go and Docker builds no longer report `devel` when external build arguments
  are unavailable.

### Changed

- Release validation now requires the Git tag, `VERSION`, and `package.json` to
  describe the same release without passing version metadata into Docker.

## [0.6.0] - 2026-08-29

### Added

- `Testkit-Version` on every HTTP response, including errors, redirects, static
  assets, SSE streams, and successful WebSocket handshakes.

### Changed

- Browser page footers now display the same build version already used by
  protocol payloads and structured logs.

## [0.5.0] - 2026-08-29

### Added

- Opt-in, versioned peer monitoring for fixed HTTP/HTTPS destinations, with
  local and observed remote process identities, immediate and periodic
  fresh-connection checks, sanitized non-cacheable in-memory state, bounded
  timeouts and concurrency, and correlated structured logs.
- Security and lifecycle coverage for peer configuration, redirects, proxy
  bypass, TLS validation, response limits, identity matching, state changes,
  remote restarts, concurrency, and shutdown cancellation.

### Security

- Peer destinations can only come from the read-only configuration file; public
  HTTP requests cannot provide or trigger arbitrary outbound destinations.

## [0.4.0] - 2026-08-28

### Added

- UUIDv7 correlation IDs for completed REST requests and accepted WebSocket and
  SSE connections, with validated client propagation, safe server fallback, and
  automatic generation and display in the bundled browser labs.

### Changed

- Clarified that active connection facts are process-local, how client
  disconnects are detected, and the limits of silent SSE failure detection.

### Fixed

- Graceful shutdown now waits for accepted WebSocket handlers to emit their
  closing facts before the server exits.

## [0.3.0] - 2026-08-28

### Added

- Structured JSON logs for server lifecycle, REST request completion, and
  WebSocket and SSE connection lifecycle.
- Per-process active connection snapshots every 15 minutes while at least one
  WebSocket or SSE connection is active.
- Monotonic per-process connection sequences so concurrent lifecycle facts can
  be reconstructed in transition order.

## [0.2.0] - 2026-08-24

### Added

- Localized REST, GraphQL, and Server-Sent Events browser laboratories.
- Guided REST presets for status, items, echo, and error responses.
- Guided GraphQL presets for status/version, variables, and invalid queries.
- Explicit SSE connect, disconnect, and reconnect controls with event details.
- Browser regression coverage for the new laboratories and localized routes.

### Changed

- The home protocol map now links to active REST, GraphQL, SSE, and WebSocket
  laboratories.
- README documentation now includes the localized browser laboratory aliases.

## [0.1.1] - 2026-08-23

### Fixed

- Updated the release pipeline to use the Node.js 24-compatible `setup-node` action runtime.

## [0.1.0] - 2026-08-23

### Added

- Localized browser pages for English, Brazilian Portuguese, and Argentine Spanish.
- Automatic locale detection using query parameter, cookie, and `Accept-Language`.
- JSON translation catalogs embedded in the Go binary.
- Localized WebSocket dashboard routes with language switcher and breadcrumbs.
- Chat and JSON WebSocket views with localized relative timestamps.
- Frontend test execution in the release workflow.

### Changed

- `/` and `/websocket` now redirect to the detected localized route.
- WebSocket broadcasts with identical payloads remain visible as separate received events.
- Browser interface text is centralized in locale catalogs, including the brand name.

## [0.0.2] - 2026-08-23

### Added

- Browser WebSocket console with two independent clients.
- Dedicated WebSocket page with JSON and chat-oriented event views.
- Local send and receive timestamps for browser-side WebSocket events.

## [0.0.1] - 2026-08-23

### Added

- Initial multi-platform image publication workflow for GitHub Container Registry.

[Unreleased]: https://github.com/molejo-platform/testkit/compare/v0.8.0...HEAD
[0.8.0]: https://github.com/molejo-platform/testkit/compare/v0.7.1...v0.8.0
[0.7.1]: https://github.com/molejo-platform/testkit/compare/v0.7.0...v0.7.1
[0.7.0]: https://github.com/molejo-platform/testkit/compare/v0.6.1...v0.7.0
[0.6.1]: https://github.com/molejo-platform/testkit/compare/v0.6.0...v0.6.1
[0.6.0]: https://github.com/molejo-platform/testkit/compare/v0.5.0...v0.6.0
[0.5.0]: https://github.com/molejo-platform/testkit/compare/v0.4.0...v0.5.0
[0.4.0]: https://github.com/molejo-platform/testkit/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/molejo-platform/testkit/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/molejo-platform/testkit/compare/v0.1.1...v0.2.0
[0.1.1]: https://github.com/molejo-platform/testkit/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/molejo-platform/testkit/compare/v0.0.2...v0.1.0
[0.0.2]: https://github.com/molejo-platform/testkit/compare/v0.0.1...v0.0.2
[0.0.1]: https://github.com/molejo-platform/testkit/releases/tag/v0.0.1
