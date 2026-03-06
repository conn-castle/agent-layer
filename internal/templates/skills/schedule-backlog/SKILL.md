---
name: schedule-backlog
description: >-
  Propose reviewable backlog scheduling updates by mapping a coherent subset of
  backlog work into existing or new roadmap phases, cross-checking issue and
  decision impacts, and using judgment-based human checkpoints before any
  roadmap file edits.
---

# schedule-backlog

This is a backlog-scheduling workflow, not an implementation workflow.
Default behavior is proposal-first:
- evaluate a coherent backlog slice
- map it into roadmap phases
- summarize the proposal before mutating `ROADMAP.md`, `BACKLOG.md`, or `ISSUES.md`

## Defaults

- Default mode is proposal-only.
- Default proposal size is small to medium and reviewable.
- Prefer existing incomplete phases when they fit the work cleanly.
- Do not renumber completed phases.

## Inputs

Accept any combination of:
- a focus area, phase number, or text query
- a proposal size
- a maximum number of new phases
- whether issue-impact notes should be included
- whether approved changes should later be applied

## Required artifact

Write the proposal report to:
- `.agent-layer/tmp/schedule-backlog.<run-id>.report.md`

Use `run-id = YYYYMMDD-HHMMSS-<short-rand>`.
Create the file with `touch` before writing.

## Multi-agent pattern

Use subagents liberally when available.

Recommended roles:
1. `Standards reader`: extracts roadmap and decision constraints.
2. `Backlog triage lead`: selects candidate backlog items.
3. `Issue cross-checker`: identifies issue prerequisites or obsoletions.
4. `Roadmap integrator`: drafts the phase proposal.
5. `Reporter`: writes the proposal and approval guidance.

## Global constraints

- Do not implement backlog items in this workflow.
- Do not invent requirements that are not in `BACKLOG.md`, `ROADMAP.md`, `ISSUES.md`, `DECISIONS.md`, or `README.md`.
- Keep the proposal reviewable and coherent.
- Treat non-user-visible engineering work as issue-ledger material unless the roadmap explicitly needs a separate engineering phase.

## Human checkpoints

- Required: ask when a proposed placement would require renumbering a completed phase or making a non-obvious sequencing tradeoff.
- Optional: ask after the proposal only when the requested apply step would commit to a non-obvious prioritization or sequencing choice.
- Stay autonomous while reading, triaging, and drafting the proposal itself.

## Roadmap workflow

### Phase 0: Preflight (Standards reader)

1. Read, in order, when they exist:
   - `ROADMAP.md`
   - `DECISIONS.md`
   - `BACKLOG.md`
   - `ISSUES.md`
   - `README.md`

### Phase 1: Select the backlog slice (Backlog triage lead)

1. Respect any user focus first.
2. Otherwise choose a reviewable subset using:
   - roadmap alignment
   - priority
   - cohesion
   - dependency clarity
3. Separate any misclassified engineering-only items that belong in `ISSUES.md`, not the roadmap.

### Phase 2: Cross-check issue and decision impacts (Issue cross-checker)

For each candidate group, identify:
- prerequisite issues
- issues that would likely become obsolete
- decisions or roadmap constraints that affect scheduling

### Phase 3: Draft the proposal (Roadmap integrator)

For each suggestion, provide:
- target phase or new phase proposal
- included backlog items
- why the grouping fits
- prerequisites
- issue impacts

If a new phase is needed, include:
- preferred placement
- goal
- high-level tasks
- exit criteria

### Phase 4: Write the proposal report (Reporter)

The report must contain:

1. `# Roadmap Update Summary`
   - current roadmap shape
   - backlog slice reviewed
   - short recommendation summary
2. `## Suggestions`
   - labeled `A`, `B`, `C`, ...
3. `## Backlog Hygiene Notes`
   - duplicates or misclassified items
4. `## Open Questions`
   - only when a sequencing decision truly needs the human
5. `## Approval Options`
   - how the user can approve, reject, or modify each suggestion

## Guardrails

- Do not schedule a huge backlog sweep in one proposal.
- Do not bury prerequisites or sequencing risk.
- Do not mutate roadmap files in the same step as drafting the proposal unless the request already includes applying a clear, low-ambiguity proposal.
- Do not present engineering refactors as user-visible roadmap work unless the roadmap truly needs them there.

## Final handoff

After writing the report:
1. Echo the report path.
2. Summarize the best proposal options in chat.
3. Tell the user how to approve, reject, or modify each suggestion.
