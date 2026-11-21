# Agent Layer repository conventions

## Issue tracking

Track this repository's defects and deferred engineering work in
[GitHub Issues](https://github.com/conn-castle/agent-layer/issues). Shared skills'
`ISSUES.md` and `fix-issue-log` references mean this tracker here; do not recreate
the local issue ledger. Search before filing, include reproducible evidence, and
report issue URLs. If filing fails, retain findings in task scratch and report
them as unfiled. Close issues when their requirements are complete.

Keep planned features in `docs/agent-layer/BACKLOG.md` and decisions in
`docs/agent-layer/DECISIONS.md`. This override is repo-only; preserve upstream
skills and other projects' `ISSUES.md` support.

## Local working files

<!-- BEGIN GENERATED PATHS -->

- Keep agent plans, reports, handoffs, and retained evidence under `.agent-layer/working/`.
- Put regenerable downloads, build outputs, and disposable intermediate files under `.agent-layer/cache/`.
- Create task worktrees under `.agent-layer/worktrees/` and use Git to manage them. Native harness-managed worktrees follow the harness's configured location.

<!-- END GENERATED PATHS -->
