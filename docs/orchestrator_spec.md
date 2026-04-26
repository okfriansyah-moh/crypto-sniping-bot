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

---

## 11. Production Hardening Worker Rules

The orchestrator and the workers it dispatches MUST satisfy the determinism + exactly-once + failure-safety contract defined in `docs/architecture.md` § 4.10. The rules below are normative and apply to every consumer worker (ingestion, data_quality, edge, probability, selection, capital, execution, position, learning).

### 11.1 Event Claim Discipline (§ 4.10.A, § 4.10.B)

Every worker dequeues events using the adapter's `ClaimNextEvents` method:

```
events = adapter.ClaimNextEvents({
    Chain:          chain,
    Consumer:       worker.Consumer,
    WorkerID:       worker.ID,           // 0..NumWorkers-1
    NumWorkers:     cfg.workers.num,
    Limit:          cfg.workers.batch_size,
})
// Adapter implementation:
//   SELECT ... FROM events
//   WHERE chain=$1 AND consumer=$2 AND processed=false
//     AND (HASHTEXT(token_address) % $5) = $4   -- partition shard
//   ORDER BY logical_order_key ASC               -- NEVER created_at
//   FOR UPDATE SKIP LOCKED LIMIT $6
```

Workers MUST process the returned batch in the order delivered (already sorted by `logical_order_key`). Out-of-order processing within a partition is FORBIDDEN.

### 11.2 Single-Worker-per-Token Lifecycle (§ 4.10.B.2)

Partitioning by `HASHTEXT(token_address) % NumWorkers` guarantees the same token's events always land on the same worker. Operators MUST NOT rebalance partitions while events are in flight; rebalancing requires the drain procedure of § 11.5.

### 11.3 DLQ Handling (§ 4.10.C)

```
result, err = handler.Process(event)
switch classify(err) {
case nil:
    adapter.MarkProcessed(event.event_id, consumer)
case Transient:
    if retry := adapter.IncrementEventRetry(event.event_id, consumer); retry > 5 {
        adapter.MoveToDLQ(...)
    }
    // event remains in events table; SKIP LOCKED returns it after backoff
case Application:
    if retry := adapter.IncrementEventRetry(event.event_id, consumer); retry > 3 {
        adapter.MoveToDLQ(...)
    }
case Determinism:   // version mismatch, ordering gap
    adapter.MoveToDLQ(event, consumer, "determinism_violation", err.Error())
    // retry budget = 0; never retried
}
```

After `MoveToDLQ` returns, the partition advances — DLQ does NOT block forward progress.

### 11.4 Exactly-Once Execution Claim (§ 4.10.D)

Every Layer 8 worker MUST gate submission on `ClaimExecution`:

```
claimed, err := adapter.ClaimExecution(allocDTO)
if err != nil      { return err }
if !claimed        { /* another worker owns this exec_id; skip silently */ return nil }
// proceed to RPC submission
```

In-memory locks (sync.Mutex, channel-based, etc.) are advisory only — the adapter is the **single** authoritative dedup boundary. A worker that submits without a successful `ClaimExecution` is a contract violation.

### 11.5 Strategy Version Activation (§ 4.10.G.2)

Mid-run config changes are FORBIDDEN. A new strategy version activates only via the orchestrator's drain procedure:

```
1. orchestrator.SuspendIngestion()
2. adapter.DrainAndCheckPipelineIdle(timeoutSec=cfg.drain.timeout_sec)
3. adapter.PromoteStrategyVersion(newVersionID, drainTimeoutSec)
   // atomically marks old version superseded + new version active
4. orchestrator.ResumeIngestion()
```

Workers reading events with `event.version_id != active.version_id` route them to DLQ with `reason="version_mismatch"` (§ 11.3).

### 11.6 Kill Switch Gate (§ 4.10.H)

Every Layer 8 execution path MUST check the kill switch before submission:

```
halted, reason, _ := adapter.IsSystemHalted(ctx)
if halted {
    emit ExecutionResultDTO{Status: "rejected", FailureCategory: "system_halted", RejectReason: reason}
    return
}
```

Halt state is persistent across process restart. Resume requires explicit operator command (`/resume`); the orchestrator MUST NOT auto-resume on startup.

### 11.7 Replay Determinism (§ 4.10.I)

The orchestrator MUST NOT consume any non-deterministic input that is not already encoded in the event stream:

- No `time.Now()` for DTO field values; use `event.BlockTimestamp` or `event.IngestedAt`.
- No `rand.*` anywhere on the production path.
- No environment variable reads after startup; config is captured into `StrategyVersion` snapshot at activation.
- Worker count and partition count are config-time constants for any given pipeline run.

CI runs replay validation (snapshot diff per § 4.10.I.2) on every merge to `main`. A green replay is a hard merge gate.

---

## 12. Stage 4 Hardening (architecture § 4.11)

### 12.1 Pure Event-Driven Execution (§ 4.11.A)

The orchestrator is a **supervisor**, not a sequencer. Boot sequence:

```
1. ApplyMigrations()
2. CrashRecovery()                           // § 12.3
3. LoadActiveStrategyVersion()
4. CheckSystemHalt()                         // refuse to start workers if halted
5. Spawn worker pools (one consumer per layer × cfg.workers.num)
6. Spawn reconciliation worker
7. Spawn evaluation janitor
8. Spawn backpressure monitor
9. Block on signal handler
```

The orchestrator MUST NOT call any module's `Process()` function during steady state. All stage transitions are mediated by the event bus. Any direct module invocation outside boot/shutdown is a contract violation.

