#!/bin/sh
set -eu

container_name="testkit-postgres-$PPID"
cleanup() {
	docker rm -f "$container_name" >/dev/null 2>&1 || true
}
trap cleanup EXIT INT TERM

docker run --detach --rm \
	--name "$container_name" \
	--publish 127.0.0.1::5432 \
	--env POSTGRES_USER=testkit \
	--env POSTGRES_PASSWORD=testkit \
	--env POSTGRES_DB=testkit \
	postgres:18-alpine >/dev/null

attempt=0
until docker exec "$container_name" pg_isready --username testkit --dbname testkit >/dev/null 2>&1; do
	attempt=$((attempt + 1))
	if [ "$attempt" -ge 30 ]; then
		echo "PostgreSQL did not become ready" >&2
		exit 1
	fi
	sleep 1
done

host_port=$(docker port "$container_name" 5432/tcp | sed 's/.*://')
TESTKIT_POSTGRES_INTEGRATION_DSN="postgresql://testkit:testkit@127.0.0.1:${host_port}/testkit?sslmode=disable" \
	go test -race -cover -tags=integration ./...
