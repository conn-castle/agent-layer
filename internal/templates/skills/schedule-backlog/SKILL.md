---
name: schedule-backlog
description: >-
  Map BACKLOG.md items into roadmap phases, checking issue and decision impacts
  and stopping for user-owned prioritization before ambiguous roadmap edits.
  Do not use for implementation planning.
---

# schedule-backlog

Produce one coherent roadmap proposal from backlog evidence. Apply it only when
the user has authorized the resulting prioritization and sequencing.

## Scope and inputs

- Default mode is proposal-only.
- Accept a focus area, phase, or text query; a proposal size; a maximum number
  of new phases; whether issue impacts are included; and whether an already
  clear proposal should be applied.
- Prefer a small or medium coherent slice and existing incomplete phases when
  they fit.
- Never renumber completed phases.

## Evidence and decision contract

- Read ROADMAP.md, DECISIONS.md, BACKLOG.md, ISSUES.md, and README.md as needed,
  in that priority order.
- Do not invent requirements or implement scheduled work.
- Keep engineering-only defects and refactors in ISSUES.md unless the roadmap
  genuinely needs an engineering phase.
- Sequencing and prioritization that materially change committed direction are
  user-owned decisions. Routine placement within an already established
  direction is not.

## Workflow

### 1. Select one backlog slice

Respect the user's focus. Otherwise select a reviewable, cohesive subset using
current roadmap alignment, stated priority, dependencies, and shared outcome.
Identify duplicates and items that belong in ISSUES.md.

### 2. Cross-check constraints once

For each proposed group, identify prerequisite issues, issues the work would
obsolete, relevant decisions, and roadmap sequencing constraints.

### 3. Draft one proposal

For every suggestion, provide:

- target existing phase or proposed new phase
- included backlog items and grouping rationale
- prerequisites and issue impacts
- for a new phase: placement, goal, high-level tasks, and exit criteria

Present alternatives only where a substantive user-owned tradeoff actually
exists. Include pros, cons, and a recommendation for each such choice.

### 4. Apply or yield

- In proposal-only mode, return the proposal and stop for approval.
- If the request already authorizes a clear, low-ambiguity application, update
  ROADMAP.md, BACKLOG.md, and ISSUES.md once so each item has one canonical
  location.
- If application would choose a non-obvious priority or sequence, stop for the
  smallest user decision before editing.

## Final summary

Return:

1. `# Roadmap Update Summary`
2. `## Proposal`
3. `## Backlog Hygiene`
4. `## Decisions Needed`
5. `## Applied Changes` or `## Approval Needed`

Use `None` for empty sections.

Return the proposal or applied outcome after one coherent slice is evaluated
against roadmap, issue, and decision evidence. Name placement, rationale,
prerequisites, issue impacts, and any prioritization decision still required.
