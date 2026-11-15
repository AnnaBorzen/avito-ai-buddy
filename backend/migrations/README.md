# Database migrations

Migrations live next to the backend and use the `golang-migrate` naming scheme:

```text
{version}_{name}.up.sql
{version}_{name}.down.sql
```

## Apply

```bash
make db-up
make db-migrate-up
make db-migrate-version
```

`make db-up` starts PostgreSQL from `docker-compose.yml`. The `migrate` service
is available under the `tools` compose profile and is not started by `make up`.

## Rollback

```bash
make db-migrate-down
```

Down migrations are destructive and intended for local development. Production
rollouts are forward-only; ship a corrective migration instead of rolling data
back.

## Rules

- The API never applies migrations on start. Migrations are a separate deploy
  step so that several backend replicas do not race each other.
- `000001_operations` owns operations, immutable payload snapshots, tickets and
  idempotency records.
- `000002_analytics` owns analytics facts. Events are historical snapshots and
  intentionally have no foreign keys to operations.
- Reference data (`profiles`, `teams`, `resources`, scenario definition) is
  seeded in `backend/internal/domain/seed.go`; §9 of the architecture does not
  define tables for it.
- Changing an enum, event name or event field requires a new migration and an
  update to `docs/metrics.md` and the domain constants.
