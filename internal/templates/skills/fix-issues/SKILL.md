---
name: fix-issues
description: >-
  Resolve a selected ISSUES.md set through one reviewed plan, bounded
  implementation batches, one concrete-work review, and one verification pass.
---

# fix-issues

Resolve the selected issue set as one workflow. Batches organize mutations; they
do not each create a new planning, review, and verification cycle.

## Inputs and scope

- Accept explicit issue identifiers, a maximum count, a scope filter, or
  plan-only mode. Otherwise select every open ISSUES.md entry.
- Require `plan_reviewers` before side effects and pass them unchanged to
  `/plan-work`.
- Keep scope to selected issues and prerequisites that directly block them.
- Write `.agent-layer/tmp/fix-issues.<run-id>.report.md`, where `run-id` is
  `YYYYMMDD-HHMMSS-<short-rand>`.

Use a fresh built-in triage subagent to select, validate, and group the issue
set. Use fresh built-in implementers for independent packages where useful and
a fresh built-in verifier for final contract verification. Preserve plan-review
dispatch through `/plan-work`.

## Issue disposition contract

Every selected issue ends as exactly one of:

- `fixed`: resolved with evidence and removed from ISSUES.md
- `reclassified`: an end-user-visible capability moved to BACKLOG.md
- `deferred`: still valid but blocked by an explicit scope boundary or
  user-owned decision; leave the canonical ISSUES.md entry unchanged
- `rejected`: repository evidence proves the entry invalid or already resolved;
  remove it and record the evidence

Do not defer because a clear fix is inconvenient or spans multiple files. Log a
new out-of-scope engineering problem only when it is concrete, material, and not
already present.

## Workflow

### 1. Triage and batch once

Read ISSUES.md, then relevant ROADMAP.md, DECISIONS.md, COMMANDS.md, and code
evidence. Have the triage subagent:

- validate each selected entry against the current tree
- propose reclassification for clear feature requests
- merge duplicate selected entries into one repair obligation without losing
  their identifiers
- group the remaining work into ordered, coherent implementation packages
- identify user-owned decisions

Record these dispositions without editing the ledgers yet; Stage 6 applies them
after the selected contract is verified.

If no open issue remains in the selected scope, write a `no-work` outcome and
yield without planning.

If plan-only mode was requested, continue through planning and return the
reviewed artifacts without implementing.

### 2. Plan the selected set once

Run `/plan-work` once for the complete selected issue set and its packages:

```text
/plan-work
{selected issues, evidence, package map, and report path}
plan_reviewers are {agent 1, agent 2, ...}
```

Continue only with `implementation-ready`. Ask only the exact user-owned
decision returned by planning.

In plan-only mode, finalize the report with the selected set and reviewed
artifact paths, then yield here.

### 3. Implement all packages

Execute every package once in dependency order. A package implementer receives
the shared plan artifacts, its bounded issues, the relevant current tree, and
the issue disposition contract. Require a failing reproducer for a defect when
feasible; debt and refactors may use established contract evidence instead.

Run mutations sequentially when packages overlap. Resolve routine
implementation and focused-check details autonomously. Stop only for a concrete
failure or user-owned decision; do not silently drop the rest of the selected
set.

After all packages, run `/clean-and-fix-code` once only when the combined
uncommitted work contains meaningful cleanup scope.

### 4. Review the concrete result once

Run `/review-uncommitted-code` once over the selected issue changes and their
direct boundaries. Validate and directly address every `Recommended Accept`
finding with focused evidence. Do not start another broad review.

### 5. Verify once and update the ledger

Run `/verify-work` once in a fresh built-in subagent using the selected issues
and reviewed plan artifacts as the authoritative contract. Include accepted
review repairs as supplemental obligations.

Directly address material in-scope verification findings with focused evidence;
do not launch another plan, review, or verification pass. Then apply each issue
disposition to ISSUES.md or BACKLOG.md and finalize the report.

When no broader orchestrator owns closeout, run `/finish-task` once with current
evidence.

## Report and completion contract

The report contains:

1. `# Issue Resolution Summary`
2. `## Selected Issues and Packages`
3. `## Plan Artifacts`
4. `## Dispositions and Evidence`
5. `## Concrete-Work Review`
6. `## Verification`
7. `## Blockers and Residual Risk`

Return the report and delegated artifact paths, every selected issue's terminal
disposition, fixes, ledger changes, verification evidence, and any blocker.
The run is complete only when all selected issues are accounted for; each stage
runs once by default and then yields.
