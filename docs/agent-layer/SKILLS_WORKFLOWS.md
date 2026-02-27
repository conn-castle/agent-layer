# Skills Workflow Guide

Use these sequences as defaults for common tasks. They prioritize deterministic planning, explicit approval gates, and verification before completion.

## Feature development
1. `continue-roadmap` to select active phase tasks and produce plan/task artifacts.
2. `review-plan` to critique scope, sequencing, and verification before coding.
3. Implement approved plan tasks.
4. `fix-tests` to drive checks to green.
5. `finish-task` to update memory files and summarize outcomes.

## Bug fix
1. Reproduce with a persistent automated test (red).
2. `fix-tests` to iterate on failing checks while implementing the fix.
3. Re-run repository checks and confirm green.
4. `finish-task` to record deferred risks and cleanup memory entries.

## Code review or quality audit
1. `find-issues` for report-first quality findings.
2. If docs drift is suspected, run `audit-documentation`.
3. Decide which findings to fix now vs defer to `ISSUES.md`.
4. If fixing now, use `fix-issues` with explicit approval and bounded scope.

## Notes
- `review-plan` discovers the active plan from `.agent-layer/tmp/*.plan.md` using standard `<workflow>.<run-id>.plan.md` naming and the latest valid run-id.
- Prefer one focused workflow at a time; chain only when outputs from one workflow become inputs to the next.
- Keep verification aligned with `docs/agent-layer/COMMANDS.md`.
