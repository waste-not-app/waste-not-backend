# Contributing

Thank you for helping improve WasteNot Backend. This guide covers the workflow and standards every change is expected to follow.

## Workflow

1. Branch from `main` using a descriptive prefix: `feat/`, `fix/`, `refactor/`, `docs/`, or `chore/`.
2. Keep each commit to one logical change. Write a single-line message in the [Conventional Commits](https://www.conventionalcommits.org/) format, for example `fix(auth): release the hashing slot on error`.
3. Run `make check` before pushing. If you change SQL, migrations, or transactions, also run `make test-integration`.
4. Open a pull request against `main`. CI must pass before the pull request is merged.

## Code standards

- **Formatting and lint.** Format with `make fmt`. `make lint` must report zero issues.
- **Comments.** Prefer self-explanatory code. Add a comment only when it is essential, and keep it to one line. Swagger annotations (`// @...`) are required on handlers.
- **Layering.**
  - Handlers translate HTTP.
  - Services hold business rules.
  - Repositories hold SQL and transactions.
  - Shared rules belong in `internal/domain`.
- **Queries.** Write new queries in `sql/queries/`, then run `make sqlc`.
- **Schema changes.** Add the change as a new migration in both `internal/database/` and `sql/migrations/`. Never edit a migration that has already been applied.
- **API changes.** Update `contracts/core.openapi.json` and run `make swag`.
- **AI contract changes.** These need matching updates in the AI repository. See [contracts/README.md](contracts/README.md).

## Security

Never commit secrets, tokens, `.env` files, or database dumps. Report vulnerabilities privately to the maintainers rather than in a public issue.
