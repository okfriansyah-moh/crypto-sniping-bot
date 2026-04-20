---
name: vertical-slice
type: skill
description: "Vertical slice architecture enforcement. Use when organizing features, creating modules, or reviewing project structure. Ensures each feature is self-contained with handler/service/repository/DTO layers."
---

## Purpose

Enforce vertical slice architecture where each feature is a self-contained unit with its own handler, service, repository, and DTO layers. Prevents horizontal layering that leads to cross-cutting coupling.

---

## Rules

### Feature Organization

1. **Each feature is self-contained** — handler, service, repository, DTO all live in the same directory
2. **Features don't import other features** — use the module's port interface for cross-feature communication
3. **One feature per use case** — `list`, `detail`, `create`, `update`, `delete`, `sync` are separate features
4. **Feature names are verbs or noun-phrases** — describe what the feature DOES, not what layer it IS

### Module Structure

```
app/modules/<module_name>/
├── port/                    # Public interface for other modules
│   └── <module>_port.*      # Exported interfaces/types
├── model.*                  # Domain models (ORM or plain)
├── feature/                 # Vertical slices (each feature is self-contained)
│   ├── <feature_a>/
│   │   ├── handler.*        # HTTP/gRPC handler (request → response)
│   │   ├── service.*        # Business logic (orchestrates domain operations)
│   │   ├── repository.*     # Data access (queries, persistence)
│   │   ├── dto.*            # Request/response DTOs for this feature
│   │   └── instrumentation.*  # Metrics, logging, tracing for this feature
│   └── <feature_b>/
│       └── ...
├── endpoint/                # Route registration (aggregates all features)
│   └── http.*               # Maps routes → feature handlers
└── integration/             # Module-scoped integration tests
    └── test_*
```

### Layer Responsibilities

| Layer               | Responsibility                               | Depends On            |
| ------------------- | -------------------------------------------- | --------------------- |
| **handler**         | Parse request, call service, format response | service, dto          |
| **service**         | Business logic, validation, orchestration    | repository, dto, port |
| **repository**      | Data access, queries, persistence            | model, dto            |
| **dto**             | Feature-specific request/response types      | (none)                |
| **instrumentation** | Metrics, logging, tracing                    | (none)                |

### Port Interface

1. **Ports are the ONLY public API** — other modules depend on port interfaces, not feature internals
2. **Ports define interfaces** — abstract types that hide implementation details
3. **Ports live at module root** — `<module>/port/<module>_port.*`
4. **Ports use contracts DTOs** — for inter-module communication, use types from `contracts/`

### Endpoint Registration

1. **One endpoint file per module** — aggregates all feature handlers
2. **Declarative route mapping** — maps HTTP verbs/paths to feature handlers
3. **No business logic in endpoints** — pure routing only

---

## Anti-Patterns

| Pattern                           | Problem             | Fix                                               |
| --------------------------------- | ------------------- | ------------------------------------------------- |
| `services/` directory at root     | Horizontal layering | Move to `<module>/feature/<feature>/service.*`    |
| `repositories/` directory at root | Horizontal layering | Move to `<module>/feature/<feature>/repository.*` |
| Feature importing another feature | Coupling            | Use port interface                                |
| Handler with business logic       | Mixed concerns      | Extract to service layer                          |
| Shared repository across features | Hidden coupling     | Each feature owns its queries                     |

---

## Checklist

```
[ ] Each feature has handler, service, DTO (repository optional)
[ ] No cross-feature imports within a module
[ ] Inter-module communication goes through port interfaces
[ ] Endpoint file only does route registration
[ ] Feature names describe use cases, not layers
[ ] Integration tests are module-scoped
[ ] No business logic in handlers (delegate to service)
[ ] Each feature's DTO is specific to that feature's request/response
```
