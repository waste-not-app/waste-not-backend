# WasteNot Backend

**The REST API behind WasteNot, a mobile app for tracking household waste through Reduce, Reuse, Recycle, and Trash.**

![Go 1.27](https://img.shields.io/badge/Go-1.27-00ADD8?logo=go&logoColor=white)
![Fiber v3](https://img.shields.io/badge/Fiber-v3-00ACD7)
![PostgreSQL 18](https://img.shields.io/badge/PostgreSQL-18-4169E1?logo=postgresql&logoColor=white)
![OpenAPI](https://img.shields.io/badge/API-OpenAPI%203-6BA539?logo=openapiinitiative&logoColor=white)
![Docker](https://img.shields.io/badge/image-distroless%20nonroot-2496ED?logo=docker&logoColor=white)

This service powers the [WasteNot Android](https://github.com/waste-not-app/waste-not-android) app. It handles accounts and sessions, waste logs, the dashboard and statistics, achievements, private photo attachments, and the curated waste guide.

It's also the only public way into the [WasteNot AI](https://github.com/waste-not-app/waste-not-ai) workers. Before anything from AI reaches the app, the API checks who is asking, enforces ownership and quotas, and validates the worker's response against a pinned contract.

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

### Accounts

Passwords are hashed with Argon2id. Access tokens are HS256 JWTs that last 15 minutes. Refresh tokens rotate on every use, and replaying an old one revokes the whole session. The API checks the session in the database on every request, so signing out takes effect right away.

Forgotten passwords are handled with a one-time code sent by email. Codes go through an encrypted outbox that retries delivery. Deleting an account removes everything the user owns and waits for their photo cleanup to finish before confirming.

Google sign-in is optional. The API verifies the Google ID token and a one-time nonce, then creates a regular WasteNot session. The stable Google subject identifies the user after linking. Matching an existing email never links accounts without the current WasteNot password. New Google accounts have no password and use a fresh Google proof to confirm account deletion.

### Waste logs and progress

- Create, edit, and soft-delete logs. Creates are idempotent through `client_request_id`, and edits and deletes use optimistic concurrency through `version` and `If-Match`.
- Lists use signed cursors, so pagination can't be tampered with.
- The dashboard and statistics show daily, category, and method totals in the user's time zone, along with the recycling rate, XP, level, and streaks.
- Achievements are derived from the user's active logs.

### Photos and guides

Users can upload private photos, attach them to logs, and download them. Only the owner can read a photo. A durable cleanup queue makes sure deleted files really go away, even after a crash.

The waste guide serves curated, source-reviewed articles with search and category filters.

### AI gateway

The API forwards object detection and handling recommendations to the AI workers. It adds per-user quotas, request deduplication, and expiring sessions, and it rejects any worker response that doesn't match the schema.

## Architecture

```
Android app ──HTTPS──▶ WasteNot API (this repo) ──▶ PostgreSQL 18
                              │
                              └──private HTTP──▶ AI workers (detection, recommendation)
```

The API keeps no state of its own. Rate limits, idempotency keys, and job queues live in PostgreSQL, so several instances can share one database without resetting anyone's quota.

Clients never talk to the AI workers directly. The API reaches them over loopback or a private network, using a separate service token for each worker.

Every feature module follows `handler → service → repository`. Handlers deal with HTTP, services hold the business rules, and repositories own the SQL and transactions.

## Getting started

### With Docker (recommended)

You'll need Docker with Compose v2, `make`, and `openssl`.

```sh
make env    # create .env with random local secrets
make up     # start PostgreSQL, run migrations, import guides, start the API
curl http://127.0.0.1:8080/health/ready
make logs
make down
```

The development stack in `compose.dev.yaml` gives you:

| Service | Address or location |
|---|---|
| API | `127.0.0.1:8080` |
| PostgreSQL | `127.0.0.1:5432` |
| Photo attachments | A Docker volume |
| Swagger UI | <http://127.0.0.1:8080/swagger/index.html> |

AI features are off by default. To turn them on, start the workers from the AI repository on your machine, then set `AI_ENABLED=true` and the worker tokens in `.env`.

### From source

You'll need Go 1.27.4 and PostgreSQL 18.

```sh
make env       # then point DATABASE_URL at your local PostgreSQL
make migrate
make guides
make run
```

The binaries don't read `.env` on their own; the Make targets load it for them. `make build` produces `bin/api`, `bin/migrate`, and `bin/guides`.

## Configuration

Everything is configured through environment variables, and [`.env.example`](.env.example) lists all of them.

| Variable | Required | Default | Description |
|---|---|---|---|
| `DATABASE_URL` | Yes | | PostgreSQL connection URL |
| `JWT_SECRET_BASE64` | Yes | | At least 32 random bytes, base64-encoded (`openssl rand -base64 32`) |
| `HTTP_ADDR` | | `127.0.0.1:8080` | Listen address |
| `WASTENOT_RELEASE` | | | Release identifier returned in `X-WasteNot-Release` by `/health/ready` |
| `WASTENOT_GOOGLE_WEB_CLIENT_ID` | For Google sign-in | | Public Google OAuth Web client ID; use the same value in the Android build |
| `AI_ENABLED` | | `false` | Turns on AI detection |
| `AI_RECOMMENDATION_ENABLED` | | Value of `AI_ENABLED` | Turns on AI recommendations |
| `AI_ALLOW_PROTOTYPE` | | `false` | Accepts fixture or draft worker output. For testing only |
| `AI_DETECTION_URL`, `AI_RECOMMENDATION_URL` | When AI is on | | Private worker origins |
| `AI_SERVICE_TOKEN`, `AI_RECOMMENDATION_TOKEN` | When AI is on | | Two different tokens of at least 24 characters |
| `PHOTO_STORAGE_DIR` | | | Private directory for photo attachments. Leave it empty to turn photos off |
| `SMTP_HOST`, `SMTP_PORT`, `SMTP_USERNAME`, `SMTP_PASSWORD`, `SMTP_FROM` | For password recovery | Port `587` | SMTP with verified STARTTLS, or implicit TLS on port 465 |
| `SWAGGER_ENABLED` | | `false` | Serves Swagger UI. Never turn it on for a public server |

## API overview

The full contract is in [`contracts/core.openapi.json`](contracts/core.openapi.json), and the public AI endpoints are in [`contracts/ai/public.openapi.json`](contracts/ai/public.openapi.json).

### Conventions

All paths start with `/v1`. Bodies are UTF-8 JSON with `snake_case` fields, IDs are UUIDs, and timestamps are RFC 3339 in UTC.

Every endpoint needs `Authorization: Bearer <access_token>`, except registration, password and Google sign-in, Google challenge issuance, refresh, password reset, and `/health/*`.

Each response carries an `X-Request-ID`. If the client sends a valid UUID in that header, the API reuses it.

Successful responses look like `{"data": ...}`, and lists add `"page": {"next_cursor": ..., "has_more": ...}`. Errors look like `{"error": {"code", "message", "fields", "retryable"}, "request_id", "api_version"}`.

When you get a `429` or `503`, wait for the time in `Retry-After`. If you retry a create, send the same `client_request_id` and payload.

### Endpoints

| Method | Path | Description |
|---|---|---|
| `POST` | `/v1/auth/register` | Create an account and a session |
| `POST` | `/v1/auth/login` | Start a session |
| `POST` | `/v1/auth/google/challenge` | Issue a five-minute, one-time Google nonce |
| `POST` | `/v1/auth/google` | Exchange a verified Google ID token for a WasteNot session |
| `POST` | `/v1/auth/refresh` | Rotate the refresh token |
| `POST` | `/v1/auth/logout` | End the current session |
| `POST` | `/v1/auth/password-reset` | Request a recovery code |
| `POST` | `/v1/auth/password-reset/confirm` | Set a new password and end all sessions |
| `GET` | `/v1/me` | Profile and progress |
| `DELETE` | `/v1/me` | Delete the account and everything it owns |
| `GET` | `/v1/metadata` | Enums, form limits, and capabilities |
| `GET`, `POST` | `/v1/waste-logs` | List or create waste logs |
| `GET`, `PATCH`, `DELETE` | `/v1/waste-logs/{id}` | Read, edit, or delete a waste log |
| `GET` | `/v1/dashboard` | Today's summary, recent logs, and progress |
| `GET` | `/v1/statistics` | Totals by day, category, and method for a date range |
| `GET` | `/v1/waste-guide` | Search published guides |
| `GET` | `/v1/waste-guide/{id}` | Read a published guide |
| `GET` | `/v1/achievements` | Achievements and progress toward them |
| `POST` | `/v1/photos` | Upload a private photo |
| `GET`, `DELETE` | `/v1/photos/{id}` | Download or delete one of your photos |
| `GET` | `/v1/ai/objects` | AI object catalog and model capabilities |
| `POST` | `/v1/ai/detections` | Detect objects in an image |
| `POST` | `/v1/ai/recommendations` | Get handling advice for a confirmed object |
| `DELETE` | `/v1/ai/detections/{id}` | Cancel an AI session |
| `GET` | `/health/live` | Liveness probe |
| `GET` | `/health/ready` | Readiness probe: the database is reachable and the latest migration is applied |

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

`make help` lists every target. These are the ones you'll reach for most:

| Target | Purpose |
|---|---|
| `make up` / `make down` / `make logs` | Manage the local Docker stack |
| `make run` | Run the API from source |
| `make fmt` | Format code with gofmt and goimports |
| `make lint` | Run golangci-lint, including gosec, errorlint, and staticcheck |
| `make vuln` | Scan dependencies and the call graph with govulncheck |
| `make test` | Run unit tests. Integration tests are skipped without a database |
| `make test-integration` | Run every test against a throwaway PostgreSQL container |
| `make check` | Local gate: format, vet, lint, and test |
| `make ci` | CI gate: format, vet, lint, vulnerability scan, race tests, and build |
| `make sqlc` / `make swag` | Regenerate queries and Swagger docs |
| `make docker-build` | Build the production image |

Tool versions are pinned in the Makefile and run through `go run`, so there's nothing to install globally.

CI (`.github/workflows/ci.yml`) runs on every pull request. It verifies modules, checks the AI contract pin, tests the deployment scripts, runs `make ci` against PostgreSQL 18, and builds the image. CD (`.github/workflows/cd.yml`) only deploys to staging after CI passes on `main`. Dependabot keeps Go modules, GitHub Actions, and base images up to date each week.

See [CONTRIBUTING.md](CONTRIBUTING.md) for how branches, commits, and reviews work.

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

The API never migrates the database when it starts. Run `cmd/migrate` as its own deployment step; Compose does this for you through the `migrate` service.

Migrations are embedded in the binary and applied in name order under an advisory lock. Their checksums are recorded, so once a migration has run, you must never edit it. Change the schema by adding new files instead. That's also why the oldest migrations still contain the original Indonesian seed text; a later migration updates those rows to English.

The schema lives in two places: `internal/database/*.sql` for the runner and `sql/migrations/*.up.sql` for sqlc. Add every change to both, and `TestSchemaCopiesStayInSync` will tell you if they drift apart.

Write queries in `sql/queries/*.sql` and regenerate `internal/database/db` with `make sqlc`. Don't edit the generated code by hand.

## Deployment

Staging runs on a self-hosted GitHub Actions runner with Docker Compose. Its public endpoint is <https://waste-not-api.bccdev.id/v1/>.

Once CI passes on `main`, the CD workflow:

1. Builds the image.
2. Loads runtime configuration from private files on the host.
3. Runs migrations and starts the API.
4. Imports the reviewed guides.
5. Waits for `/health/ready`.

The API container runs as a nonroot user with a read-only filesystem, all capabilities dropped, and `no-new-privileges`.

Before the first deployment, create the photo directory on the host:

```sh
sudo install -d -o 65532 -g 65532 -m 0700 /var/lib/wastenot-photos
```

A few things to keep in mind when running it:

- Never commit `.env` files, tokens, or database dumps. Rotating `JWT_SECRET_BASE64` signs everyone out.
- Terminate TLS at the ingress, and keep PostgreSQL and the AI workers off the public network.
- The reverse proxy has to forward `X-Real-IP` (see [`deploy/nginx-location.conf`](deploy/nginx-location.conf)), otherwise login rate limits apply to the proxy instead of each client.
- Each instance opens up to 10 database connections. Size PostgreSQL's connection limit for every instance, plus migrations.

## Related repositories

| Repository | Description |
|---|---|
| [waste-not-android](https://github.com/waste-not-app/waste-not-android) | Android client |
| [waste-not-ai](https://github.com/waste-not-app/waste-not-ai) | Detection and recommendation workers |
| [waste-not-landing](https://github.com/waste-not-app/waste-not-landing) | Marketing site and privacy policy |