### 12.2 Partition Lease Lifecycle (§ 4.11.B)

Each worker on startup:

```
parts, _ := adapter.ClaimPartitions(ctx, chain, consumer, workerID, cfg.workers.partitions_per_worker, cfg.workers.lease_ttl_sec)
go func() {
    t := time.NewTicker(cfg.workers.lease_ttl_sec / 3 * time.Second)
    for { select {
        case <-ctx.Done(): adapter.ReleasePartitions(...); return
        case <-t.C:        adapter.RenewPartitions(...)
    } }
}()
// Use only `parts` for ClaimNextEvents.
```

A worker that fails to renew its lease MUST stop processing immediately and re-claim from scratch. The orchestrator monitors lease coverage: if any partition is unclaimed for > `cfg.workers.unclaimed_alert_sec`, emit `system_event{level=critical, reason='partition_unclaimed'}`.

### 12.3 Crash-Safe Recovery (§ 4.11.C)

Recovery runs **before** worker pools start:

```
inFlight := adapter.ListInFlightExecutions(ctx)
for _, ex := range inFlight {
    switch {
    case ex.HasTxHash:
        receipt := rpc.GetTransactionByHash(ex.Chain, ex.TxHash)
        if receipt != nil:
            adapter.FinalizeExecution(ctx, ex.ID, receipt)
        elif now - ex.SentAt > cfg.execution.recovery_grace_sec:
            adapter.MarkExecutionLost(ctx, ex.ID, "tx_not_found_after_grace")
        else:
            // leave as in_flight; re-poll on next recovery cycle (worker startup blocks)
            requeue(ex)
    case !ex.HasTxHash:
        // reserved but never sent
        adapter.AbortReservedExecution(ctx, ex.ID, "crash_before_send")
    }
}
emit system_event{level=info, reason='crash_recovery_complete', count=len(inFlight)}
```

Workers MUST NOT start until recovery returns. `recovery_grace_sec` default = `2 × cfg.execution.receipt_timeout_sec`.

### 12.4 Reorg Detection & Cascade (§ 4.11.D)

Ingestion workers detect reorgs by parent-hash mismatch. On detection:

```
adapter.RecordReorg(chain, oldBlock, newBlock, depth)
if depth > cfg.reorg.max_depth:
    adapter.SetSystemHalt(true, "reorg_exceeds_max_depth", "automatic")
    return
adapter.InvalidateBlockRange(chain, oldBlock-depth, head)
adapter.MarkPositionsUncertain(chain, oldBlock-depth, "reorg")
emit system_event{level=warn, reason='reorg', depth, old_block, new_block}
```

After `cfg.reorg.settle_blocks` confirmations on the new head, a reorg-resolution worker:

```
for _, ex := range adapter.ListReorgPendingExecutions(chain) {
    receipt := rpc.GetTransactionByHash(chain, ex.TxHash)
    outcome := classifyReorgOutcome(receipt, ex)   // confirmed | dropped | mutated
    adapter.ReResolveExecutionAfterReorg(ex.ID, ex.TxHash, outcome)
}
```

The position monitoring loop (§ Layer 9) MUST skip exits for positions in `status='uncertain'` until they resolve. A reorg-uncertain position never converts to a forced loss without on-chain confirmation.

### 12.5 Evaluation Coverage Invariant (§ 4.11.E)

Atomic to every `execution_event` emission:

```
BEGIN TX
INSERT INTO events (..., type='execution_event', ...);
adapter.RecordExecutionForEvaluation(executionID, cfg.evaluation.deadline_sec);
COMMIT;
```

The evaluation worker calls `adapter.MarkEvaluationDone(executionID)` after consuming the event and producing its `evaluation_event`.

The janitor worker (orchestrator-spawned) runs every `cfg.evaluation.janitor_interval_sec`:

```
missing := adapter.ListMissingEvaluations(ctx)
for _, m := range missing {
    emit system_event{level=warn, reason='evaluation_missing', execution_id=m.ID, age_sec=m.AgeSec}
}
if same execution_id missing for 3 consecutive cycles:
    emit system_event{level=critical, reason='evaluation_worker_stalled'}
```

Coverage SLO: ≥ 99.9% over any 1h window. Below SLO triggers `cfg.alerts.evaluation_coverage_breach` Telegram alert.

### 12.6 Backpressure Control (§ 4.11.F)

The backpressure monitor (orchestrator-spawned) polls every `cfg.backpressure.poll_interval_sec`:

```
for each (chain, consumer):
    n := adapter.GetUnprocessedCount(chain, consumer)
    if n > cfg.events.ingest_pause_threshold AND chain not paused:
        ingestion[chain].Pause("backpressure")
        emit system_event{level=warn, reason='backpressure_pause', consumer, count=n}
    elif n < cfg.events.ingest_resume_threshold AND chain paused:
        ingestion[chain].Resume()
        emit system_event{level=info, reason='backpressure_resume', consumer, count=n}
```

Ingestion drop policy on full publish buffer:

```
if buffer.full():
    if policy == 'score_based':
        candidate := buffer.findMinScore(features=cfg.backpressure.drop_priority_features)
        if candidate.score < cfg.backpressure.drop_min_score:
            buffer.evict(candidate)
            adapter.RecordDrop(chain, "buffer_full_score_based", candidate.token, candidate.score)
        else:
            block until space (apply backpressure to source RPC)
    if policy == 'tail':
        buffer.evictNewest(); adapter.RecordDrop(...)
```

Pool-init events MUST bypass the drop policy. A dropped pool-init is a hard alert (`level=critical`).
