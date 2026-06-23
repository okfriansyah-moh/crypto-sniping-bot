# Integration tests (`backend-dashboard/tests/integration`)

Cross-boundary smoke tests for operator dashboard plan (Task 25).  
Canonical location: Go `internal` import rules require tests that wire `backend-dashboard/internal/api` to live under `backend-dashboard/`.

## Packages

| File | Coverage |
|------|----------|
| `dashboard_api_test.go` | Read-only `GET /api/v1/*` smoke via in-memory fixture + auth middleware |
| `operator_command_test.go` | `POST /api/v1/commands` → event bus → `internal/operator.ExecuteCommand` round-trip |

## Run

```bash
go test ./backend-dashboard/tests/integration/... -v -count=1
go build ./...
go test ./...
cd frontend-dashboard && npm run build
```

No Postgres, network, or GPU required — uses `httptest.Server` and `dashboardFixtureDB`.

## Docker Compose smoke (manual)

```bash
export DASHBOARD_API_KEY=... SNIPER_DB_PASSWORD=...
docker compose up -d db migrate hydrate dashboard-api dashboard-ui sniper-bot
curl -fsS http://localhost:8090/api/v1/health
curl -fsS -H "X-Dashboard-Key: $DASHBOARD_API_KEY" http://localhost:8090/api/v1/overview
```

Sniper (`:8080`) and dashboard-api (`:8090`) deploy independently; dashboard read API degrades gracefully when pipeline workers are idle.
