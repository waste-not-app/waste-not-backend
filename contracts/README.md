# Contracts

Machine-readable contracts shared by the backend, the Android app, and the AI workers.

| Path | Owner | Purpose |
|---|---|---|
| `core.openapi.json` | Backend | Core REST API consumed by Android |
| `android/*.json` | Android | Transport fixtures the core API must keep compatible with |
| `ai/` | AI repository | Snapshot of the agreed AI contract: three OpenAPI documents, the error catalog, and JSON examples |
| `ai.lock.json` | Backend | SHA-256 checksums for every artifact in `ai/` |
| `embed.go` | Backend | Embeds the AI schemas so the API can validate requests and worker responses at runtime |

## AI contract pinning

The AI repository generates the AI contract. The backend keeps a reviewed snapshot in `ai/`, and its checksums in `ai.lock.json` must match `contracts/backend-pin.json` in the AI repository.

CI in each repository verifies its own copy against the pin. Neither CI job rewrites contracts to make a build pass. Before you publish a paired change, verify both checkouts against each other:

```sh
python3 scripts/check_contracts.py contracts/ai contracts/ai.lock.json \
  --peer ../waste-not-ai/contracts/v1
```

A change to the AI API must update all of the following together:

- the generator source in the AI repository;
- both snapshots and pins;
- the examples;
- the Go tests;
- any affected Android DTOs and tests.

Never bump the pin only to silence a failing check.

## Fixtures

Examples with `provider=fixture` or `prototype_only=true` are test material and are never valid production output. The API rejects them unless `AI_ALLOW_PROTOTYPE=true`.
