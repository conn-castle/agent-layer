# Agent Review Prompt

Review a plan/task/context artifact set before implementation. This is a
report-only review of explicitly supplied artifacts, not a code audit and not
an implementation pass.

## Required inputs

The caller must provide paths to all three artifacts:

- a plan file
- a task file
- a context file

If any artifact path is missing, stop and ask for the missing path. Do not
discover, infer, or auto-select artifacts from `.agent-layer/tmp/`.

## Defaults

- Produce a report only. Do not modify implementation files, plan artifacts,
  task artifacts, or context artifacts.
- Treat the supplied artifact set as the review contract.
- Surface missing user decisions as findings or open questions; do not ask the
  user to resolve them during this report-only review.
- If any artifact path does not exist or cannot be read, stop and ask for a
  corrected path.

## Decision checkpoint standard

Use this standard for user-owned decisions:

- A user decision is required when repo evidence leaves multiple viable
  approaches and choosing one would commit the user to materially different
  behavior, public API, CLI behavior, compatibility, architecture, ownership
  boundaries, sequencing, rollout, scope, risk, cost, data migration, security
  or privacy posture, or destructive or irreversible work.
- A user decision is not required for routine implementation details,
  mechanical choices, verification selection, context gathering, or choices
  already settled by the user request, roadmap, DECISIONS.md, repo conventions,
  or supplied artifacts.

A plan satisfies this standard by recording the user-confirmed decision, citing
the source that already settles it, or narrowing scope so the decision is no
longer needed.

## Required artifact

Write the report to the child report path supplied by the orchestrator.

If no child report path was supplied, write to:

- `.agent-layer/tmp/review-plan.agent-review.<run-id>.report.md`

Use `run-id = YYYYMMDD-HHMMSS-<short-rand>` only for the fallback path. Create
the file before writing. Never write to or modify the parent
`.agent-layer/tmp/review-plan.<run-id>.report.md`.

## Multi-agent pattern

Use built-in subagents as independent review lenses when available. Keep
artifact loading, artifact summary extraction, final judgment, and report
synthesis with the current agent.

Recommended roles:

1. `Artifact alignment reviewer`: checks whether the plan, task list, and
   context file agree on objective, scope, non-goals, dependencies, sequencing,
   and exit criteria.
2. `Assumption reviewer`: looks for claims the artifacts treat as true without
   evidence, including API behavior, repo conventions, existing coverage,
   architecture constraints, command availability, migration safety, and
   compatibility.
3. `Decision reviewer`: looks for user-owned decisions under the decision
   checkpoint standard that have not actually been made.
4. `Experience reviewer`: looks for ways the plan could harm end-user
   experience, developer experience, operator experience, support burden, or
   maintenance paths.
5. `Verification reviewer`: stress-tests the test, docs, memory, and update
   expectations.

## Review workflow

Use an adversarial but evidence-backed posture: try to falsify the plan against
its stated objective, artifact set, and relevant repo constraints. Challenge
assumptions and look for plan-level hidden coupling, edge cases, and failure
modes.

### Phase 1: Extract artifact intent

From the plan, task, and context artifacts, extract:

- objective
- in-scope items
- explicit non-goals
- sequencing and dependencies
- promised tests or verification
- promised docs or memory updates
- user-confirmed decisions and still-open decision points
- exit criteria
- key files and entry point from the context file

### Phase 2: Evaluate with independent review lenses

Use the recommended review roles as independent lenses. Use repository and
memory files to validate concrete artifact claims, such as paths, sequencing,
roadmap constraints, or verification commands. Do not use that context to hunt
for unrelated implementation issues.

Check for:

- missing requirements or non-goals
- hidden large refactors
- dependencies ordered after dependents
- risky assumptions presented as settled
- user-owned decisions presented as settled, omitted entirely, or lacking a
  recorded user-confirmed choice under the decision checkpoint standard
- end-user, developer, operator, support, or maintenance experience risks
- roadmap, issue, backlog, or decision constraints that were missed
- context file gaps: missing key files, stale paths, invalid paths, or missing
  entry point
- weak or missing verification commands
- missing test work for risky changes
- missing docs or memory updates
- exit criteria that are subjective or not testable
- task list items that are too large or vague to execute safely

### Phase 3: Synthesize actionable findings

Each finding must include:

- `Title`
- `Severity`: Critical | High | Medium | Low
- `Confidence`: High | Medium | Low
- `Location`: exact artifact path and section
- `Why it matters`
- `Evidence`
- `Recommendation`

Every finding must be tied to the artifact set under review.

## Required report structure

The report must contain:

1. `# Plan Review Summary`
   - plan path
   - task path
   - context path
   - short outcome summary
2. `## Findings`
   - findings first, ordered by severity
3. `## Open Questions`
   - only unresolved items that block confidence
4. `## Strengths`
   - short list of what the plan does well
5. `## Recommendation`
   - `approve`
   - `approve-with-changes`
   - `revise`

## Guardrails

- Do not report vague "needs more detail" complaints without naming what is
  missing.
- Do not invent implementation problems or preferred alternatives that are not
  implied by the artifacts.
- If the plan is ambiguous, say so explicitly in findings or open questions
  instead of guessing intent.
- Do not ask the user to make plan decisions during review; report missing
  decision checkpoints as findings or open questions.
- Do not widen this into a code review.
- If a finding depends on an assumption, say so explicitly.

## Definition of done

- The report exists with every required section.
- Every finding names artifact path, section, severity, confidence, evidence,
  and specific recommendation.
- The report ends with exactly one recommendation: `approve`,
  `approve-with-changes`, or `revise`.
- Plan, task, and context artifacts were not modified.

## Final handoff

After writing the report:

1. Echo the report path.
2. Summarize the top findings in chat.
3. State the recommendation clearly.
