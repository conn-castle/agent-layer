---
name: fix-ci
description: >-
  Diagnose and directly repair failing checks on an open pull request, verify
  the repair locally, and return uncommitted changes and evidence to the caller.
---

# fix-ci

Own local diagnosis and repair of an observed pull request CI failure. Do not
commit, push, or wait for a new remote run; the caller owns publishing and remote
observation.

## Inputs

Optional inputs:

- pull request number or URL; default to the current branch's open pull request
- CI run ID
- caller-provided failure evidence

Use `run-id = YYYYMMDD-HHMMSS-<short-rand>` and store downloaded artifacts under
`.agent-layer/tmp/ci-artifacts/<run-id>`. Write the report to
`.agent-layer/tmp/fix-ci.<run-id>.report.md`.

## Rules

- Use failed logs and available artifacts together as the diagnostic source of
  truth.
- Build a credible local red reproducer before keeping a code fix.
- Treat a CI-only mismatch as a reproducibility defect and reproduce the
  material environment difference locally.
- Make the minimum root-cause fix directly, regardless of complexity.
- Do not weaken, skip, disable, or remove tests or checks, lower coverage, or
  include unrelated warnings and refactors.
- Stop before a fix that requires a user-owned behavior, architecture, scope,
  risk, or cost decision.
- Never stage, commit, push, or post to GitHub.

## Workflow

### 1. Diagnose

Identify the failing required checks. Read failed logs, download diagnostic
artifacts, inspect relevant workflow configuration, and compare the CI
environment with the documented local command in `COMMANDS.md`.

Preserve the check, run ID, command, exit status, relevant output or artifact,
suspected location, and material environment differences.

### 2. Reproduce locally

Run the failing command or closest documented equivalent. If it passes locally,
build a focused reproduction of the material CI difference.

If evidence identifies an infrastructure or external-service failure, or no
credible local reproducer can be built and no safe patch is justified, keep no
speculative change and return `remote-retry-needed`. Preserve the evidence the
caller needs to request one bounded rerun of the failed remote checks.

Use `blocked` instead when required evidence or credentials are unavailable, a
user-owned decision is required, or no supported remote retry path can be
identified for the caller.

### 3. Fix directly

Repair the root cause in the current working tree, including directly required
tests, documentation, configuration, or memory updates. Demonstrate the local
reproducer changing from red to green, then run the CI-equivalent checks for the
affected path and inspect the resulting diff.

If an evidence-equivalent local failure recurs, do not repeat the same repair
strategy. Revisit the causal model and add focused instrumentation or another
discriminating diagnostic when useful. Continue only when new evidence supports
a safe repair; otherwise stop with `repeated-failure`.

### 4. Report

Write:

1. `# CI Fix Summary`
2. `## Failure Evidence`
3. `## Local Reproducer`
4. `## Direct Repair`
5. `## Local Verification`
6. `## Changed Files`
7. `## Status and Residual Risk`

Use one status:

- `ready-to-publish`
- `remote-retry-needed`
- `blocked`
- `repeated-failure`

## Completion contract

Return the report path, local changes, red-to-green evidence, focused check
results, material diagnostic attempts, status, and any failed-check identifiers
needed for a remote retry. Confirm that no staging, commit, push, or GitHub
write occurred.
