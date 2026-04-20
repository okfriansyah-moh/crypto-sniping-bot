---
name: security-audit
type: skill
description: "OWASP-aware security auditing. Use when reviewing code for security vulnerabilities, input validation, injection prevention, authentication, authorization, and secrets management."
---

## Purpose

Enforce security best practices across all modules. Detect OWASP Top 10 vulnerabilities, injection risks, authentication gaps, and data exposure issues before they reach production.

---

## Rules

### Input Validation

1. **All external input sanitized at system boundary** — validate at the adapter/handler layer, never trust upstream data
2. **Use allowlists over denylists** — define what IS valid, not what ISN'T
3. **Type-safe parsing** — parse into typed DTOs immediately, never pass raw strings through the system
4. **Length limits on all string inputs** — prevent buffer overflow and DoS via oversized payloads
5. **Reject unexpected fields** — strict schema validation, no extra properties

### Injection Prevention

1. **All SQL uses parameterized queries** — `?` placeholders, NEVER string interpolation
2. **No f-strings, template literals, or string concatenation in SQL** — regex check: `f".*SELECT|INSERT|UPDATE|DELETE`
3. **No `eval()`, `exec()`, or dynamic code execution** — forbidden in all modules
4. **HTML output escaped** — prevent XSS in any web-facing output
5. **Command injection** — never pass user input to shell commands without sanitization

### Authentication & Authorization

1. **AuthN before AuthZ** — authenticate identity before checking permissions
2. **Fail closed** — if auth check fails or errors, deny access (never default to allow)
3. **Session tokens** — cryptographically random, sufficient entropy (>= 128 bits)
4. **Principle of least privilege** — grant minimum necessary permissions

### Secrets Management

1. **No secrets in source code** — no API keys, passwords, tokens in any file tracked by git
2. **No secrets in container images** — use environment variables or secret managers
3. **No secrets in logs** — structured logging must redact sensitive fields
4. **Rotate secrets** — design for rotation without downtime

### Data Protection

1. **Encrypt sensitive data at rest** — use appropriate encryption for PII, credentials, financial data
2. **Encrypt in transit** — TLS for all network communication
3. **Minimize data exposure** — return only necessary fields in API responses
4. **Audit logging** — log all security-relevant events (login, access denied, data modification)

### Dependency Security

1. **No known vulnerable dependencies** — check against CVE databases
2. **Pin dependency versions** — avoid floating versions that could introduce vulnerabilities
3. **Minimal dependency surface** — fewer dependencies = fewer attack vectors
4. **Verify dependency integrity** — use checksums/lock files

---

## Checklist

```
[ ] All SQL uses parameterized queries (no string interpolation)
[ ] All external input validated at system boundary
[ ] No eval(), exec(), or dynamic code execution
[ ] No secrets in source code or logs
[ ] Authentication fails closed (deny on error)
[ ] Dependencies checked against known vulnerabilities
[ ] Sensitive data encrypted at rest and in transit
[ ] Error messages don't leak internal details
[ ] Rate limiting on authentication endpoints
[ ] CORS configured restrictively (not wildcard)
```

---

## CVSS Severity Guide

| Score    | Severity | Action                         |
| -------- | -------- | ------------------------------ |
| 9.0-10.0 | Critical | Fix immediately, block release |
| 7.0-8.9  | High     | Fix before release             |
| 4.0-6.9  | Medium   | Fix in current sprint          |
| 0.1-3.9  | Low      | Track, fix when convenient     |

---

## Anti-Patterns

| Pattern                          | Risk                     | Fix                              |
| -------------------------------- | ------------------------ | -------------------------------- |
| `f"SELECT * FROM {table}"`       | SQL injection            | Use parameterized queries        |
| `eval(user_input)`               | Remote code execution    | Never use eval on external input |
| `password = "hardcoded"`         | Credential exposure      | Use environment variables        |
| `except: pass`                   | Silent security failures | Log and handle appropriately     |
| `Access-Control-Allow-Origin: *` | CORS bypass              | Restrict to known origins        |
