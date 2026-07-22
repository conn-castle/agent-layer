---
name: review-plan
description: >-
  Explicit-only.
  Review and repair a plan/task/context artifact set with independent evidence,
  then report implementation readiness.
---

# review-plan

Review a plan independently; the owner synthesizes, edits, and decides
readiness.

## Required inputs

Require:

- one or more `plan_reviewers` as self-contained dispatch target specifications
- plan, task, and context artifact paths
- an optional specification artifact path

Each reviewer must supply an exact agent. Model and reasoning-effort overrides
are optional; when omitted, dispatch uses the configured Agent Layer value or
the agent CLI's default. Omit overrides that `al dispatch options` reports as
unsupported. Missing artifacts or an empty reviewer list block review.

## Workflow

1. Read all artifacts and confirm objective/scope alignment.
2. Render `references/agent-review-prompt.md` to
   `.agent-layer/tmp/review-plan.<run-id>.prompt.md`, replacing every input
   placeholder with its exact path or "not provided". Every reviewer receives
   this same prompt; do not assign complementary coverage.
3. Start one independent conversation per reviewer, retaining every returned
   handle. The following example supplies both optional overrides for an agent
   that supports them.

   ```bash
   al dispatch start --agent <reviewer-agent> --model <reviewer-model> \
     --reasoning-effort <reviewer-effort> \
     --prompt-file ".agent-layer/tmp/review-plan.<run-id>.prompt.md"
   ```

4. Run `al dispatch wait <handle>` for every reviewer and read each completed
   `result_path`. If a result is unusable, request its correction with
   `continue` on that same handle; never substitute a reviewer.
5. Validate findings against the artifacts and repository. Merge duplicates and
   retain material correctness, safety, scope, implementability, verification,
   or maintainability gaps.
6. Apply accepted corrections to the artifacts and update direct dependencies.
   Escalate only under the repository's human-escalation rules.

## Output

Write `.agent-layer/tmp/review-plan.<run-id>.report.md` using run ID
`YYYYMMDD-HHMMSS-<short-rand>` and preserve canonical reviewer results as
evidence. Include sources, accepted changes, unresolved decisions, and exactly
one value:

- `implementation-ready`
- `blocked-for-user-decision`

Finish only after evaluating every report and applying accepted corrections.
Return evidence paths, changes, genuine user decisions, and readiness.
