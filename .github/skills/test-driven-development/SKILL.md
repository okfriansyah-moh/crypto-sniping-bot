---
name: test-driven-development
type: skill
description: >
  RED-GREEN-REFACTOR cycle enforcement. No production code without a failing
  test first. Prevents test-after contamination, forces interface design before
  implementation, yields naturally testable architecture.
---

# Test-Driven Development

**Iron Law:** No production code without a failing test first. Always. No exceptions.

---

## The Cycle

```
RED   → Write a failing test for exactly the next behavior needed
GREEN → Write the minimum code to make that specific test pass
        (ugly, hardcoded, naive — doesn't matter)
REFACTOR → Improve code while keeping tests green
         → Refactor tests for readability (keep behavior the same)
```

Each iteration covers exactly one behavior. Commit after every GREEN.

---

## Verification Checklist (Before Writing Any Code)

Before writing production code:

- [ ] Test written first
- [ ] Test fails with a meaningful error (not compile/import error)
- [ ] Error message confirms what's actually missing
- [ ] Test name follows `Test<WhatBehavior>_<Condition>` pattern

After making test pass:

- [ ] Test passes
- [ ] All other tests still pass
- [ ] No production code beyond what the test requires
- [ ] Ready to refactor (if needed)

---

## Common Rationalizations (All Forbidden)

| Rationalization                     | Why It's Wrong                                      |
| ----------------------------------- | --------------------------------------------------- |
| "I'll add tests after"              | You won't. After-tests don't drive design.          |
| "This is too simple to need a test" | Simplest code has bugs. Simple tests are free.      |
| "I know what it should do"          | Write it as a test then.                            |
| "It's just a helper/utility"        | Helpers accumulate bugs. Test them first.           |
| "TDD takes longer"                  | It doesn't. Debugging without tests takes longer.   |
| "This is a spike"                   | Fine. Delete all code when spike is done, then TDD. |
| "I need to see the code first"      | Write the test with `// TODO: implement`.           |

---

## Test Structure (AAA Pattern)

```go
func TestModuleName_Behavior_Condition(t *testing.T) {
    // Arrange
    input := SomeDTO{...}  // from contracts/
    expected := SomeOutputDTO{...}

    // Act
    result := ProcessSomething(input)

    // Assert
    assert.Equal(t, expected, result)
}
```

- **One assertion concept per test**
- **Descriptive test name** that reads as a specification
- **No test logic** — no loops, no conditionals in tests
- **Independent** — each test runs and passes alone

---

## RED Phase — Making Tests Fail Correctly

Test must fail because the **behavior doesn't exist yet**, not because of:

- Import errors (fix these before counting as RED)
- Compile errors (not a failing test — fix compilation first)
- Wrong test logic (bad arrange or assert)

Fail message should be:

- "function not found" → compile error, not RED yet
- "expected X, got nil/zero" → this is RED ✅
- "expected true, got false" → RED ✅

**Never proceed to GREEN on a test that fails for the wrong reason.**

---

## GREEN Phase — Minimum Code

Write the **minimum code to pass the specific failing test:**

- Hardcoded return value? Fine.
- Naive loop? Fine.
- Duplicate code? Fine.
- No abstraction? Fine.

Resist the urge to "do it right" here. Save design improvements for REFACTOR.

**Never write code "for the next test."** That's not TDD.

---

## REFACTOR Phase — Improve Without Breaking

Improve code while keeping all tests green:

- Extract duplicate logic (only if it exists in ≥2 places)
- Rename for clarity
- Simplify conditionals
- Clean up test readability (names, setup, assertion messages)

**Never add behavior during REFACTOR.** If you want new behavior, write a new RED test.

---

## skeleton-parallel Test Guidelines

### Module tests (pure functions)

```go
// Correct: test module function directly
result, err := module.ProcessStage(inputDTO)
assert.NoError(t, err)
assert.Equal(t, expectedDTO, result)
```

### Orchestrator tests (integration)

- Use in-memory SQLite for DB tests
- Reset DB state between tests
- Test the full pipeline for small inputs

### Test file placement

```
tests/
  unit/
    modules/
      health/
        check_test.go       # mirrors app/modules/health/feature/check.go
  integration/
    pipeline_test.go        # full pipeline smoke test
```

### What NOT to test

- Internal implementation details (test behavior, not internals)
- Private functions (test them via the public function that calls them)
- Error paths that can't happen (don't add validation for impossible states)

---

## Commit Discipline

Commit after every GREEN:

```
git commit -m "test: add failing test for health check timeout
git commit -m "feat: implement health check timeout handling"
git commit -m "refactor: extract timeout constant to config"
```

Small commits = easier bisect, cleaner history, clear progression.

---

## Checklist

- [ ] Test written before any production code
- [ ] Test fails with meaningful message (behavior missing, not compile error)
- [ ] Production code written to pass this specific test only
- [ ] All tests green after implementation
- [ ] Refactor complete (if needed), tests still green
- [ ] No rationalization accepted (see table above)
- [ ] Commit made after GREEN
