# Dashboard integration tests (relocated)

Operator dashboard cross-boundary integration tests live under:

**`backend-dashboard/tests/integration/`**

Go `internal` import rules forbid importing `backend-dashboard/internal/api` from the root `tests/` tree — dashboard API integration tests must live inside `backend-dashboard/`.

See `backend-dashboard/tests/integration/README.md` for run commands.

**Do not add** `dashboard_*` or `operator_command_*` Go files here.
