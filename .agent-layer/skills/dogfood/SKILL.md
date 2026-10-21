---
name: dogfood
description: >-
  Explore Agent Layer through real tasks and inventive experiments in disposable
  projects; investigate failures, usability friction, and batch fixes.
  Use for product dogfooding in this repository, excluding skill-content
  evaluation and A/B benchmarks.
---

# Dogfood Agent Layer

Exercise the product through its public interfaces and inspect the resulting
state. Pursue useful real tasks, invent workflows, combine features, and follow
surprises. Look for confusing or missing capabilities as well as errors.
Use the current request to determine discovery, fix, and shipping scope.
Create disposable projects outside existing Git/Agent Layer roots, normally under
`/var/tmp/`; use nested projects only for deliberate discovery cases. Keep the
batch binary, harnesses, and evidence outside every cleanup/deletion target in a
unique retained directory under `.agent-layer/tmp/` or `/var/tmp/`.

When selecting coverage, read open
[GitHub issues](https://github.com/conn-castle/agent-layer/issues) and relevant
`docs/agent-layer/DECISIONS.md` entries. Use [the scenario menu](references/scenarios.md)
for ideas and known fragile boundaries; choose experiments beyond it too.
Consult [the source campaign](references/run-evidence.md) only when tracing a
lesson's provenance; its logs and historical approvals are not execution inputs.

## Candidate and workspace

- Build one fixed source snapshot per discovery batch and copy the binary into
  the retained batch directory so later development builds cannot replace it.
  Record its commit, uncommitted changes, build command, binary path/SHA-256,
  embedded version/commit, platform, and relevant provider versions. Give
  implementation workers separate checkouts.
- Invoke the candidate by absolute path and verify nested launchers and MCP
  servers use it. Follow `docs/DEVELOPMENT.md` for scratch source selection:
  set `AL_DEV_BYPASS_VERSION_DISPATCH=1`, `AL_DEV_EXECUTABLE` to the retained
  candidate, and `AL_DEV_SOURCE_ROOT` to each scratch project's own configuration
  root. Pins and PATH lookup can otherwise select an older release. Leave
  development overrides unset when testing real version dispatch.
- Reconnect owned test MCP servers after rebuilding: a running server retains
  its loaded code. Check the chat's orchestration-server identity separately
  before attributing its failures to the candidate.
- For cases without real-provider inference, use local bare Git remotes, dummy
  secrets, fake providers/local MCP servers, and child-only HOME/XDG/TMPDIR/Git
  configuration. Preserve unrelated files and processes with sentinels. Use
  ordinary disk storage for persistence cases; tmpfs misses fsync/publication
  behavior. `AL_NO_NETWORK=1` controls relevant Agent Layer downloads, not
  arbitrary network access.
- Run authorized native-provider calls separately in disposable projects with
  existing provider homes/sign-ins; do not copy credentials into fixtures.
  Record effective provider/model/effort and overrides. A substituted provider
  covers only that provider.

## Discovery batches

Use at most three concurrent discovery/implementation workers combined when
delegation is available and authorized; otherwise run the lanes sequentially.
Possible lanes, adaptable to the task:

1. Installation, upgrade, update, and recovery.
2. Wizard, configuration, imports, and projections.
3. Dispatch, MCP, and process lifecycle.

Choose a finite batch from real user goals, current risks, and coverage gaps.
For broad campaigns, spread exploration across product surfaces and leave room
to follow unexpected results. Adapt later batches as you learn, covering high-risk
boundaries affected by fixes. Give workers the candidate identity, owned
writable paths, selected cases, limits, and report requirements. Discovery workers
return evidence without production edits.

For directed checks, define the expected outcome; for open exploration, state the
user goal or question and refine checks as you learn. Retain argv, cwd, sanitized
environment differences, exit status, elapsed time, output, and relevant
before/after state in separate logs for each attempt. Redact real credentials,
tokens, and personal data, and omit unrelated file contents before retaining logs
or publishing issue evidence. Verify:

- File bytes, modes, kinds/link targets, parsed config/locks, and transaction
  status as relevant; preservation after success/refusal, repeat convergence,
  and failure recovery. Compare rollback with its documented snapshot scope,
  separately from generated sync projections.
- Actual overlap/interruption boundaries through held locks, commands gated by
  named pipes, product events, or PTY prompts. Bound timing-sensitive attempts;
  record gate misses.
- Dispatch session/handle continuity, invocation selection, answer publication,
  and durable termination. Track owned PID/start identities and descendants;
  check process groups, output EOF, and readers rather than inferring cleanup
  from a returned answer or exited parent.

Classify failures as product defects, harness/setup failures, documented behavior,
external blockers, or unresolved observations against current docs and CLI help.
Correct a broken fixture in a new attempt and preserve the original. Passing
retries do not explain the original failure. Count workflows separately from
assertions, polling commands, and unsuccessful setup attempts.

## Fix and repeat

Collect the batch's findings before grouping fixes. Each defect needs a minimal
public-interface reproduction, expected/actual behavior, affected boundary,
candidate identity, and evidence paths. Group related root causes or overlapping
changes; keep unrelated mechanisms separate. Escalate an unresolved choice to drop
a supported use case or change the intended public contract before that fix;
continue independent work.

For authorized fixes, use `implement` for substantive code changes and the normal
repository workflow for small corrections. Close exposed gaps in the normal CI
path, including native macOS/Linux coverage for platform-specific process behavior.

Recheck reproductions and adjacent preservation/recovery behavior against the
corrected binary. Use `ship-pr` when shipping is in scope; its prerequisites and
merge policy remain owned by that skill.

For discovery-only work, return the batch's findings. For an authorized fix loop,
integrate all implemented fixes from the batch, including both shipped and local
changes, into one tree. Record its identity, build the next candidate, and run
another finite batch over remaining gaps. Stop broad discovery when a batch
covers the chosen goals and affected high-risk boundaries without new material
defects and implemented fixes pass their rechecks. Stop earlier if the user's scope/time/cost
limit is reached or remaining cases are externally blocked; report that coverage
as incomplete. A help-only correction after a clean functional batch needs
targeted checks rather than another broad campaign.

Report candidate identity, workflows passed/failed/blocked, findings and fix/PR
status, unresolved observations, coverage gaps, evidence paths, and owned-process
cleanup. Preserve useful reproductions, search/update or file GitHub issues for
unresolved findings, and include their URLs even for discovery-only runs. Keep
redacted logs in the run directory and durable decision rationale in project memory.
Follow the repository's local issue-tracking instructions if filing fails.
For release-readiness claims, follow `docs/RELEASE.md`;
scratch or substituted-profile probes do not replace its live-checkout gates.
