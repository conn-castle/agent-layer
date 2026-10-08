# Issues

Note: This is an agent-layer memory file. It is primarily for agent use.

## Purpose
Deferred defects, maintainability refactors, technical debt, risks, and engineering concerns. Add an entry only when you are not fixing it now.

## Format
- Insert new entries immediately below `<!-- ENTRIES START -->` (most recent first).
- Keep each entry **3–5 lines**.
- Line 1 starts with `- Issue YYYY-MM-DD <id>:` and a short title.
- Lines 2–5 are indented by **4 spaces** and use `Key: Value`.
- Keep **exactly one blank line** between entries.
- Prevent duplicates: search the file and merge/rewrite instead of adding near-duplicates.
- When fixed, remove the entry from this file.
- Describe the problem without choosing a solution or listing options.
- Use `Next step` only when the action is useful regardless of the eventual solution. Otherwise, use `Open question: <decision needed>`.

### Entry template
```text
- Issue YYYY-MM-DD short-slug: Short title
    Priority: Critical | High | Medium | Low. Area: <area>
    Description: <observed problem or risk>
    Next step: <smallest concrete next action>
    Notes: <optional dependencies/constraints>
```

## Open issues

<!-- ENTRIES START -->

- Issue 2026-10-04 shared-scratch-deletion-recovery: WARNING — repeated discovery cleanup deleted other sessions' scratch work
    Priority: High. Area: discovery / scratch ownership
    Description: Discovery agents repeatedly deleted shared `/tmp/alhunt` contents and overwrote `/tmp/alhunt/al`. Still unrecovered: original binary bytes (`al`, `al-old`, `al-cur`), generated repositories `r1`–`r3` and their Git metadata, `strace.txt`, and any unknown branch identity, uncommitted/untracked work, or unrecorded changes beyond the verified recovered contents.
    Evidence: Keep `/tmp/alhunt-recovery-d2dp71gh/RECOVERY.md` and `/tmp/alhunt-incident-recovery-881um2eg/RECOVERY.md` and their directories. The later report verifies 14,742 preserved surviving files, 1,032 historical file copies, and four later audit tests plus four overlays recovered from logged writes, with no checksum mismatches. Historical snapshots and replacement binaries do not establish complete recovery of either incident's unknown local work.
    Notes: Before discovery, create a fresh unique scratch directory with `mktemp -d` or equivalent atomic unique creation and record its exact path. Clean up only the exact directories created by the current task; never reuse, overwrite, or delete pre-existing/shared scratch paths, guessed paths, or wildcard matches. Do not remove either recovery directory or restore historical copies over active code or shared scratch paths.

- Issue 2026-10-06 codex-bundled-skills-cleanup-race: Repo-local Codex bundled-skill installation can race AL legacy cleanup.
    Priority: Medium. Area: Codex launch and sync.
    Description: An additional AL Codex pane failed before native startup because removing `.codex/skills` returned `directory not empty` while the native bundled-skill installer recreated that directory.
    Next step: Reproduce concurrent native cache installation and legacy cleanup, then distinguish Codex-owned cache from retired AL outputs.
    Notes: Verified native-acceptance-v8/same-pane-visible.json under `.agent-layer/tmp/herdr-daemon-fix`; ordinary legacy cleanup is in internal/sync/prompts.go. Recovery fixtures using bundled skills disabled do not prove this launch path.

## Preserved handoff notes

The following PRs merged on 2026-10-04. Historical scope, review-gate, and recovery details are retained at the user's request.

- Issue 2026-10-03 wizard-profile-preview-merged-pr-287: Wizard profile preview fix merged
    Priority: High. Area: wizard / profile
    Description: PR #287, https://github.com/conn-castle/agent-layer/pull/287, merged into main at 2026-10-04T12:55:08Z as 0e8d66495f234b843601730311a4566fe7277162, fixing installation before profile validation when `al wizard --profile` ran without `--yes` and `.agent-layer/config.toml` was missing. Prior handoff head: 4e7254a52b0e5fda4f331d0751a3039851c3a3ab; branch: fix/wizard-profile-preview-no-install.
    Notes: Scope validates the profile with config.ParseConfig before any write. Without `--yes`, a missing config writes nothing, prints WizardProfileInstallPreviewNote, and diffs an empty config. With `--yes`, install then backup, write, and sync, including recovery of a deleted config.toml. Partial-install guard, cleanup-backups, interactive wizard, and profile semantics stay out of scope. At the prior handoff, the gate rejected only the missing disposition reply to https://github.com/conn-castle/agent-layer/pull/287#issuecomment-5973706538, where CodeRabbit requested docstrings and no canonical reply such as `Disagreed.` had been posted.
    Status: Merged; preserve the handoff details and avoid a duplicate implementation.

