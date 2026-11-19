# Contributing

Thanks for helping out with WasteNot Backend. Here's how we work.

## Workflow

1. Branch off `main` with a prefix that says what you're doing: `feat/`, `fix/`, `refactor/`, `docs/`, or `chore/`.
2. Keep each commit to one change, with a one-line [Conventional Commits](https://www.conventionalcommits.org/) message such as `fix(auth): release the hashing slot on error`.
3. Run `make check` before you push. If you touched SQL, migrations, or transactions, run `make test-integration` as well.
4. Open a pull request against `main`. It can merge once CI is green.

## Code standards

Format with `make fmt`, and keep `make lint` at zero issues.

Write code that explains itself. Add a comment only when you really need one, and keep it to a single line. Handlers still need their Swagger annotations (`// @...`).

Keep the layers apart: handlers deal with HTTP, services hold the business rules, and repositories own SQL and transactions. Rules shared across modules go in `internal/domain`.

Put new queries in `sql/queries/` and run `make sqlc`. Schema changes go in as a new migration in both `internal/database/` and `sql/migrations/`, and an applied migration is never edited.

If you change an endpoint, update `contracts/core.openapi.json` and run `make swag`. Changes to the AI contract need a matching change in the AI repository; [contracts/README.md](contracts/README.md) explains how.

All user-facing text, including validation messages and emails, is written in English.

## Security

Never commit secrets, tokens, `.env` files, or database dumps. If you find a vulnerability, tell the maintainers privately instead of opening a public issue.
