# Contributing to Molejo Testkit

[Português (Brasil)](docs/pt-BR/CONTRIBUTING.md) |
[Español (Argentina)](docs/es-AR/CONTRIBUTING.md)

Thank you for helping improve Molejo Testkit. The project is experimental and has
published pre-releases, so changes should remain small, reproducible, and directly
connected to a testing need.

## Before starting

- Check existing issues and pull requests before duplicating work.
- Open an issue for changes that introduce a protocol, dependency, public
  endpoint, or incompatible behavior.
- Report suspected vulnerabilities privately according to
  [SECURITY.md](SECURITY.md).

## Development environment

You need:

- Go 1.26.x;
- Docker with Buildx for container validation;
- Git and common command-line tools.

Clone the repository and run the baseline tests before modifying it:

```sh
git clone https://github.com/molejo-platform/testkit.git
cd testkit
go test -race -cover ./...
go vet ./...
go mod tidy -diff
```

## Making changes

- Keep the implementation deterministic and suitable for disposable tests.
- Avoid abstractions or dependencies without a demonstrated testing requirement.
- Treat endpoints, payloads, exit codes, and container behavior as contracts.
- Keep one-off network probes as explicit commands and periodic peers in a
  read-only allowlist. Never let a public HTTP request select or trigger an
  outbound destination.
- Preserve the restricted runtime: non-root, read-only filesystem compatible,
  no required capabilities, and bounded shutdown.
- Add a regression test before fixing a defect.
- Update English documentation and corresponding translations when behavior
  changes.
- Use the official logo and semantic color tokens described in
  [docs/BRANDING.md](docs/BRANDING.md) for browser interface changes.

Format and verify the code:

```sh
gofmt -w *.go
go test -race -cover ./...
go vet ./...
go mod tidy -diff
```

When the container contract changes, also build and run the image using the
restricted flags documented in [README.md](README.md).

## Commit messages

Use [Conventional Commits](https://www.conventionalcommits.org/) with an English
summary. Add a body when a commit changes more than three files or when the reason
for the change is not evident from the summary.

Examples:

```text
feat(rest): add deterministic headers endpoint
fix(websocket): close clients during shutdown
docs: document Kubernetes probe usage
```

## Pull requests

A pull request should:

- explain the testing problem being solved;
- describe the observable contract change;
- include regression coverage;
- list the commands used for validation;
- avoid unrelated cleanup or generated noise;
- update documentation when applicable.

Maintainers may ask for a smaller change if unrelated behaviors are coupled.

## Contribution license

By contributing, you agree that your contributions are licensed under the
[Apache License 2.0](LICENSE), as described by section 5 of the license.
