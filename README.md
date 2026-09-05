# hookline

Reliable webhook delivery as a service — sign it, retry it, and never lose it.

[![CI](https://github.com/Gustavo-Leite/hookline/actions/workflows/ci.yml/badge.svg)](https://github.com/Gustavo-Leite/hookline/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

> **Status: early development.** The service boots, connects to its dependencies
> and reports health. Event ingestion and delivery are not implemented yet — see
> [Roadmap](#roadmap) for exactly what exists and what does not.

## What it is

Sending a webhook is easy. *Guaranteeing* it arrives is not.

The receiver is down for six hours. The connection times out after the handler
already ran, so a retry would deliver twice. The receiver has no way to tell
your request apart from an attacker's. Every team that ships webhooks rebuilds
the same machinery to handle this, usually badly, usually inline with the
request that triggered the event.

hookline is that machinery, extracted: you register endpoints, you `POST` an
event, and the service owns delivery from there — signing each request,
retrying with exponential backoff, parking what it cannot deliver in a
dead-letter queue, and keeping a full attempt history you can inspect and
replay.

## Why it exists

This is a portfolio project, and it is deliberately not a CRUD app. It is a
small distributed system, so almost every part of it is a real design decision
with a real trade-off: at-least-once versus exactly-once, queue in Postgres
versus Redis, how long to keep retrying a dead endpoint, how to make an
ingestion endpoint idempotent. Those decisions are documented as they are made.

## Tech stack

| Layer | Choice |
|---|---|
| Language | Go 1.27 |
| HTTP | `net/http` (standard library routing) |
| Database | PostgreSQL 18 via `pgx` |
| Cache / rate limiting | Redis 8 |
| Migrations | goose (plain SQL) |
| Logging | `log/slog` (structured JSON) |
| Lint | golangci-lint v2 |
| CI | GitHub Actions |
| Local infra | Docker Compose |

## Getting started

### Prerequisites

- [Go 1.27+](https://go.dev/dl/)
- Docker and Docker Compose

Go and goose are needed only to work on the code — see
[Development](#development).

### Run it

```bash
git clone https://github.com/Gustavo-Leite/hookline.git
cd hookline

cp .env.example .env    # the defaults work as-is for local use
docker compose up -d --build
```

That is the whole setup. Compose starts Postgres and Redis, waits for both to
report healthy, applies the pending migrations in a one-shot container, and
only then starts the API:

```bash
curl -s localhost:8080/healthz    # {"status":"ok"}
curl -s localhost:8080/readyz     # {"postgres":"ok","redis":"ok"}
```

```bash
docker compose ps       # what is running
docker compose logs -f api
docker compose down     # stop everything, keep the data
docker compose down -v  # stop everything and wipe the database
```

### Configuration

Every variable lives in `.env.example`, ready to copy.

| Variable | Default | Description |
|---|---|---|
| `APP_ENV` | `development` | Environment name |
| `HTTP_PORT` | `8080` | Port the API listens on |
| `LOG_LEVEL` | `info` | Log verbosity |
| `POSTGRES_USER` / `POSTGRES_PASSWORD` / `POSTGRES_DB` | `hookline` | Credentials used by the Postgres container |
| `POSTGRES_PORT` | `5432` | Host port mapped to Postgres |
| `REDIS_PORT` | `6379` | Host port mapped to Redis |
| `DATABASE_URL` | `postgres://…@localhost:5432/…` | Connection string, for running the service on the host |
| `REDIS_URL` | `redis://localhost:6379/0` | Redis connection string, same |
| `GOOSE_DRIVER` / `GOOSE_DBSTRING` / `GOOSE_MIGRATION_DIR` | — | Read automatically by the goose CLI |

`DATABASE_URL` and `REDIS_URL` point at `localhost`, which is what a process on
your machine needs. Containers reach the same services by name — `postgres` and
`redis` — so the `api` service overrides both in `docker-compose.yml`. That way
one `.env` serves both ways of running it, with nothing to edit when you switch.

## Development

Running the service from source gives you fast rebuilds and a debugger, so keep
only its dependencies in containers:

```bash
docker compose up -d postgres redis migrate
go run ./cmd/api
```

`migrate` applies pending migrations and exits; you can also drive goose
directly, which reads its settings from `.env` and needs no arguments.

To watch readiness do its job, stop a dependency while the service is running:

```bash
docker compose stop redis
curl -i -s localhost:8080/readyz    # 503, redis "unavailable"
curl -i -s localhost:8080/healthz   # 200, the process is still alive
docker compose start redis
```

### Checks

```bash
go build ./...          # compile
go test -race ./...     # tests, with the race detector
go vet ./...            # standard static analysis
golangci-lint run       # full lint suite
gofmt -l .              # anything listed here is unformatted

goose status            # which migrations are pending
goose up                # apply them
goose down              # roll the last one back
```

CI runs the same checks on every push and pull request, plus a smoke test that
builds the images, brings the whole stack up with Docker Compose and asserts
that `/readyz` answers — so the quick start above cannot silently rot.

### Project layout

```
cmd/api/            entry point: wires dependencies and owns the server lifecycle
internal/config/    environment parsing, validated at boot
internal/httpapi/   HTTP handlers and middleware
internal/postgres/  connection pool and data access
internal/redis/     Redis client
migrations/         versioned SQL, applied with goose
```

`internal/` is enforced by the compiler: nothing outside this module can import
it. Packages are named after what they adapt, and interfaces are declared by
the code that consumes them rather than the code that implements them — so they
stay small and easy to fake in tests.

## Design decisions

Decisions worth defending, and what each one costs.

**UUIDv7 for primary keys.** Postgres 18 ships `uuidv7()` natively. UUIDv4 is
random, so every insert lands on a random B-tree page and the index fragments
under write load. UUIDv7 carries a timestamp prefix, so inserts are close to
sequential like a `bigserial` — without leaking row counts or requiring a round
trip to generate the key. The cost is 16 bytes per key instead of 8, and rows
leak their creation time to anyone holding an ID.

**Endpoint secrets are stored recoverable, not hashed.** API keys are hashed,
because verifying them only requires a comparison. Endpoint secrets cannot be:
computing the HMAC-SHA256 signature of every outgoing request needs the
original value, and hashing is one-way. They should therefore be encrypted at
rest with an application key — currently they are not, which is tracked in the
roadmap below. Pretending a hash would work here is the common mistake.

**Liveness and readiness are separate endpoints.** `/healthz` answers "is the
process alive?" and deliberately touches no dependency. `/readyz` answers "can
it serve traffic?" and pings Postgres and Redis under a 2s deadline. Collapsing
the two means a briefly flapping database gets your healthy process killed and
restarted by the orchestrator, which helps nobody.

**Readiness failures are logged in full and reported vaguely.** The response
says `"unavailable"`; the log holds the actual error. Connection errors carry
hosts, ports and sometimes usernames, and that does not belong in an HTTP
response body.

**Queue backend: not decided yet.** Postgres with `FOR UPDATE SKIP LOCKED` or
Redis Streams. This is the central architectural choice of the project and will
be documented here — with what the losing option would have given up — once the
delivery pipeline is built.

## Roadmap

**Milestone 1 — foundation** ✅

- [x] Postgres and Redis via Docker Compose
- [x] Configuration validated at boot, structured JSON logging
- [x] Liveness and readiness endpoints
- [x] SQL migrations with goose
- [x] Graceful shutdown and HTTP server timeouts
- [x] CI: build, vet, format check, lint, tests
- [x] One `docker compose up` runs the whole stack, service included

**Milestone 2 — the API**

- [ ] API key authentication, stored hashed
- [ ] CRUD for endpoints
- [ ] `POST /events` — fast ingestion, `202 Accepted`
- [ ] Idempotent ingestion via `Idempotency-Key`

**Milestone 3 — delivery**

- [ ] Worker pool consuming the queue
- [ ] HMAC-SHA256 signature with timestamp, replay-protected
- [ ] Exponential backoff with jitter, then a dead-letter queue
- [ ] Attempt history and manual replay

**Milestone 4 — production concerns**

- [ ] Rate limiting per API key
- [ ] Prometheus metrics and a Grafana dashboard
- [ ] OpenAPI specification
- [ ] Load test results (k6) published here
- [ ] Encrypt endpoint secrets at rest

## License

[MIT](LICENSE) © Gustavo Martins Leite
