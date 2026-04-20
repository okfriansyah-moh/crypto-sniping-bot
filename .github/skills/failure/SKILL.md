---
name: failure
description: "Failure handling. Use when implementing retry logic, abort thresholds, graceful degradation, or error recovery. Defines retry policies, failure thresholds, state transitions on error, and timeout handling."
---

# Failure Handling Skill

## Purpose

Define and enforce failure handling patterns: retry policies, abort thresholds, graceful degradation, and error recovery. Ensures the pipeline degrades gracefully and never silently corrupts data.

## Rules

### Failure Thresholds

| Condition                    | Threshold              | Action                                   |
| ---------------------------- | ---------------------- | ---------------------------------------- |
| Entity processing failures   | > 50% of items fail    | Abort pipeline, status = `failed`        |
| Optional stage failure       | Stage returns None     | Log WARN, continue with fallback         |
| Disk space during processing | < 500MB remaining      | Abort pipeline, clean intermediates      |
| External process timeout     | > configurable seconds | Kill process, retry once, then skip item |
| Empty input data             | 0 items to process     | Log WARN, continue with defaults         |

### State Transitions

Pipeline-level:

```
started → failed            (input validation failure)
processing → failed         (>50% entities failed)
processing → partial        (some entities failed, others succeeded)
processing → completed      (all entities succeeded)
```

Entity-level:

```
created → failed            (processing failure after retries exhausted)
queued → failed             (external operation failure after retries)
failed → queued             (manual retry, max 3 times total)
```

**Terminal states:** `completed` and `failed` (after exhausting retries) are final.

## Inputs

- Pipeline execution state from `database/adapter.*`
- Module processing results (success/failure DTOs)

## Outputs

- Correct state transitions on failure
- Structured error logs with JSON fields
- Graceful degradation with fallback behavior

## Examples

### Graceful Degradation

```python
def process_with_fallback(
    entity: EntityDTO,
    optional_data: OptionalDTO | None,
    config: dict
) -> ResultDTO:
    if optional_data is None:
        return create_default_result(entity, config)
    else:
        return create_enhanced_result(entity, optional_data, config)
```

### External Process Recovery

```python
def run_external_safe(command: list[str], timeout: int = 300) -> subprocess.CompletedProcess:
    try:
        return subprocess.run(command, timeout=timeout, capture_output=True, check=True)
    except subprocess.TimeoutExpired:
        logger.warning("Process timeout, retrying with conservative settings")
        fallback_cmd = apply_fallback_settings(command)
        return subprocess.run(fallback_cmd, timeout=timeout * 2, capture_output=True, check=True)
```

### Pre-Flight Checks

```
# Example pre-flight (pseudocode)
function preflight_checks(input_path, config):
    assert runtime_version >= minimum_required
    assert free_disk_space(config.output_dir) >= config.min_disk_space_bytes
    assert file_exists(input_path)
```

## Checklist

- [ ] Retry uses different settings on retry (not same parameters)
- [ ] External operations have exponential backoff
- [ ] Pipeline aborts if >50% entities fail
- [ ] Optional stage failures fall back gracefully
- [ ] Empty input doesn't crash processing
- [ ] External commands have timeout parameter
- [ ] All failures are logged with structured JSON
- [ ] Pipeline status correctly transitions to `failed` or `partial`
