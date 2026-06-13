# Documentation

> **Start here.** The `docs/` root contains only this index, `REDIRECTS.md`, and eight folders — no loose spec files.

```
docs/
├── README.md          ← you are here
├── REDIRECTS.md       ← old path → new path (bookmarks)
├── reference/         ← canonical specs (architecture, DTOs, DB, orchestrator, roadmap)
├── guides/            ← how to work (starter, parallel dev, agents/skills)
├── ops/               ← living status (progress report)
├── plans/             ← executable task plans
├── specs/             ← pre-plan design brainstorms
├── analysis/          ← dated investigations & certifications
├── archive/           ← superseded chunks (architecture-context)
└── mockups/           ← UI mockups
```

---

## Quick navigation

| I want to…                 | Read                                                                                                                     |
| -------------------------- | ------------------------------------------------------------------------------------------------------------------------ |
| Understand the system      | [`reference/architecture.md`](reference/architecture.md)                                                                 |
| Implement a pipeline stage | [`reference/architecture.md`](reference/architecture.md) §3 + [`reference/dto_contracts.md`](reference/dto_contracts.md) |
| See build phase history    | [`reference/implementation_roadmap.md`](reference/implementation_roadmap.md)                                             |
| Run parallel development   | [`guides/PARALLEL_DEV.md`](guides/PARALLEL_DEV.md)                                                                       |
| Use agents and skills      | [`guides/AGENTS_AND_SKILLS.md`](guides/AGENTS_AND_SKILLS.md)                                                             |
| Get started                | [`guides/STARTER_GUIDE.md`](guides/STARTER_GUIDE.md)                                                                     |
| See what shipped           | [`ops/PROGRESS_REPORT.md`](ops/PROGRESS_REPORT.md)                                                                       |
| Execute a plan             | [`plans/README.md`](plans/README.md)                                                                                     |
| Build operator dashboard   | [`plans/2026-06-13-operator-dashboard-plan.md`](plans/2026-06-13-operator-dashboard-plan.md)                             |
| Review dated analysis      | [`analysis/README.md`](analysis/README.md)                                                                               |
| Old bookmark broken?       | [`REDIRECTS.md`](REDIRECTS.md)                                                                                           |

---

## Folder purposes

| Folder                     | Tier | What goes here                         |
| -------------------------- | ---- | -------------------------------------- |
| [`reference/`](reference/) | 1    | Canonical specs — **do not duplicate** |
| [`guides/`](guides/)       | 2    | Onboarding and workflow playbooks      |
| [`ops/`](ops/)             | 2    | Progress tracking, operational logs    |
| [`plans/`](plans/)         | 3    | Task-numbered implementation plans     |
| [`specs/`](specs/)         | 4    | Design specs before plans              |
| [`analysis/`](analysis/)   | 5    | Historical investigations (dated)      |
| [`archive/`](archive/)     | 6    | Superseded extracts                    |
| [`mockups/`](mockups/)     | —    | Static UI references                   |

---

## Rules

1. **One concept, one canonical file** — cross-reference; never copy sections.
2. **New plans** → `plans/YYYY-MM-DD-<topic>-plan.md`
3. **New design specs** → `specs/YYYY-MM-DD-<topic>-design.md`
4. **New analysis** → `analysis/` with date prefix or descriptive slug
5. **Never delete** — archive or document in `REDIRECTS.md`
6. **`reference/` wins** — if `archive/architecture-context/` disagrees with `reference/architecture.md`, reference wins.
