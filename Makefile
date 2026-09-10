.PHONY: test-local test-browser test-postgres test-full

test-local:
	test -z "$$(gofmt -l *.go)"
	go mod tidy -diff
	go vet ./...
	go test -race -cover ./...
	npm run test:frontend

test-browser:
	npm run test:browser

test-postgres:
	./scripts/test-postgres.sh

test-full: test-local test-postgres test-browser
