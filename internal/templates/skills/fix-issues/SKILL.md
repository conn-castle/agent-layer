---
name: fix-issues
description: >-
  Resolve a selected ISSUES.md set through one reviewed plan, bounded
  implementation packages, one concrete-work review, and one verification pass.
---

# fix-issues

Resolve the selected issue set as one workflow. Packages organize mutations;
they do not create separate planning, review, or verification cycles.

## Inputs and disposition

Accept issue IDs, maximum count, scope filter, or plan-only mode; otherwise
select every open issue. Require `plan_reviewers`. Keep scope to selected issues
and direct prerequisites. Write
`.agent-layer/tmp/fix-issues.<run-id>.report.md`.

Use one fresh triage subagent, fresh package implementers when useful, and one
fresh final verifier. Serialize all mutations. Do not stage, commit, or push.

Every selected issue ends as:

- `fixed`: resolved with evidence and removed from ISSUES.md
- `reclassified`: end-user capability moved to BACKLOG.md
- `deferred`: valid but blocked by a genuine scope or user decision; unchanged
- `rejected`: evidence proves it invalid or already resolved; remove it

Do not defer clear fixes because they are inconvenient or multi-file.

## Workflow

### 1. Triage and plan once

Read ISSUES.md and relevant ROADMAP.md, DECISIONS.md, COMMANDS.md, and code. The
triage subagent validates entries, proposes reclassifications, merges duplicate
obligations without losing IDs, groups ordered packages, and identifies genuine
user decisions. Do not edit ledgers until verification. Return `no-work` when
nothing remains.

Run `/plan-work` once for the complete selected set and package map with
`plan_reviewers`; continue only with `implementation-ready`. In plan-only mode,
return the reviewed artifacts here.

### 2. Implement all packages

Execute packages once in dependency order against the latest tree. Give each
implementer the shared artifacts, bounded issues, and disposition contract.
Require a failing reproducer for defects when feasible; debt and refactors may
use established contract evidence. Resolve routine details without asking and
do not silently drop selected work.

After all packages, run `/clean-and-fix-code` once when meaningful cleanup scope
exists.

### 3. Review, verify, and update ledgers

Run `/review-uncommitted-code` once over the issue changes and direct boundaries;
validate and fix every `Recommended Accept` finding with focused evidence.

Run `/verify-work` once in a fresh subagent using the selected issues and plan
as the contract and review repairs as supplemental obligations. Directly repair
material in-scope findings without another plan, review, or verification pass.
Then apply every disposition to ISSUES.md or BACKLOG.md. When no broader
orchestrator owns closeout, use the current verification evidence and update
only documentation or memory made stale by the resolved issues. Read each
memory file's format, merge duplicates, and record only non-obvious durable
decisions. Do not add another verification or review stage.

## Completion contract

Report selected issues and packages, plan artifacts, every terminal disposition,
fixes and ledger changes, review and verification evidence, blockers, and
residual risk. The run is complete only when every selected issue is accounted
for.
