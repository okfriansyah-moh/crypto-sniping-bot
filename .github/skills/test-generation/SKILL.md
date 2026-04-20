---
name: test-generation
type: skill
description: "Test generation patterns. Use when creating unit tests, integration tests, or validating test coverage. Provides testing strategies, mocking patterns, and coverage requirements."
---

## Purpose

Generate comprehensive, maintainable tests that validate behavior without coupling to implementation. Ensure all modules have sufficient test coverage with deterministic, fast-executing tests.

---

## Rules

### Test Structure

1. **Arrange-Act-Assert (AAA)** — every test follows: set up inputs → execute → verify outputs
2. **One assertion concept per test** — test one behavior, not multiple unrelated things
3. **Descriptive names** — `test_<feature>_<scenario>_<expected_result>` pattern
4. **No test interdependence** — each test runs independently, no shared mutable state
5. **Tests mirror module structure** — `tests/` structure maps to `app/modules/`

### Unit Tests

1. **Test pure functions** — given input DTO → expected output DTO
2. **Mock external dependencies** — database, network, filesystem
3. **Test edge cases** — empty inputs, max values, unicode, special characters
4. **Test error paths** — invalid input, timeout, missing data
5. **Deterministic** — no randomness, no time-dependence, no network calls

### Integration Tests

1. **Test module boundaries** — verify DTO flows between producer and consumer
2. **Test the happy path** — complete flow from input to output
3. **Test failure recovery** — checkpoint/resume, retry logic
4. **Use test fixtures** — reusable, version-controlled test data
5. **Scope per module** — each module has its own `integration/` directory

### Coverage Requirements

| Type                            | Minimum | Target |
| ------------------------------- | ------- | ------ |
| Unit tests                      | 80%     | 90%    |
| Integration tests               | 60%     | 80%    |
| Critical paths (auth, payments) | 95%     | 100%   |

### Test Data

1. **No real data in tests** — use synthetic, deterministic fixtures
2. **Content-addressable test IDs** — `SHA256("test_" + fixture_name)[:16]`
3. **Minimal fixtures** — smallest data that exercises the behavior
4. **Frozen fixtures** — test data doesn't change between runs

---

## Patterns

### Unit Test Pattern

```
test_<module>_<feature>_<scenario>():
    # Arrange
    input_dto = create_test_dto(...)
    config = create_test_config(...)

    # Act
    result = module.process(input_dto, config)

    # Assert
    assert result.status == "completed"
    assert result.entity_id == expected_id
```

### Integration Test Pattern

```
test_<stage_a>_to_<stage_b>_flow():
    # Arrange: output of stage A
    stage_a_output = create_stage_a_output(...)

    # Act: feed to stage B
    stage_b_result = stage_b.process(stage_a_output, config)

    # Assert: validates DTO contract between stages
    assert isinstance(stage_b_result, ExpectedDTO)
    assert stage_b_result.required_field is not None
```

---

## Anti-Patterns

| Pattern                        | Problem                                 | Fix                              |
| ------------------------------ | --------------------------------------- | -------------------------------- |
| `test_everything()`            | Tests too many things                   | Split into focused tests         |
| `time.sleep(1)` in tests       | Flaky, slow                             | Use deterministic waits or mocks |
| `@skip("fix later")`           | Dead tests accumulate                   | Fix or delete immediately        |
| Testing implementation details | Brittle to refactoring                  | Test behavior via public API     |
| `random.seed(42)`              | Still non-deterministic across versions | Use fixed test fixtures          |

---

## Checklist

```
[ ] All modules have unit tests
[ ] Integration tests cover DTO boundaries
[ ] No network calls in unit tests
[ ] No filesystem dependencies in unit tests
[ ] Test names describe behavior, not implementation
[ ] Edge cases covered (empty, max, invalid)
[ ] Error paths tested
[ ] Coverage meets minimum thresholds
[ ] Tests are deterministic and fast (<5s per test)
[ ] Test fixtures are version-controlled
```
