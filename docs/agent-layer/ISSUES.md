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

### Entry template
```text
- Issue YYYY-MM-DD abcdef: Short title
    Priority: Critical | High | Medium | Low. Area: <area>
    Description: <observed problem or risk>
    Next step: <smallest concrete next action>
    Notes: <optional dependencies/constraints>
```

## Open issues

<!-- ENTRIES START -->

- Issue 2026-02-18 auto-approve-names-duplication: buildClaudeSettings returns unused autoApprovedNames
    Priority: Low. Area: internal/sync
    Description: buildClaudeSettings computes and returns autoApprovedNames which WriteClaudeSettings discards. collectAutoApprovedSkills in sync.go recomputes the same list. The iteration logic overlaps but contexts differ (one builds permission patterns gated on !AllowMCP, the other always collects names for display).
    Next step: Drop []string return from buildClaudeSettings; keep collectAutoApprovedSkills as single source for display names.

- Issue 2026-02-16 test-coverage-parity: Local test coverage does not match GitHub Actions CI
    Priority: Medium. Area: testing / CI.
    Description: Test coverage reports generated locally (e.g., via `go test -cover`) do not align with the results produced in GitHub Actions. This makes it difficult to ensure coverage requirements are met before pushing code.
    Next step: Investigate the differences in coverage calculation (e.g., flags, environment, or tool versions) and provide a script or Makefile target that reproduces the CI coverage report locally.

- Issue 2026-02-16 skill-standard-rename: Rename slash-commands to skills and align with standard
    Priority: High. Area: slash-commands / skills.
    Description: Slash-commands should be renamed to "skills" to align with the established skill standard. This includes supporting supplemental folders within the skill directory and updating `al doctor` to verify compatibility using the standard toolset.
    Next step: Perform a global rename of slash-command terminology and implement structural/validation updates to match the skill standard.

- Issue 2026-02-16 upg-ver-diff-ignore: Ignore diffs for al.version during upgrades
    Priority: Low. Area: install / UX.
    Description: Upgrades currently show or warn about diffs for the `al.version` file. Since updating the version is the primary goal of an upgrade, this warning is redundant and potentially confusing for users.
    Next step: Modify the upgrade logic to specifically ignore the `al.version` file when calculating or presenting file diffs to the user.

- Issue 2026-02-15 upg-config-toml-roundtrip: Config migrations strip user TOML comments/formatting
    Priority: Medium. Area: install / UX.
    Description: `upgrade_migrations.go` decodes `.agent-layer/config.toml` into a map and re-marshals after key/default migrations, which removes user comments and original key ordering.
    Next step: Preserve comments/order for simple key migrations (line-level edit or AST-preserving strategy), or explicitly document this destructive formatting side effect.
    Notes: Explicitly deferred in the Upgrade continuation scope; wizard/profile flows now show rewrite previews and backup files before write.

- Issue 2026-02-14 upg-snapshot-scope: Upgrade snapshot captures all unknowns before deletion approval
    Priority: Low. Area: install / efficiency.
    Description: `createUpgradeSnapshot` in `upgrade_snapshot.go` captures all unknown files under `.agent-layer` before the upgrade transaction begins, regardless of whether the user later approves or rejects deletion. This is by design — the snapshot must exist before the transaction starts — but means snapshots may include files that were never at risk of deletion.
    Next step: Consider a two-phase approach where deletion-eligible unknowns are snapshotted lazily at deletion-prompt time, or document current behavior as intentional in upgrade docs.

- Issue 2026-02-14 upg-scoped-restore: Automatic rollback restores all snapshot entries instead of scoped targets
    Priority: Low. Area: install / correctness.
    Description: `rollbackUpgradeSnapshotState` computes scoped targets for the delete phase but `restoreUpgradeSnapshotEntriesAtRoot` restores all snapshot entries unfiltered. If a write fails on an unrelated entry during restore, the snapshot is marked `rollback_failed` even though that path was never part of the failed transaction scope. In practice this is safe (unmodified files are rewritten with identical content) but is an inconsistency.
    Next step: Filter the restore phase to entries covered by scoped targets, or document current full-restore behavior as intentional.

- Issue 2026-02-14 upg-rollback-audit-v1: Manual rollback success is not represented in snapshot status
    Priority: Low. Area: install / observability.
    Description: `al upgrade rollback <snapshot-id>` intentionally leaves successful rollbacks at `status: applied` because schema v1 has no dedicated manual-rollback-complete status.
    Next step: Add a schema/status extension for manual rollback auditability and update rollback/docs/tests accordingly.
    Notes: Current behavior still records manual rollback failures as `rollback_failed` with `failure_step=manual_rollback`.

