| Document                         | Purpose                                                                                                                                                                           |
| -------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `docs/architecture.md`           | **Single source of truth.** Unified architecture — control system, 10-layer pipeline, backbone, meta systems, KPIs, operational modes. All other docs must be consistent with it. |
| `docs/implementation_roadmap.md` | Phase-based implementation roadmap with schemas, algorithms, exit criteria, priority layers                                                                                       |
| `docs/orchestrator_spec.md`      | Orchestrator specification — execution model, checkpointing, resume, idempotency, failure handling                                                                                |
| `docs/dto_contracts.md`          | DTO definitions with all fields/types/constraints, cross-module dependency matrix, validation rules                                                                               |
| `docs/db_adapter_spec.md`        | Database abstraction layer — adapter interface, SQL compatibility, migration strategy, engine portability                                                                         |
| `docs/PARALLEL_DEV.md`           | Parallel development orchestration guide — 3-mode execution system, phase grouping, token optimization                                                                            |
| `docs/AGENTS_AND_SKILLS.md`      | Agent/skill system — agents, skills, composition matrices, token optimization, parallel dev integration                                                                           |
| `docs/STARTER_GUIDE.md`          | Getting started playbook — setup, architecture generation, roadmap generation, parallel system usage                                                                              |
| `docs/PROGRESS_REPORT.md`        | Implementation status — completed work, test results, remaining items, phase-by-phase progress tracking                                                                           |
| `contracts/`                     | Immutable DTO definitions — all modules MUST use these, not upstream sources or raw dicts/objects                                                                                 |
| `config/`                        | YAML configuration files — all thresholds, paths, and tunable parameters live here                                                                                                |

When generating code, refer to these documents for exact schemas, DTO definitions, interfaces, and algorithms. Do not invent new structures that contradict them.

---