- Issue 2026-10-03 skills-add-remove-report-merged-pr-283: Failed skill add/remove report fix merged
    Priority: High. Area: skills import
    Description: PR #283, https://github.com/conn-castle/agent-layer/pull/283, merged into main at 2026-10-04T12:39:33Z as c1c16f13e4bee1a21c5914af6c673a1925377bdd, fixing imported, retired, or unchanged skill reports and their repetition in errors when `al skills add` or `al skills remove` aborted without writing. Prior handoff head: ef15972b43b0224abe46d67744ef78a727c0c201; branch: fix/skills-add-remove-report-only-failures.
    Notes: Scope keeps only failed skill results on the three preflight stops and on `txn.Commit()` rollback, renders `al skills add failed` or `al skills remove failed`, and returns `no local state was changed because N skill(s) failed`. Pull and push partial success is unchanged. At the prior handoff, the gate rejected the missing disposition reply to https://github.com/conn-castle/agent-layer/pull/283#discussion_r4174347401, where Copilot requested commit-failure rollback and returned-report tests and no canonical reply had been posted.
    Status: Merged; preserve the handoff details and avoid a duplicate implementation.

- Issue 2026-10-03 mcp-secret-url-merged-pr-265: MCP URL secret warning fix merged
    Priority: High. Area: warnings / MCP policy
    Description: PR #265, https://github.com/conn-castle/agent-layer/pull/265, merged into main at 2026-10-04T12:20:36Z as 1c9fad6bc6f265d3d37bfed33c0577e34ea83ed7, fixing the skipped critical POLICY_SECRET_IN_URL warning when an MCP URL hid a literal secret behind a ${...} placeholder host or userinfo. Prior handoff head: 8ff8aaefac39feaf53434ff94d3a622774e0bea2; branch: fix/mcp-secret-url-placeholder-host.
    Notes: Scope scans the raw URL with envref.LiteralSecretQueryKey, flags a non-placeholder password or a non-placeholder username only when the password is empty or absent, leaves a literal username beside a placeholder password unflagged, ignores a ? after #, and still scans scheme-relative // userinfo. At the prior handoff, the gate rejected only the missing disposition reply to https://github.com/conn-castle/agent-layer/pull/265#issuecomment-5965625483, where CodeRabbit requested docstrings and no canonical reply such as `Disagreed.` had been posted.
    Status: Merged; preserve the handoff details and avoid a duplicate implementation.

- Issue 2026-10-02 codex-transient-error-merged-pr-252: Codex stream-retry dispatch fix merged
    Priority: High. Area: agent dispatch / Codex
    Description: PR #252, https://github.com/conn-castle/agent-layer/pull/252, merged into main at 2026-10-04T11:58:19Z as e8b43618b5850c743001329c394ea8770ce5151c, fixing dispatch failures from transient Codex `error` events during recovered stream retries. Prior handoff head: 58941e724d388262507daa0e84a3be086726f6d2; branch: fix/codex-transient-error-diagnostics.
    Notes: Scope keeps Codex `error` events as non-terminal progress diagnostics, leaves `turn.failed` and `turn.aborted` terminal, and appends the last provider error on a nonzero exit or a missing terminal result. At the prior handoff, the gate rejected only the missing disposition reply to https://github.com/conn-castle/agent-layer/pull/252#issuecomment-5959623988, where CodeRabbit requested docstrings and no canonical reply such as `Disagreed.` had been posted.
    Status: Merged; preserve the handoff details and avoid a duplicate implementation.

- Issue 2026-10-02 wizard-mcp-subtables-merged-pr-251: Wizard array sub-table fix merged
    Priority: High. Area: wizard / TOML patching
    Description: PR #251, https://github.com/conn-castle/agent-layer/pull/251, merged into main at 2026-10-04T11:36:06Z as ca11a3ed860f45f93d00bcfc84391ae05ceda0db, fixing moved or dropped section-style MCP server sub-tables. Prior handoff head: dac31d411d78c2c5767ec3048116af06b1f460f7.
    Notes: The fix covers repeated env/headers, nested arrays, intervening tables, per-server ownership, and sanitizer comment preservation. The prior handoff preserved PR #251 and branch fix/wizard-mcp-server-subtables; its gate rejected only the missing disposition reply to https://github.com/conn-castle/agent-layer/pull/251#issuecomment-5958185934.
    Status: Merged; preserve the implementation handoff at .agent-layer/tmp/planner-handoff-recovery/implementation_input.md and recovery branch recovery/auto-skill-loop-20261002-toml-subtables at a405bcf3d294f8b3ea059a49b8f8a966e0412ef9; avoid a duplicate implementation.