- Issue 2026-02-14 upg-snapshot-size: No per-snapshot size guard for upgrade snapshots
    Priority: Low. Area: install / storage.
    Description: `internal/install/upgrade_snapshot.go` stores full file contents for rollback entries and retains up to 20 snapshots, but does not warn or cap snapshot size in unusually large repos.
    Next step: Add snapshot-size budget checks (warning and/or configurable cap) and document retention sizing guidance.

- Issue 2026-02-12 wiz-globals: Mutable exported globals and dead code in wizard package
    Priority: Low. Area: wizard / maintainability.
    Description: `internal/wizard/catalog.go:15-95` has mutable exported slice variables (e.g., `MCPServerCatalog`) that any caller can modify. Also, `approval_modes.go` and `helpers.go` contain unreferenced functions (`ApprovalModes`, `approvalModeHelpText`, `commentForLine`, `inlineCommentForLine`).
    Next step: Convert exported catalog variables to functions returning fresh copies; remove confirmed dead code.

- Issue 2026-02-12 stub-dup: writeStubWithExit test helper duplicated across 5+ packages
    Priority: Low. Area: testing / DRY.
    Description: The `writeStubWithExit` test helper (writes an executable stub script) is duplicated across `cmd/al`, `cmd/publish-site`, and other test packages with minor variations. `writeStubExpectArg` is also duplicated across `internal/clients/claude/launch_test.go` and `internal/clients/gemini/launch_test.go`.
    Next step: Consolidate into `internal/testutil` alongside `boolPtr`, `withWorkingDir`, and `writeStubExpectArg`.

- Issue 2026-02-12 envfile-asym: Asymmetric envfile encode/decode
    Priority: Low. Area: envfile / correctness.
    Description: `internal/envfile/envfile.go:113-130` handles quoting and escaping differently on encode vs. decode paths, meaning a round-trip (write then read) may not preserve values with embedded quotes, newlines, or special characters.
    Next step: Add round-trip property tests and align encode/decode paths.

- Issue 2026-02-12 3c5f958c: Duplicated boolPtr test helper across packages
    Priority: Low. Area: testing / DRY.
    Description: `boolPtr` is defined identically in `internal/install/upgrade_readiness_coverage_test.go` (38 uses) and `internal/sync/sync_extra_test.go` (1 use).
    Next step: Create `internal/testutil` package and consolidate.

- Issue 2026-02-12 3c5f958d: Duplicated withWorkingDir test helper across packages
    Priority: Low. Area: testing / DRY.
    Description: `withWorkingDir` is defined in `cmd/al/root_test.go` (25 uses) and `cmd/publish-site/main_test.go` (15 uses) with a minor error-handling difference.
    Next step: Consolidate into `internal/testutil` package using the fatal-logging variant.

- Issue 2026-02-12 3c5f958f: installer struct accumulating responsibilities
    Priority: Low. Area: install / maintainability.
    Description: The `installer` struct in `internal/install/` has 23 fields and 57+ methods spread across 8+ files. While logically grouped, this concentration increases coupling risk as features continue to be added.
    Next step: Audit current method count (Phase 11 is complete). If it exceeds ~70, extract sub-structs (e.g., `templateManager`, `ownershipClassifier`). Scheduled in Phase 13 (maintenance).

- Issue 2026-02-10 upg-ver: Cannot apply a specific intermediate release during upgrade
    Priority: Medium. Area: upgrade / version pinning / UX.
    Description: With `al upgrade` always running the currently installed binary (dispatch bypass) and no `al upgrade --version`, there is no supported way to upgrade a repo from an older pin to an intermediate version (for example 0.6.0 -> 0.6.1) when a newer version is installed (for example 0.7.0); the repo is forced to upgrade to the installed version or rely on manual pin edits/reinstalling.
    Next step: Decide on a supported workflow (`al upgrade --version X.Y.Z`, or an equivalent “dispatch/exec target version for upgrade” mechanism) and add tests + docs.

- Issue 2026-02-09 web-seo: Update website metadata, SEO, and favicon
    Priority: Medium. Area: website / marketing.
    Description: The website needs professional metadata, SEO optimization, and a proper favicon to improve visibility and professional appearance.
    Next step: Audit `site/` for missing meta tags and favicon, then implement them.

- Issue 2026-02-08 tmpl-mk: Slash-command templates reference non-existent Makefile targets
    Priority: Low. Area: templates / developer experience.
    Description: `finish-task.md`, `fix-issues.md`, and `cleanup-code.md` templates reference `make test-fast` and `make dead-code`. The templates already guard these with conditional language ("preferred when available", "only if already present"), but agents may still attempt them in repos that do not provide them.
    Next step: Consider whether the conditional language is sufficient, or whether a stronger guard (e.g., checking target existence before invocation) would reduce noise.
    Notes: Reconfirmed by documentation audit on 2026-02-18; keep this as a template-level guardrail issue (not a repo-local Makefile requirement).
