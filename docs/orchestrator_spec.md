# Orchestrator Specification

> Generated using `.github/prompts/orchestrator.prompt.md`.
> Defines the execution model, checkpointing, resume behavior, and failure handling.

---

## 1. Execution Model

The orchestrator is a **single-threaded, sequential executor** that:

- Owns the entire pipeline call graph
- Is the ONLY component that calls modules
- Is the ONLY component that calls the database adapter
- Advances through stages one at a time, checkpointing after each

```python
# Conceptual execution model
for stage in PIPELINE_STAGES:
    if stage <= last_completed_stage:
        continue  # Resume: skip completed stages
    output_dto = stage.module.process(input_dto, config)
    adapter.checkpoint(run_id, stage.name)
    input_dto = output_dto  # Feed output to next stage
```

---

## 2. Stage Ordering

The pipeline stage sequence is defined in `docs/architecture.md` and is **immutable**:

```
stage_1 → stage_2 → stage_3 → ... → stage_N
```

**Rules:**

- Never reorder stages
- Never skip stages
- Never parallelize stages at runtime
- Advance by exactly one stage at a time

---

## 3. Checkpointing

After every stage completes successfully:

1. Validate stage postconditions (output DTO is valid)
2. Write `last_completed_stage` to `pipeline_runs` table
3. Commit the transaction

```sql
UPDATE pipeline_runs
SET last_completed_stage = ?,
    updated_at = CURRENT_TIMESTAMP
WHERE run_id = ?;
```

**Rules:**

- Checkpoint is a single SQL UPDATE in a transaction
- No skip-forward (must advance by exactly one stage)
- If checkpoint write fails, the stage is NOT considered complete

---

## 4. Resume Behavior

On restart, the orchestrator:

1. Computes entity ID from input (content-addressable)
2. Queries `pipeline_runs` for an existing run with that entity ID
3. Decision:
   - If `status = 'completed'` → exit early (idempotent)
   - If `status = 'failed'` → start fresh or resume based on policy
   - If incomplete → reconstruct DTOs from DB, resume from next stage after `last_completed_stage`

```python
run = adapter.get_run_by_entity(entity_id)
if run and run.status == 'completed':
    logger.info("Already completed, exiting")
    return
if run:
    start_index = STAGES.index(run.last_completed_stage) + 1
else:
    start_index = 0
```

### DTO Reconstruction

For resume, the orchestrator reconstructs intermediate DTOs by querying the database:

```python
# Example: reconstruct Stage2Result from DB
if start_index > 2:
    stage_2_data = adapter.get_stage_2_data(entity_id)
    stage_2_result = Stage2Result(**stage_2_data)
```

---

## 5. Pre-Flight Checks

Before the pipeline starts, validate:

1. **Runtime version** — Meets project's minimum requirement
2. **Disk space** — Sufficient free space for output
3. **Input validation** — Input exists, readable, correct format
4. **External dependencies** — Required tools available in PATH (project-specific)
5. **Database** — Can connect, schema is up to date

```
# Example pre-flight (pseudocode)
function preflight(input_path, config):
    assert runtime_version >= minimum_required
    assert file_exists(input_path)
    assert free_disk_space(config.output_dir) >= config.min_disk_space_mb * 1024 * 1024
```

---

## 6. State Transitions

### Pipeline Run Lifecycle

```
started → processing → completed
                    → partial    (some entities succeeded, some failed)
                    → failed     (critical failure or >50% entity failures)
```

### Entity Lifecycle

```
created → queued → processed → completed
                             → failed
```

**Rules:**

- No backward transitions
- `completed` is terminal — pipeline exits early on re-run
- `failed` is terminal after retry exhaustion
- `partial` indicates mixed results — some work was saved

---

## 7. Failure Handling

### Pipeline-Level Failures

| Trigger                       | Action                               |
| ----------------------------- | ------------------------------------ |
| Pre-flight check fails        | Abort immediately, status = `failed` |
| Stage throws unhandled error  | Log, attempt retry, then abort       |
| >50% entities fail in a stage | Abort pipeline, status = `failed`    |
| Disk space exhausted          | Abort pipeline, clean temp files     |

### Entity-Level Failures

| Trigger          | Action                                 |
| ---------------- | -------------------------------------- |
| Processing error | Retry up to `max_retries` (default 2)  |
| Retry exhausted  | Mark entity `failed`, continue to next |
| External timeout | Kill process, retry once with fallback |

### Retry Policy

```python
max_retries = config["thresholds"]["max_retries"]  # Default: 2
for attempt in range(max_retries + 1):
    try:
        result = module.process(input_dto, config)
        break
    except Exception as e:
        if attempt == max_retries:
            adapter.update_entity_status(entity_id, "failed")
            logger.error("Entity failed after retries", extra={...})
        else:
            logger.warning(f"Retry {attempt + 1}/{max_retries}", extra={...})
```

---

## 8. DTO Routing

The orchestrator routes DTOs between stages:

```python
# stage_1 output feeds stage_2 input
result_1 = stage_1.process(raw_input, config)
adapter.checkpoint(run_id, "stage_1")

# stage_2 output feeds stage_3 input
result_2 = stage_2.process(result_1, config)
adapter.checkpoint(run_id, "stage_2")

# Continue for all stages...
```

For fan-out stages (one input, multiple consumers):

```python
# Multiple stages consume result_1
result_a = stage_a.process(result_1, config)
result_b = stage_b.process(result_1, config)
```

For fan-in stages (multiple inputs):

```python
# stage_c requires outputs from both stage_a and stage_b
result_c = stage_c.process(result_a, result_b, config)
```

---

## 9. Database Interaction

All database operations go through `database/adapter.*`:

```python
adapter = DatabaseAdapter(config)
adapter.initialize()

# Create run
adapter.create_run(PipelineRunDTO(run_id=run_id, entity_id=entity_id))

# Checkpoint
adapter.update_run_stage(run_id, stage_name)

# Query
run = adapter.get_run(run_id)

# Cleanup
adapter.close()
```

**Rules:**

- Adapter accepts and returns frozen DTOs
- All SQL uses portable syntax
- All inserts use `ON CONFLICT DO NOTHING`
- Parameterized queries only

---

## 10. Idempotency Guarantees

1. **Content-addressable IDs** — Same input = same entity ID
2. **ON CONFLICT DO NOTHING** — Duplicate inserts are safe
3. **Skip-if-completed** — Pipeline exits early if already done
4. **Skip-if-processed** — Individual entities skipped if already processed
5. **Atomic file writes** — Write to temp, then rename
