---
name: fix-ci
description: >-
  Explicit-only.
  Diagnose and directly repair failing checks on an open pull request, verify
  the repair locally, and return uncommitted changes and evidence to the caller.
---

# fix-ci

Diagnose and repair an observed pull-request CI failure locally. The caller
owns commits, pushes, remote reruns, and GitHub communication.

## Inputs and artifacts

Accept a pull request, CI run ID, or supplied evidence; otherwise use the
current branch's open pull request. Store downloaded artifacts under
`.agent-layer/tmp/ci-artifacts/<run-id>` and write
`.agent-layer/tmp/fix-ci.<run-id>.report.md`, using
`YYYYMMDD-HHMMSS-<short-rand>` for `run-id`.

## Workflow

1. Identify failing required checks from logs and artifacts. Compare workflow
   configuration and the CI environment with COMMANDS.md. Preserve run IDs,
   commands, exit status, useful output, and material environment differences.
2. Reproduce the failure locally with the failing command or closest documented
   equivalent. If it passes, reproduce the relevant CI-only difference rather
   than guessing.
3. Make the smallest root-cause repair, including directly required tests,
   configuration, documentation, or memory. Prove red-to-green behavior, run
   CI-equivalent affected checks, and inspect the diff.

Never weaken checks, lower thresholds, or include unrelated cleanup. When an
equivalent failure recurs, revise the causal model and gather discriminating
evidence rather than repeating a strategy; continue only when new evidence
supports a safe repair, and otherwise stop with `repeated-failure`. Consult
authoritative tooling or dependency documentation when local evidence is
insufficient.

If evidence proves an infrastructure or external failure, or no safe patch has
a credible reproducer, keep no speculative change and return
`remote-retry-needed` with rerun identifiers. Use `blocked` only for missing
required evidence or credentials, an unresolved contract, or a user decision.

## Completion contract

Report `ready-to-publish`, `remote-retry-needed`, `repeated-failure`, or
`blocked`, including the
failure evidence, reproducer, changes, red-to-green and affected-check results,
remote retry identifiers, and residual risk. Confirm no stage, commit, push, or
GitHub write occurred.
