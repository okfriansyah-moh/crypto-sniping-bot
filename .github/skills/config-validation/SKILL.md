---
name: config-validation
description: "Configuration validation and management. Use when creating, modifying, or reviewing YAML configuration files. Ensures no hardcoded values, correct config structure, and config-driven parameter management."
---

# Config Validation Skill

## Purpose

Enforce configuration-driven design. All tunable parameters, paths, thresholds, and magic numbers live in YAML config files under `config/`. No hardcoded values in module code.

## Rules

- All tunable parameters must be in `config/*.yaml`
- No hardcoded paths, thresholds, or magic numbers in Python code
- Config is loaded once at startup and passed to modules via `config: dict`
- Modules receive config from the orchestrator — they never read YAML directly
- `config/` is **append-only** — new keys allowed, existing keys never removed
- Config keys use `snake_case` naming

## Inputs

- YAML configuration files under `config/`
- Module code that consumes configuration
- `docs/architecture.md` — expected configuration structure

## Outputs

- Validated configuration files with correct structure
- No hardcoded values in module code

## Examples

### Correct Config Structure

```yaml
# config/pipeline.yaml
pipeline:
  stages:
    - name: stage_a
      enabled: true
      timeout_seconds: 300
  failure:
    max_entity_failure_pct: 50
    max_retries: 2
  paths:
    output_dir: "output/"
    temp_dir: "output/.tmp/"
```

### Correct Config Usage in Module

```python
# ✅ CORRECT — config passed in by orchestrator
def process(input_dto: InputDTO, config: dict) -> OutputDTO:
    timeout = config["pipeline"]["stages"][0]["timeout_seconds"]
    max_retries = config["pipeline"]["failure"]["max_retries"]
    ...

# ❌ FORBIDDEN — hardcoded values
def process(input_dto: InputDTO, config: dict) -> OutputDTO:
    timeout = 300  # Magic number
    output_path = "/tmp/output"  # Hardcoded path
    ...

# ❌ FORBIDDEN — module reads config file directly
import yaml
with open("config/pipeline.yaml") as f:
    config = yaml.safe_load(f)
```

## Checklist

- [ ] No hardcoded paths in Python code
- [ ] No hardcoded thresholds or magic numbers
- [ ] All tunable parameters live in `config/*.yaml`
- [ ] Config keys use `snake_case`
- [ ] Modules receive config from orchestrator, never read YAML directly
- [ ] No existing config keys removed (append-only policy)
