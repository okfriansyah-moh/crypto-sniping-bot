---
name: api-design
type: skill
description: "API design patterns. Use when designing REST/gRPC endpoints, error responses, pagination, versioning, or request/response contracts."
---

## Purpose

Enforce consistent API design patterns across all modules. Ensure endpoints follow RESTful conventions, return consistent error formats, and handle pagination and versioning correctly.

---

## Rules

### REST Conventions

1. **Nouns for resources, verbs via HTTP methods** — `GET /users`, not `GET /getUsers`
2. **Plural resource names** — `/users`, `/templates`, `/orders`
3. **Nested resources for relationships** — `/users/{id}/orders`
4. **HTTP methods map to CRUD** — GET=read, POST=create, PUT=replace, PATCH=update, DELETE=remove
5. **Idempotent methods** — GET, PUT, DELETE are idempotent; POST is not

### Status Codes

| Code | Meaning               | Use                                     |
| ---- | --------------------- | --------------------------------------- |
| 200  | OK                    | Successful GET, PUT, PATCH              |
| 201  | Created               | Successful POST that creates a resource |
| 204  | No Content            | Successful DELETE                       |
| 400  | Bad Request           | Validation error, malformed input       |
| 401  | Unauthorized          | Missing or invalid authentication       |
| 403  | Forbidden             | Authenticated but not authorized        |
| 404  | Not Found             | Resource doesn't exist                  |
| 409  | Conflict              | Duplicate resource, state conflict      |
| 422  | Unprocessable Entity  | Semantically invalid input              |
| 500  | Internal Server Error | Unexpected server failure               |

### Error Response Format

```json
{
  "error": {
    "code": "VALIDATION_ERROR",
    "message": "Human-readable description",
    "details": [{ "field": "email", "reason": "Invalid email format" }]
  }
}
```

1. **Consistent error structure** — every error response uses the same shape
2. **Machine-readable error codes** — uppercase snake_case (e.g., `VALIDATION_ERROR`)
3. **No internal details in production** — no stack traces, no SQL errors, no internal paths
4. **Localization-ready messages** — human-readable messages are separate from codes

### Pagination

```json
{
  "data": [...],
  "pagination": {
    "page": 1,
    "per_page": 20,
    "total": 150,
    "total_pages": 8
  }
}
```

1. **Cursor-based for large datasets** — avoid offset pagination at scale
2. **Default page size** — configurable via `config.yaml`, not hardcoded
3. **Maximum page size limit** — prevent DoS via oversized page requests

### Versioning

1. **URL path versioning** — `/api/v1/users` (preferred for simplicity)
2. **No breaking changes within a version** — additive only
3. **Deprecation headers** — `Sunset: <date>` header before removal

---

## Checklist

```
[ ] Endpoints use plural nouns (not verbs)
[ ] HTTP methods match CRUD operations
[ ] Error responses follow consistent format
[ ] No internal details in error responses
[ ] Pagination on all list endpoints
[ ] Page size has configurable maximum
[ ] API version in URL path
[ ] Content-Type headers set correctly
[ ] Input validated at handler layer
[ ] Response DTOs are specific to the endpoint
```
