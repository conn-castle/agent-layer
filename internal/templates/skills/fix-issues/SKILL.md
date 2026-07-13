---
name: fix-issues
description: >-
  Resolve a selected ISSUES.md set through a reviewed plan, bounded
  implementation packages, concrete-work review, and verification.
---

# fix-issues

Resolve a selected ISSUES.md set through one coherent plan and delivery.

## Inputs and dispositions

Accept issue IDs, a maximum count, a scope filter, optional plan reviewers, or
plan-only mode; otherwise select every open issue. Limit work to selected issues
and prerequisites. Delegate bounded work when useful, validate results against
the latest tree, and serialize mutations. Do not stage, commit, or push.

Write `.agent-layer/tmp/fix-issues.<run-id>.report.md`.

Every selected issue must end as:

- `fixed`: resolved with evidence and removed from ISSUES.md
- `reclassified`: moved to BACKLOG.md because it is an end-user capability
- `deferred`: valid but blocked by an external or contract constraint or user
  decision
- `rejected`: disproven or already resolved, and removed from ISSUES.md

Agent failure, inconvenience, or multi-file scope is not grounds for deferral.

## Workflow

1. Read ISSUES.md and relevant repository context. Validate entries, merge
   duplicate obligations without losing IDs, identify reclassifications and
   user decisions, and group dependency-ordered packages. Return `no-work` when
   nothing valid remains.
2. Run `/plan-work` once for the complete selected set and package map. In
   plan-only mode, return the reviewed artifacts without changing ledgers.
3. Implement every package against the latest tree. Use a failing reproducer
   for defects when feasible and established contract evidence for debt or
   refactors. Resolve routine details from repository evidence and do not drop
   work silently. Run `/clean-and-fix-code` when meaningful cleanup exists.
4. Run `/review-uncommitted-code` over the delivery and affected boundaries,
   repairing accepted findings. Run `/verify-work` against the issues and plan;
   repair material gaps and refresh invalidated evidence.
5. After verification, apply every disposition to ISSUES.md or BACKLOG.md using
   their formats. Update documentation or memory made stale by the work, merging
   duplicates and retaining only durable non-obvious decisions.

## Completion contract

Report the selected issues, packages, plan artifacts, disposition of every
issue, changes, ledger updates, review and verification evidence, blockers, and
residual risk. The workflow is complete only when every selected issue is
accounted for.
