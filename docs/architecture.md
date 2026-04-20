# System Architecture

> Generated using `.github/prompts/architecture.prompt.md`.
> This is the master reference for the system. All implementation must conform to this document.

---

## 1. System Goal

<!-- Replace with your system's one-paragraph description -->

This system is a **deterministic, modular monolith pipeline** that processes [INPUT] through a sequence of staged transformations to produce [OUTPUT]. It runs as a single process with a database-backed state layer, enforcing strict idempotency and content-addressable identifiers throughout. The database engine is project-specific — see `docs/db_adapter_spec.md`.

---

## 2. Pipeline Stages

Define your pipeline as a strict sequential chain — never reorder, skip, or parallelize at runtime:

```
stage_1 → stage_2 → stage_3 → ... → stage_N
```

### Stage Definitions

| #   | Stage Name | Input DTO      | Output DTO   | Description                       |
| --- | ---------- | -------------- | ------------ | --------------------------------- |
| 0   | stage_1    | raw input      | Stage1Result | Initial processing and validation |
| 1   | stage_2    | Stage1Result   | Stage2Result | First transformation              |
| 2   | stage_3    | Stage2Result   | Stage3Result | Second transformation             |
| ..  | ...        | ...            | ...          | ...                               |
| N   | stage_N    | StageN-1Result | FinalOutput  | Final output generation           |

---

## 3. Module Breakdown

One module per pipeline stage, located under `app/modules/`:

```
app/modules/
├── stage_1/
│   └── ...              # exports: process(input, config) -> Stage1Result
├── stage_2/
│   └── ...
├── ...
```

### Module Rules

- Each module is a **pure function**: accepts DTOs, returns DTOs, no side effects
- No cross-module imports — only `contracts/` types
- No database access — orchestrator handles all DB operations
- No shared mutable state
- Config passed in as dict from YAML (by orchestrator)

---

## 4. Data Model

### Tables

#### pipeline_runs

| Column               | Type | Constraints                     |
| -------------------- | ---- | ------------------------------- |
| run_id               | TEXT | PRIMARY KEY                     |
| entity_id            | TEXT | NOT NULL, content-addressable   |
| status               | TEXT | DEFAULT 'started'               |
| last_completed_stage | TEXT | NULL (updated after each stage) |
| created_at           | TEXT | DEFAULT CURRENT_TIMESTAMP       |
| updated_at           | TEXT | DEFAULT CURRENT_TIMESTAMP       |

#### entities

| Column     | Type | Constraints               |
| ---------- | ---- | ------------------------- |
| entity_id  | TEXT | PRIMARY KEY, SHA256[:16]  |
| name       | TEXT | NOT NULL                  |
| status     | TEXT | DEFAULT 'created'         |
| created_at | TEXT | DEFAULT CURRENT_TIMESTAMP |

<!-- Add project-specific tables as needed -->

### ID Generation

All IDs are **content-addressable**:

```python
entity_id = SHA256(content_signature)[:16]
item_id   = SHA256(entity_id + unique_fields)[:16]
```

---

## 5. DTO Registry

See `docs/dto_contracts.md` for the complete registry with all fields and constraints.

Summary:

| DTO          | Source File            | Producer | Consumers |
| ------------ | ---------------------- | -------- | --------- |
| Stage1Result | `contracts/stage_1.py` | stage_1  | stage_2   |
| Stage2Result | `contracts/stage_2.py` | stage_2  | stage_3   |
| ...          | ...                    | ...      | ...       |

---

## 6. Configuration

All configuration lives in `config/pipeline.yaml`:

```yaml
pipeline:
  name: "my-pipeline"
  stages:
    - stage_1
    - stage_2
    - stage_3

paths:
  output_dir: "output/"
  database: "data/pipeline.db"

thresholds:
  max_retries: 3
  failure_abort_pct: 50
  min_disk_space_mb: 500

# Stage-specific configuration
stage_1:
  param_a: value
  param_b: value

stage_2:
  param_c: value
```

**Rules:**

- No hardcoded values in module code
- All thresholds, paths, and tunable parameters in YAML
- Config dict passed to modules by orchestrator

---

## 7. State Machine

### Pipeline Run States

```
started → processing → completed
                    → partial   (some entities failed)
                    → failed    (critical failure)
```

### Entity States

```
created → queued → processed → completed
                             → failed
```

**Rules:**

- No backward transitions
- `completed` and `failed` are terminal
- `failed` allows manual retry (bounded)

---

## 8. Failure Handling

| Condition                  | Threshold              | Action                            |
| -------------------------- | ---------------------- | --------------------------------- |
| Entity processing failures | > 50% fail             | Abort pipeline, status = `failed` |
| Optional stage failure     | Stage returns None     | Log WARN, continue with fallback  |
| Disk space exhaustion      | < 500MB remaining      | Abort pipeline                    |
| External process timeout   | > configurable seconds | Kill, retry once, then skip       |

See `docs/orchestrator_spec.md` for detailed retry and degradation policies.

---

## 9. Forbidden Technologies

> These defaults exist to prevent accidental complexity. Override when justified — document in this file.

| Category     | Default Forbidden                                                | Override             |
| ------------ | ---------------------------------------------------------------- | -------------------- |
| Architecture | Microservices, Kafka, RabbitMQ, Kubernetes, Docker orchestration | Unless project needs |
| Databases    | MongoDB, Redis, any distributed database                         | Unless project needs |
| AI/ML        | OpenAI API, Anthropic API, LangChain, AutoGPT, any paid LLM      | Unless project needs |
| Cloud        | AWS, GCP, Azure, any cloud compute or storage                    | Unless project needs |
| Runtime      | Agent loops, autonomous planners, event-driven architectures     | Unless project needs |
