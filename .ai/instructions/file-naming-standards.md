Every source file name must describe the functionality it contains — not a task code, sprint ticket, phase label, or internal shorthand.

**MUST NOT:**

- Use opaque task codes or phase references as file names (e.g., `b3_test.go`, `phase4.go`, `task_impl.go`)
- Use single-letter or abbreviated names that require context to interpret (e.g., `h.go`, `dq.go`, `wt.go`)
- Name a test file after the ticket that created it — name it after the behavior it verifies
- Reuse the same name across different packages when the functionality differs

**MUST:**

- Name source files after the domain concept or behavior they implement (e.g., `confidence_gate_test.go`, `wash_trading.go`, `honeypot.go`)
- Follow the `<concept>_test.go` pattern for test files, where `<concept>` is the behavior under test
- When a file tests multiple related behaviors, use the broadest accurate concept (e.g., `process_with_estimates_test.go` for all `ProcessWithEstimates` variants)
- When renaming a file, update the package-level comment at the top of the file to match

**Examples:**

| Bad (ambiguous) | Good (functional)         | Reason                                  |
| --------------- | ------------------------- | --------------------------------------- |
| `b3_test.go`    | `confidence_gate_test.go` | Names the behavior, not the sprint task |
| `phase4.go`     | `slippage_model.go`       | Names the domain concept                |
| `helpers.go`    | `token_math.go`           | Disambiguates the specific helpers      |
| `wt.go`         | `wash_trading.go`         | Full word, no abbreviation              |
| `task_impl.go`  | `feature_extraction.go`   | Describes what the code does            |

---