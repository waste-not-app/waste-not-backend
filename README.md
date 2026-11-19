# WasteNot Backend

**The REST API behind WasteNot, a mobile app for tracking household waste through Reduce, Reuse, Recycle, and Trash.**

![Go 1.27](https://img.shields.io/badge/Go-1.27-00ADD8?logo=go&logoColor=white)
![Fiber v3](https://img.shields.io/badge/Fiber-v3-00ACD7)
![PostgreSQL 18](https://img.shields.io/badge/PostgreSQL-18-4169E1?logo=postgresql&logoColor=white)
![OpenAPI](https://img.shields.io/badge/API-OpenAPI%203-6BA539?logo=openapiinitiative&logoColor=white)
![Docker](https://img.shields.io/badge/image-distroless%20nonroot-2496ED?logo=docker&logoColor=white)

WasteNot Backend serves the [WasteNot Android](https://github.com/waste-not-app/waste-not-android) app. It owns user accounts and sessions, waste logs, dashboards and statistics, progress and achievements, private photo attachments, and the curated waste guide. It is also the only public gateway to the [WasteNot AI](https://github.com/waste-not-app/waste-not-ai) workers: it authenticates the caller, enforces ownership and quotas, and validates every worker response against a pinned contract before it reaches the app.

## Contents

- [Features](#features)
- [Architecture](#architecture)
- [Getting started](#getting-started)
- [Configuration](#configuration)
- [API overview](#api-overview)
- [Development](#development)
- [Project structure](#project-structure)
- [Database and migrations](#database-and-migrations)
- [Deployment](#deployment)
- [Related repositories](#related-repositories)

## Features

- **Accounts and sessions.**
  - Argon2id password hashing.
  - 15-minute HS256 access tokens.
  - Rotating refresh tokens with reuse detection. Replaying an old refresh token revokes the session.
  - Every request checks the session in the database, so logout takes effect immediately.
- **Password recovery.** One-time codes are delivered over SMTP through a durable, encrypted outbox with retries.
- **Waste logs.**
  - Create, edit, and soft-delete logs.
  - Creates are idempotent through `client_request_id`.
  - Edits and deletes use optimistic concurrency through `version` and `If-Match`.
  - Lists use signed cursor pagination.
- **Dashboard and statistics.** Daily, category, and method totals in the user's time zone, plus the recycling rate, XP, level, and streaks.
- **Achievements.** Derived from the user's active logs.
- **Private photos.** Upload, attach, and download photos as their owner. A durable cleanup queue makes deletes survive crashes.
- **Waste guide.** Curated, source-reviewed articles with search and category filters.
- **AI gateway.**
  - Object detection and handling recommendations from the AI workers.
  - Per-user quotas and request deduplication.
  - Expiring sessions.
  - Strict schema validation of every worker response.
- **Account deletion.** Removes every row the user owns and drains the user's photo cleanup jobs before the deletion is confirmed.

## Architecture

```
Android app ──HTTPS──▶ WasteNot API (this repo) ──▶ PostgreSQL 18
                              │
                              └──private HTTP──▶ AI workers (detection, recommendation)
```

- **Stateless API.** Rate limits, idempotency keys, and job queues live in PostgreSQL. Several instances can therefore share one database without resetting quotas.
- **AI boundary.** The AI workers are never exposed to clients. The API calls them over loopback or a private network with dedicated service tokens.
- **Request flow.** Each feature module follows `handler → service → repository`:
  - Handlers translate HTTP.
  - Services own the business rules.
  - Repositories own SQL and transactions.

## Getting started

### With Docker (recommended)

Requirements: Docker with Compose v2, `make`, and `openssl`.

```sh
make env    # create .env with random local secrets
make up     # start PostgreSQL, run migrations, import guides, start the API
curl http://127.0.0.1:8080/health/ready
make logs
make down
```

The development stack (`compose.dev.yaml`) provides:

| Service | Address or location |
|---|---|
| API | `127.0.0.1:8080` |
| PostgreSQL | `127.0.0.1:5432` |
| Photo attachments | A Docker volume |
| Swagger UI | <http://127.0.0.1:8080/swagger/index.html> |

AI features are disabled by default. To enable them, start the workers from the AI repository on the host, then set `AI_ENABLED=true` and the worker tokens in `.env`.

### From source

Requirements: Go 1.27.4 and PostgreSQL 18.

```sh
make env       # then point DATABASE_URL at your local PostgreSQL
make migrate
make guides
make run
```

The binaries do not read `.env` themselves; the Make targets load it. `make build` produces `bin/api`, `bin/migrate`, and `bin/guides`.

## Configuration

All configuration comes from environment variables. [`.env.example`](.env.example) lists every variable.

| Variable | Required | Default | Description |
|---|---|---|---|
| `DATABASE_URL` | Yes | | PostgreSQL connection URL |
| `JWT_SECRET_BASE64` | Yes | | At least 32 random bytes, base64-encoded (`openssl rand -base64 32`) |
| `HTTP_ADDR` | | `127.0.0.1:8080` | Listen address |
| `WASTENOT_RELEASE` | | | Release identifier returned in `X-WasteNot-Release` by `/health/ready` |
| `AI_ENABLED` | | `false` | Enables AI detection |
| `AI_RECOMMENDATION_ENABLED` | | Value of `AI_ENABLED` | Enables AI recommendations |
| `AI_ALLOW_PROTOTYPE` | | `false` | Accepts fixture or draft worker output. For testing only |
| `AI_DETECTION_URL`, `AI_RECOMMENDATION_URL` | When AI is enabled | | Private worker origins |
| `AI_SERVICE_TOKEN`, `AI_RECOMMENDATION_TOKEN` | When AI is enabled | | Distinct tokens of at least 24 characters |
| `PHOTO_STORAGE_DIR` | | | Private directory for photo attachments. Leave empty to disable photos |
| `SMTP_HOST`, `SMTP_PORT`, `SMTP_USERNAME`, `SMTP_PASSWORD`, `SMTP_FROM` | For password recovery | Port `587` | SMTP with verified STARTTLS, or implicit TLS on port 465 |
| `SWAGGER_ENABLED` | | `false` | Serves Swagger UI. Never enable it on a public server |

## API overview

The machine-readable contract is [`contracts/core.openapi.json`](contracts/core.openapi.json). The public AI endpoints are described in [`contracts/ai/public.openapi.json`](contracts/ai/public.openapi.json).

### Conventions

- **Base path and format.** The base path is `/v1`.
  - Bodies are UTF-8 JSON with `snake_case` fields.
  - IDs are UUIDs, and timestamps are RFC 3339 in UTC.
- **Authentication.** Every endpoint requires `Authorization: Bearer <access_token>`, except registration, login, refresh, password reset, and `/health/*`.
- **Request IDs.** Every response carries an `X-Request-ID`. A valid UUID sent by the client is reused.
- **Envelopes.**
  - Successful responses use `{"data": ...}`.
  - Lists add `"page": {"next_cursor": ..., "has_more": ...}`.
  - Errors use `{"error": {"code", "message", "fields", "retryable"}, "request_id", "api_version"}`.
- **Retries.** Respect `Retry-After` on `429` and `503`. When retrying a create, keep the same `client_request_id` and payload.

### Endpoints

| Method | Path | Description |
|---|---|---|
| `POST` | `/v1/auth/register` | Create an account and a session |
| `POST` | `/v1/auth/login` | Start a session |
| `POST` | `/v1/auth/refresh` | Rotate the refresh token |
| `POST` | `/v1/auth/logout` | Revoke the current session |
| `POST` | `/v1/auth/password-reset` | Request a recovery code |
| `POST` | `/v1/auth/password-reset/confirm` | Set a new password and revoke all sessions |
| `GET` | `/v1/me` | Profile and progress |
| `DELETE` | `/v1/me` | Delete the account and all owned data |
| `GET` | `/v1/metadata` | Enums, form limits, and capabilities |
| `GET`, `POST` | `/v1/waste-logs` | List or create waste logs |
| `GET`, `PATCH`, `DELETE` | `/v1/waste-logs/{id}` | Read, edit, or delete a waste log |
| `GET` | `/v1/dashboard` | Today's summary, recent logs, and progress |
| `GET` | `/v1/statistics` | Totals by day, category, and method for a date range |
| `GET` | `/v1/waste-guide` | Search published guides |
| `GET` | `/v1/waste-guide/{id}` | Read a published guide |
| `GET` | `/v1/achievements` | Achievements and their progress |
| `POST` | `/v1/photos` | Upload a private photo |
| `GET`, `DELETE` | `/v1/photos/{id}` | Download or delete an owned photo |
| `GET` | `/v1/ai/objects` | AI object catalog and model capabilities |
| `POST` | `/v1/ai/detections` | Detect objects in an image |
| `POST` | `/v1/ai/recommendations` | Get handling recommendations for a confirmed object |
| `DELETE` | `/v1/ai/detections/{id}` | Cancel an AI session |
| `GET` | `/health/live` | Liveness probe |
| `GET` | `/health/ready` | Readiness probe: database reachable and the latest migration applied |

### Error codes

| Status | Codes |
|---|---|
| 400 | `INVALID_REQUEST` |
| 401 | `UNAUTHENTICATED`, `SESSION_EXPIRED` |
| 403 | `WRONG_PASSWORD` |
| 404 | `NOT_FOUND` |
| 409 | `EMAIL_EXISTS`, `VERSION_CONFLICT`, `IDEMPOTENCY_CONFLICT`, `PHOTO_ALREADY_LINKED`, `PHOTO_IN_USE` |
| 410 | `LOG_DELETED`, `AI_SESSION_EXPIRED` |
| 413 | `FILE_TOO_LARGE`, `REQUEST_TOO_LARGE` |
| 415 | `UNSUPPORTED_MEDIA_TYPE` |
| 422 | `VALIDATION_ERROR`, `INVALID_RESET_TOKEN`, `INVALID_IMAGE`, `IMAGE_DIMENSIONS_EXCEEDED` |
| 428 | `PRECONDITION_REQUIRED` |
| 429 | `RATE_LIMITED` |
| 503 | `UNAVAILABLE`, `ACCOUNT_DELETION_PENDING`, `AI_DISABLED`, `AI_UNAVAILABLE`, `AI_BUSY`, `INVALID_AI_RESPONSE` |
| 504 | `AI_TIMEOUT` |

## Development

Run `make help` for every target. The ones you will use most:

| Target | Purpose |
|---|---|
| `make up` / `make down` / `make logs` | Manage the local Docker stack |
| `make run` | Run the API from source |
| `make fmt` | Format code with gofmt and goimports |
| `make lint` | Run golangci-lint, including gosec, errorlint, and staticcheck |
| `make vuln` | Scan dependencies and the call graph with govulncheck |
| `make test` | Run unit tests. Integration tests are skipped without a database |
| `make test-integration` | Run every test against a disposable PostgreSQL container |
| `make check` | Local gate: format, vet, lint, and test |
| `make ci` | CI gate: format, vet, lint, vulnerability scan, race tests, and build |
| `make sqlc` / `make swag` | Regenerate queries and Swagger docs |
| `make docker-build` | Build the production image |

Tool versions are pinned in the Makefile and run through `go run`, so no global installation is needed.

Continuous integration:

- **CI** (`.github/workflows/ci.yml`) runs on every pull request. It verifies modules, checks the AI contract pin, tests the deployment scripts, and runs `make ci` against PostgreSQL 18. It then builds the image.
- **CD** (`.github/workflows/cd.yml`) deploys to staging only after CI succeeds on `main`.
- **Dependabot** updates Go modules, GitHub Actions, and base images weekly.

See [CONTRIBUTING.md](CONTRIBUTING.md) for the branch, commit, and review workflow.

## Project structure

```
waste-not-backend/
├── cmd/
│   ├── api/             HTTP server entrypoint and container healthcheck
│   ├── migrate/         Migration runner
│   └── guides/          Reviewed guide importer
├── internal/
│   ├── server/          Dependency wiring, routing, background workers, end-to-end tests
│   ├── modules/         Feature modules: auth, logs, analytics, achievements, photos, guides, ai, system
│   ├── domain/          Shared business types and rules, free of HTTP and SQL
│   ├── database/        Connection pool, embedded migrations, guide import; db/ holds sqlc output
│   ├── aiclient/        AI worker client and contract validation
│   ├── security/        JWT, Argon2id password hashing, digests
│   ├── middleware/      Authentication and request IDs
│   ├── ratelimit/       PostgreSQL-backed quotas and concurrency slots
│   ├── cursor/          Signed pagination cursors
│   ├── response/        JSON envelopes and error mapping
│   └── config/          Environment configuration
├── contracts/           Core and AI OpenAPI contracts, Android fixtures
├── sql/                 sqlc migrations and queries
├── guides/              Curated waste guide content
├── deploy/              Deployment scripts, Nginx, systemd, and database hardening
├── docs/                Generated Swagger documentation
└── scripts/             Contract and smoke-test tooling
```

## Database and migrations

- **Migrations run separately.** The API never migrates the database on startup. Run `cmd/migrate` as its own deployment step; Compose does this through the `migrate` service.
- **Migration safety.** Migrations are embedded in the binary and applied in name order under an advisory lock. Their checksums are recorded, so a migration that has already been applied must never be edited. Evolve the schema with new files only.
- **Two copies of the schema.** It lives in `internal/database/*.sql`, which the runner reads, and in `sql/migrations/*.up.sql`, which sqlc reads. Add every schema change to both places. `TestSchemaCopiesStayInSync` verifies that they match.
- **Queries.** Write queries in `sql/queries/*.sql` and regenerate `internal/database/db` with `make sqlc`. Never edit generated code by hand.

## Deployment

Staging runs on a self-hosted GitHub Actions runner with Docker Compose. The public endpoint is <https://waste-not-api.bccdev.id/v1/>.

After CI passes on `main`, the CD workflow:

1. Builds the image.
2. Loads runtime configuration from private files on the host.
3. Runs migrations and starts the API.
4. Imports the reviewed guides.
5. Waits for `/health/ready`.

The API container runs as a nonroot user with:

- a read-only filesystem;
- all capabilities dropped;
- `no-new-privileges`.

Before the first deployment, provision the photo directory on the host:

```sh
sudo install -d -o 65532 -g 65532 -m 0700 /var/lib/wastenot-photos
```

Operational notes:

- **Secrets.** Never commit `.env` files, tokens, or database dumps. Rotating `JWT_SECRET_BASE64` signs every user out.
- **Network exposure.** Terminate TLS at the ingress, and keep PostgreSQL and the AI workers off the public network.
- **Rate limiting behind a proxy.** The reverse proxy must forward `X-Real-IP` (see [`deploy/nginx-location.conf`](deploy/nginx-location.conf)) so authentication rate limits apply per client.
- **Capacity.** Each instance opens at most 10 database connections. Plan the PostgreSQL connection limit for every instance, plus migrations.

## Related repositories

| Repository | Description |
|---|---|
| [waste-not-android](https://github.com/waste-not-app/waste-not-android) | Android client |
| [waste-not-ai](https://github.com/waste-not-app/waste-not-ai) | Detection and recommendation workers |
| [waste-not-landing](https://github.com/waste-not-app/waste-not-landing) | Marketing site and privacy policy |
