# Scenario selection

Use these examples to spark experiments and revisit known fragile boundaries.
Invent cases beyond this menu, combine product surfaces, and follow surprises.

## Open-ended exploration

Choose a useful goal and let the workflow emerge: onboard an unfamiliar project,
have agents collaborate on a small deliverable, switch clients midway through
work, or recover a project after changing its setup. Try combinations the original
campaign never attempted. Observe whether the work is understandable and useful,
including awkward steps and missing capabilities even when every command succeeds.

Turn discoveries into focused reproductions or feature proposals as appropriate.

## Install, upgrade, update, and recovery

- Fresh initialization, existing-project refusal, repeated sync, and project
  discovery from nested directories and Git worktrees.
- Create historical installations with actual released CLIs whose downloads and
  checksums are verified. Select source versions from `site/docs/upgrades.mdx`,
  distinguishing migration applicability from the sequential release guarantee.
  Customize local instructions, config, memory entries, kept files,
  executable modes, and binary assets before upgrading.
- Verify planning does not mutate project state; apply preserves user content,
  records the expected pin/baseline/snapshot, and converges on repetition.
- Exercise manual rollback, failed apply with automatic rollback, interruption
  at observed snapshot/publication boundaries, malformed/truncated snapshots,
  rollback failure followed by repair/retry, and re-upgrade.
- Probe linked ancestors and special-file destinations using owned external
  siblings with sentinels. Verify refusal precedes unsafe writes/deletion; do not
  assume all user/client symlinks are forbidden. Use the state-directory and
  instruction-source rules in `site/docs/upgrades.mdx`.
- Exercise installer/update paths containing spaces, aliases, and existing
  destinations. Check download failure/cancellation, concurrent publication,
  previous-binary usability, and owned staging cleanup. Simulated Homebrew proves
  adapter behavior only; PTY checks must preserve real terminal job control.

## Wizard, configuration, imports, and projections

- Interactive wizard under a real PTY, answer files, profile preview/apply,
  invalid or unused answers, and cancellation. Check refusal does not leave
  partial config/env/projections; successful output must match persisted state.
- Overlap wizard edits/profile apply with instruction imports, sync, and
  upgrade. Gate a real operation while the project lock is held. Verify both
  successful operations have a valid serial outcome without silently losing an
  edit. Check independent projects can proceed while one project is locked.
- Exercise local and imported instruction ordering, duplicate-order refusal,
  add/pull/diff, staged conflict resolve, reset/remove, and local-bare-remote push.
  Assert config, lock, source trees, and projections agree. Skill import/adoption
  can be a product lifecycle fixture; evaluating external skill instructions is
  outside this workflow.
- Exercise client enable/disable and approval changes, user config and hook
  preservation, generated-hook enforcement, shared Codex settings, Claude merges,
  and environment substitution. Use dummy secret canaries and inspect redaction
  of normalized/escaped values as well as the original spelling.
- Check doctor/probe timeouts when children retain stdout or parents move process
  groups. Inspect hook ownership before removing anything that resembles a
  generated entry. Distinguish disabled-client stale artifacts from corruption
  according to current cleanup policy.
- Exercise malformed-source refusal/repair, repeated sync, missing files, and
  crash recovery at a publication checkpoint. Interpret the persistent empty
  `sync.lock` file separately from whether its OS lock remains held.

## Dispatch, MCP, and process ownership

- Fake-provider checks: reserve/start, duplicate starts, invalid selectors,
  concurrent waits, active-continuation refusal, failed output publication,
  worker crash recovery, reader backpressure, cancellation, and uncertain launch.
  Prove no provider ran before treating an interrupted launch as retryable.
- Native checks, only within the authorized call budget: fresh then continuation
  on the same handle/provider session, CLI-to-MCP handoff and reverse, server
  restart between calls, and failure/cancel near terminal publication. Keep each
  invocation ID so an old invocation cannot be mistaken for a newer continuation.
- Capture failure-time evidence that can distinguish fsync stalls, inherited
  pipes, reader backpressure, and a live leader in shutdown races. Do not increase
  timeouts without causal evidence.
- Optional bounded local I/O can probe load sensitivity, but a handful of
  generations is not a soak test. Label sampling limits when short-lived
  descendants may escape observation.
- Test local stdio/HTTP MCP servers, stream EOF, unread output, and server exit.

## Other product surfaces and release follow-through

Select CLI help/examples, shell completion, launcher literal-argument handling,
hook installation, cache/pin dispatch, and scratch cleanup when coverage warrants.
For benchmark-related changes, use readiness/dry-run/offline staging as relevant;
paid benchmark execution needs its own authorized scope. Catalog environment
certification does not prove native provider compatibility.

Release gates are owned by `docs/RELEASE.md`. After an authorized publication,
test the actual published assets/checksums, default/latest selection, and fresh
install/historical upgrade in disposable projects.
