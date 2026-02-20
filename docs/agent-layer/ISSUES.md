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

- Issue 2026-02-20 exit-code-flatten: Subprocess exit codes flattened to 1 by all client launchers
    Priority: Medium. Area: clients / UX.
    Description: `al claude`, `al gemini`, `al codex`, etc. all exit with code 1 regardless of the subprocess's actual exit code. The subprocess error is wrapped via `fmt.Errorf` and cobra's top-level handler calls `os.Exit(1)` for any error. E2E test confirmed: mock exits 42, al claude exits 1. Users and scripts cannot distinguish between different failure modes based on exit code.
    Next step: Type-assert `*exec.ExitError` in launch code, extract `.ExitCode()`, and pass it to `os.Exit()` for subprocess failures. Update e2e test to assert propagated code.

- Issue 2026-02-20 wizard-profile-silent-overwrite: Wizard profile mode silently overwrites corrupt config without detection or warning
    Priority: Low. Area: wizard / UX.
    Description: `al wizard --profile X --yes` reads the existing config.toml as raw bytes (never parses as TOML), shows a diff preview, and overwrites with the profile. It does not detect or warn about TOML corruption. This is by design (profile mode is a forceful replacement), but may surprise users who don't realize their custom config was lost.
    Next step: Consider adding a warning when the existing config fails TOML parsing, so users know they're replacing a corrupt file. The backup (config.toml.bak) is already created.

- Issue 2026-02-19 upg-snapshot-recover-version-target: Snapshot recovery restores to prior version, not upgrade start version
    Priority: High. Area: install / rollback correctness.
    Description: During upgrade flows that create a snapshot, invoking recovery does not restore the environment to the version at upgrade start; it restores only to the immediately prior version state.
    Next step: E2E scenario 055 covers the basic upgrade/rollback path and asserts version restoration. Investigate whether there are edge cases where "prior version" differs from "upgrade-start version" and add a dedicated test for that distinction if so.

- Issue 2026-02-19 toml-multiline-state-dup: Multiline TOML state tracking duplicated across 7 functions in wizard/patch.go
    Priority: Low. Area: wizard / maintainability.
    Description: The pattern of checking `IsTomlStateInMultiline(state)` / `ScanTomlLineForComment(line, state)` is repeated in `extractMCPBlockKeyValue`, `removeKeyFromBlock`, `findKeyLine`, `parseKeyLineWithState`, `replaceOrInsertLine`, `findInsertIndex`, and the bracket-depth loop in `multilineValueEndIndex`. v0.8.3 added two more instances. A shared line iterator that advances state and yields parsed lines would consolidate this.
    Next step: Extract a `tomlBlockIterator` or equivalent that encapsulates state tracking and yields non-multiline-content lines, then refactor all 7 call sites.

- Issue 2026-02-18 config-new-field-checklist: Future new required config fields must include migration manifest entry
    Priority: Medium. Area: config / backwards compatibility.
    Description: v0.8.1 added `agents.claude-vscode.enabled` as required, breaking pre-v0.8.1 configs. Fixed by config resilience (lenient parsing + interactive upgrade prompts). Any future new required field must include a `config_set_default` operation in the release's migration manifest (`internal/templates/migrations/<version>.json`).
    Next step: Add a CI lint or code review checklist item that enforces: any new nil-check in `Validate()` must have a matching `config_set_default` operation in the migration manifest.

- Issue 2026-02-18 warn-deterministic-order: MCP collision warning output is non-deterministic
    Priority: Medium. Area: warnings / determinism.
    Description: `internal/warnings/mcp.go:168` iterates `toolNames` (a map) directly when generating `CodeMCPToolNameCollision` warnings. Map iteration order is non-deterministic, producing unstable CLI output.
    Next step: Sort collision subjects before appending warnings; add a deterministic ordering contract test for `CheckMCPServers` output.

- Issue 2026-02-18 race-target: No canonical race-check command in workflow docs
    Priority: Low. Area: testing / CI hygiene.
    Description: Race checks pass today but there is no documented repeatable command in COMMANDS.md. Race testing is ad hoc instead of a repeatable workflow step.
    Next step: Add a Makefile target (e.g., `make test-race`) targeting concurrency-critical packages and document it in COMMANDS.md.

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

- Issue 2026-02-12 wiz-dead-code: Dead code in wizard package
    Priority: Low. Area: wizard / maintainability.
    Description: `approvalModeHelpText()` in `notes.go` is only exercised by its own test but never called in production code. `commentForLine`/`inlineCommentForLine` in `tomlutil.go` are used internally and are NOT dead code.
    Next step: Remove `approvalModeHelpText` and its test if the function is confirmed unused.
    Notes: Mutable exported globals issue resolved by config field catalog refactor (Decision config-field-catalog).

- Issue 2026-02-12 stub-dup: writeStub/writeStubWithExit test helpers duplicated across 5+ packages
    Priority: Low. Area: testing / DRY.
    Description: `writeStub` is duplicated across 5+ test files (`internal/clients/vscode`, `claude`, `antigravity`, `gemini`, `cmd/al`). `writeStubWithExit` is similarly duplicated across `cmd/al`, `cmd/publish-site`, and others. `writeStubExpectArg` is duplicated in `internal/clients/claude` and `gemini`.
    Next step: Consolidate all three (`writeStub`, `writeStubWithExit`, `writeStubExpectArg`) into `internal/testutil` alongside `boolPtr` and `withWorkingDir`.

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

- Issue 2026-02-10 upg-ver: Cannot target a specific intermediate release during upgrade
    Priority: Low. Area: upgrade / version pinning / UX.
    Description: Migration manifests are now chained during multi-version jumps (all intermediate manifests between source and target are loaded and applied in order), so migrations are no longer missed. However, there is still no `al upgrade --version X.Y.Z` to target a specific intermediate version; the repo always upgrades to the installed binary version.
    Next step: Decide if `al upgrade --version X.Y.Z` is needed given that migration chaining resolves the data-loss risk.

- Issue 2026-02-09 web-seo: Update website metadata, SEO, and favicon
    Priority: Medium. Area: website / marketing.
    Description: The website needs professional metadata, SEO optimization, and a proper favicon to improve visibility and professional appearance.
    Next step: Audit `site/` for missing meta tags and favicon, then implement them.

- Issue 2026-02-08 tmpl-mk: Slash-command templates reference non-existent Makefile targets
    Priority: Low. Area: templates / developer experience.
    Description: `finish-task.md`, `fix-issues.md`, and `cleanup-code.md` templates reference `make test-fast` and `make dead-code`. The templates already guard these with conditional language ("preferred when available", "only if already present"), but agents may still attempt them in repos that do not provide them.
    Next step: Consider whether the conditional language is sufficient, or whether a stronger guard (e.g., checking target existence before invocation) would reduce noise.
    Notes: Reconfirmed by documentation audit on 2026-02-18; keep this as a template-level guardrail issue (not a repo-local Makefile requirement).
