---
name: schedule-backlog
description: >-
  Explicit-only.
  Map BACKLOG.md items into roadmap phases, checking issue and decision impacts
  and surfacing user-owned prioritization without making ambiguous roadmap
  edits.
  Do not use for implementation planning.
---

# schedule-backlog

Produce one coherent roadmap proposal from backlog evidence. Proposal-only is
the default; edit roadmap files only when the user has authorized a clear
prioritization and sequence.

## Inputs and evidence

Accept an optional focus, target phase, text query, proposal size, new-phase
limit, issue-impact preference, or instruction to apply an unambiguous result.
Prefer a cohesive reviewable slice and existing incomplete phases when they fit.
Never renumber completed phases.

Read ROADMAP.md, DECISIONS.md, BACKLOG.md, ISSUES.md, and README.md as relevant.
Do not invent requirements or implement scheduled work. Keep engineering defects
and refactors in ISSUES.md unless the roadmap genuinely needs an engineering
phase.

## Workflow

1. Select one cohesive slice using the user's focus, roadmap direction,
   priorities, dependencies, and shared outcome. Identify duplicates and items
   that belong in ISSUES.md.
2. Cross-check prerequisite or obsoleted issues, relevant decisions, and roadmap
   sequencing constraints.
3. Propose an existing or new target phase for each group, with rationale,
   prerequisites, impacts, and—when new—the phase placement, goal, high-level
   tasks, and exit criteria.
4. Present alternatives only for a substantive user-owned tradeoff, with pros,
   cons, and a recommendation. Routine placement within established direction
   does not require a checkpoint.
5. In proposal-only mode, return the proposal without edits. When application
   is already authorized and unambiguous, update ROADMAP.md, BACKLOG.md, and
   ISSUES.md once so each item has one canonical location. Otherwise leave files
   unchanged and return the smallest prioritization decision needed.

Return placement, rationale, prerequisites, issue impacts, backlog hygiene,
applied changes, and any decision still requiring approval. Use `None` for an
empty category.
