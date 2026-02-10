# Upgrade UX and Risk Plan

As of: 2026-02-10  
Scope: `conn-castle/agent-layer` (CLI, templates, sync/projection, launchers, docs/release flow)

## Purpose

This document is a standalone plan for reducing upgrade risk and making upgrade behavior predictable for users. It captures:

1. current behavior grounded in code,
2. a comprehensive pain-point catalog,
3. confirmed scope decisions,
4. phased mitigation work ordered by user impact.

It is intentionally independent from `ROADMAP.md` sequencing and issue triage details.
The canonical user-facing upgrade contract now lives in `site/docs/upgrades.mdx`; this file remains the internal implementation-planning document.

## Current Upgrade Behavior (Code Truth)

- Global CLI with repo pin dispatch: `cmd/al/main.go`, `internal/dispatch/*`
- Repo pin file: `.agent-layer/al.version` (`internal/install/install.go`, `internal/dispatch/pin.go`)
- Upgrade/update checks: `internal/update/check.go`, `internal/updatewarn/warn.go`, `cmd/al/init.go`, `cmd/al/upgrade.go`, `cmd/al/doctor.go`, `cmd/al/wizard.go`
- Template seeding/overwrite/delete-unknown logic: `internal/install/*`
- Client output regeneration: `internal/sync/*`
- Config/env validation and substitution: `internal/config/*`, `internal/envfile/envfile.go`
- Wizard rewrite behavior and backups: `internal/wizard/*`
- Installers and release artifacts: `al-install.sh`, `scripts/build-release.sh`, `.github/workflows/release.yml`

## Comprehensive Pain-Point Catalog

### 1. Installation and first upgrade

1. Unsupported platform/arch hard-stop (for example unsupported OS/CPU combos).
2. Installer path is shell-oriented and assumes baseline Unix tooling, which can fail in minimal/containerized environments.
3. Install requires external tools (`curl`, `sha256sum`/`shasum`) and fails when missing.
4. PATH setup is manual; users can install successfully but still get `al: command not found`.
5. Completion install is skipped in non-interactive or unsupported shell contexts; users may expect completion to “just work.”
6. Version string validation is strict (`X.Y.Z`/`vX.Y.Z` only), so prerelease expectations can fail.
7. Corporate proxies/mirrors can break checksum/download flow even when users did nothing wrong.
8. Release artifacts/install script target matrix can lag newer macOS/Linux hardware expectations.
9. Completion support is limited to bash/zsh/fish; teams using other shells cannot rely on one standardized completion workflow.

### 2. Version pinning and dispatch lifecycle

1. **[Resolved in Phase 10 work]** Empty/invalid `.agent-layer/al.version` no longer blocks command execution. Dispatch treats empty/corrupt pins as "no pin" (falls through to the current binary with a warning), and `al init`/`al upgrade` auto-repair empty/corrupt pin files without requiring prompts. Keep regression coverage to prevent re-introduction.
2. `AL_NO_NETWORK=1` + uncached pinned version causes hard failure with a message that names the cache path and env var, but does not suggest how to resolve it (for example pre-populating the cache or unsetting the variable).
3. Cache path permission problems (`AL_CACHE_DIR` or user cache dir) block dispatch.
4. Override precedence (`AL_VERSION` > repo pin) can surprise teams expecting repo lockstep. A developer with `AL_VERSION` in their shell profile will silently bypass the repo pin on every command with no visible indication.
5. Dev builds do not pin by default, so teams can drift unintentionally.
6. Upgrade checks depend on the unauthenticated GitHub API (60 requests/hour rate limit) and can warn/fail noisily in CI, shared networks, or restricted environments.
7. Old versions can leak `AL_SHIM_ACTIVE` into child shells (documented historical regression).
8. Historical command renames across versions (for example legacy `install` vs current `init`) can break old scripts.
9. **[Resolved in Phase 10 work]** Update warnings suggest `al upgrade plan` and `al upgrade` (or `al upgrade --force`). Keep regression coverage to prevent guidance drift.
10. **[Resolved in Phase 10 work]** `ensureCachedBinary` now emits "Downloading al vX.Y.Z..." / "Downloaded al vX.Y.Z" progress lines to stderr. Keep regression coverage to prevent re-introduction.
11. **[Resolved in Phase 10 work]** `al init --version X.Y.Z` now validates the release exists before writing the pin file, returning a clear not-found message instead of a cryptic 404 on the next invocation. Keep regression coverage to prevent re-introduction.
12. **[Intentional]** Pinning is required for supported repos. Disabling pinning (for example by deleting `.agent-layer/al.version`) is unsupported and should not be documented for end users.
13. Pin file format does not support comments. Users accustomed to `.gitignore`-style files may add `# comments`, which causes the entire file to fail semver validation.
14. Binary download uses a fixed 30-second HTTP timeout (`cache.go`). Large binaries on slow connections can time out with no retry.

### 3. `al upgrade` template upgrade experience

1. No built-in migration engine for renamed/deleted managed templates (stale orphans risk).
2. Diff prompts do not clearly separate "my customization" vs "upstream template change" (known issue).
3. Per-file upgrade prompts require an interactive terminal (`upgrade.go`); users must choose destructive `--force` or do manual interaction. There is no CI-safe middle ground (for example `--yes --apply-managed-updates` without `--apply-deletions`).
4. `--force` can delete unknown files under `.agent-layer` (`install_unknowns.go`), which is high-risk in mixed/manual setups. The flag name does not communicate the deletion behavior.
5. Large prompt sets are cognitively heavy when many files differ.
6. Memory files (`docs/agent-layer/*`) prompt separately with a distinct "overwrite all memory?" question; users who said "yes" to managed files may not understand why they're prompted again or may assume it was already covered.
7. Unknown-file cleanup only applies under `.agent-layer`; stale docs/memory files can still drift.
8. Invalid `gitignore.block` format (managed markers/hash present) hard-fails sync.
9. Users can customize `.agent-layer/gitignore.block` but may not realize re-running `al sync` is required to apply it to the root `.gitignore`.
10. There is no first-class template source/pinning workflow for alternate template repositories, so teams can struggle to keep forked templates deterministic across upgrades.
11. **[Resolved in Phase 10 work]** `al init --version X.Y.Z` now validates the release exists on GitHub before writing the pin, returning a clear not-found message instead of silently writing a bad pin.

### 4. Config and env compatibility over time

1. Required booleans (`enabled`) and strict transport rules cause fail-fast breakage when legacy files are incomplete.
2. Small schema changes (approval values, transport fields, client IDs) can block all commands.
3. Reserved MCP id (`agent-layer`) is forbidden; collisions fail hard.
4. Only `AL_` keys are loaded from `.agent-layer/.env`; historical non-prefixed keys stop working.
5. Missing env placeholders fail hard on substitution.
6. Process env overrides `.env`, which can create “works on my machine” drift across teammates/CI.
7. Empty env values are ignored; users may think they cleared a secret but runtime still uses process env.
8. Path expansion is selective (`~`/`${AL_REPO_ROOT}`), so relative-path expectations can differ by server config style.
9. Parse-time unknown-key enforcement is not explicit, so config typos can be harder to diagnose than strict schema errors.

### 5. Client projection differences (expectation mismatch)

1. Same MCP server projects differently per client (placeholder syntax, headers, env handling).
2. Codex requires resolved URL/command/args/env in generated config; users may expect placeholders to remain.
3. Codex header rules reject mixed placeholder strings (for example `Token ${VAR}`) for some header forms.
4. VS Code/Codex/Claude/Gemini capabilities are not symmetrical; one config may behave differently across clients.
5. Antigravity supports instructions/skills only (no MCP/approvals), so “enabled everywhere” expectations break.
6. Internal prompt server command resolution depends on `al` on PATH or a working Go toolchain/source fallback.
7. Some default template MCP commands use floating tool versions (`@latest`), so behavior can change outside Agent Layer release cadence.
8. Disabling an agent in config skips that client’s sync writers (`sync.go` enabled-gated steps), so previously generated files for that client can remain stale in the repo and confuse users.

### 6. VS Code and launcher-specific upgrade pain

1. Launchers depend on both `al` and `code` being on PATH.
2. Codex auth is per `CODEX_HOME`; switching repos requires re-auth and can feel like a regression.
3. `al vscode --no-sync` can launch with stale generated config if users forget to sync first.
4. Managed block behavior in `.vscode/settings.json` can be non-obvious when users heavily customize settings/comments.
5. First-launch slowness in some environments remains an open UX issue.
6. Generated launchers call `al vscode --no-sync`, so stale-config risk affects normal launcher usage, not only manual CLI invocations.

### 7. Wizard-driven upgrades

1. Wizard rewrites `config.toml` in canonical template order (user formatting/layout drift).
2. Inline comments may move/disappear from edited lines.
3. Wizard is terminal-only; remote/non-interactive environments cannot use it.
4. Secrets flow can disable servers during prompts, which may feel implicit if users skip carefully.
5. Backups (`.bak`) accumulate and can clutter repos if teams do frequent wizard runs.

### 8. Sync/doctor behavior during upgrade cycles

1. `al sync` returns non-zero (`ErrSyncCompletedWithWarnings` in `sync.go`) when any warnings exist, breaking CI/automation that expects "warnings != failure." There is no `--warn-only` or exit-code override.
2. `al doctor` treats warnings as failure exit (`doctor.go`: `hasFail = true`), which is strict but can surprise CI users who want doctor as a soft health check.
3. MCP discovery can take up to 30s per server; big setups feel slow.
4. Token-size warnings are heuristic-based and can produce noisy/non-actionable alerts in edge cases.
5. External MCP connectivity/auth failures surface during doctor and can look like "agent-layer broke," though root cause is external.
6. Optional update checks during sync/launch can add noise or latency in locked-down environments. The unauthenticated GitHub API rate limit (60/hour) can also cause spurious "failed to check for updates" warnings in shared CI runners.
7. If warning guidance itself is invalid, sync/doctor/init runs repeatedly surface unresolvable upgrade noise and erode trust. This is the most damaging form of guidance rot because users cannot silence it by following the instructions.
8. Launch commands (`al gemini`, `al claude`, `al codex`, `al antigravity`) always run sync before launching (`clients.Run`), so “just launch the tool” can unexpectedly modify generated files during upgrade periods.

### 9. Security and trust pain points

1. Generated files can contain sensitive resolved values (especially Codex projection paths) if users commit wrong artifacts.
2. Users may not realize `.agent-layer/.env` is the only safe secret store and accidentally place secrets in tracked files.
3. Destructive upgrade mode (`--force`) is powerful and easy to misuse under time pressure.

### 10. Documentation/discoverability pain

1. Upgrade knowledge is spread across README, reference docs, troubleshooting, changelog, roadmap, issues.
2. Users often need one upgrade playbook but currently must stitch multiple docs together.
3. No single taxonomy for upgrade event types (add/update/rename/delete/migration/manual action).
4. Conflicting upgrade instructions across surfaces reduce trust. Example class: one surface recommends automatic pin upgrade while another only documents manual pin edits.
5. No documentation of the pin file format constraints (semver only, no comments, no blank lines). Users discover this only through error messages.

## Locked Scope Decisions (confirmed)

These are confirmed implementation choices (scope), not sequencing decisions:

1. `1B`: Implement real `al init --version latest` support by resolving latest release to semver before writing `.agent-layer/al.version`.
2. `2A`: `al init` auto-recovers empty/corrupt pin file states instead of requiring manual deletion.
3. `3B`: Launch commands use sync mode `check` by default (no implicit mutation during launch; users can opt into apply/off modes). **Breaking change:** users who rely on `al gemini`/`al claude`/etc. to auto-sync generated files will need to explicitly use `apply` mode or run `al sync` first. Requires a migration manifest entry (Phase 3) and deprecation period.
4. `4C`: `al sync`/`al doctor` warning exit behavior is configurable (`strict`, `warn`, `report`) rather than one fixed policy.
5. `5B`: Pin-file parsing supports comments and blank lines.
6. `6B`: Default template MCP dependencies are version-pinned, with explicit opt-in for floating/latest update lanes.
7. `7A`: `al init` is scaffolding only: it errors if `.agent-layer/` already exists and does not perform upgrades. Use `al upgrade plan` and `al upgrade`.
8. `7B`: `.agent-layer/.env` and `.agent-layer/config.toml` are user-owned: seeded only when missing; never overwritten by init/upgrade.
9. `7C`: `.agent-layer/.gitignore` is agent-owned internal: always overwritten; excluded from upgrade plan/diffs.

## Plan to Minimize Pain (UX-first, “works like users expect”)

### Phase 0a: Fix shipping bugs in upgrade guidance (immediate, before next release)

1. **Drop Windows support.** Remove `al-install.ps1`, the Windows release target from `scripts/build-release.sh`, the `osWindows` code path in `cache.go`, `open-vscode.bat` launcher, and all Windows-specific documentation. Windows was never tested and "best-effort" support erodes trust.
2. **[Done] Fix upgrade guidance warnings.** Update warnings recommend `al upgrade plan` and `al upgrade` (or `al upgrade --force`) instead of using `al init` for upgrades.
3. **[Done] Let `al init` recover from empty/corrupt pin files.** Two fixes applied: (a) `readPinnedVersion()` in `pin.go` treats empty/invalid pins as "no pin" with a warning, so dispatch falls through to the current binary. (b) `writeVersionFile()` in `install.go` auto-repairs empty/corrupt pins without requiring prompts.
4. **Add `al init --version X` release validation.** Before writing the pin, HEAD the GitHub release URL to confirm the version exists. Fail with a clear "release X.Y.Z not found" message instead of writing a pin that will 404 on next use.
5. **[Done] Improve dispatch error messages.** `downloadToFile()` and `fetchChecksum()` now produce actionable 404 and timeout messages with remediation steps.
6. **[Done] Add download progress indicator.** `ensureCachedBinary` emits "Downloading al vX.Y.Z..." / "Downloaded al vX.Y.Z" to a `progressOut io.Writer` (wired to `sys.Stderr()` in production).

### Phase 0b: Define the upgrade contract

1. Publish a stable "upgrade event model" with categories: `safe auto`, `needs review`, `breaking/manual`.
2. Add explicit compatibility guarantees (for example N minor versions of migration support).
3. Version and publish migration rules with each release.
4. Publish an OS/shell capability matrix (supported shells, installer prerequisites, and known limitations) in one canonical location.

### Phase 1: Make upgrades explainable before they mutate files

1. Add `al upgrade plan` (dry-run only) showing categorized changes:
   - template additions
   - template updates
   - template renames
   - template removals/orphans
   - config key migrations
   - pin version changes (current → target)
2. Add clear ownership labels per diff: `upstream template delta` vs `local customization`.
3. Add machine-readable output (`--json`) for CI/repo automation.
4. Add upgrade-readiness checks in dry-run output:
   - flag suspicious/unrecognized config keys
   - flag stale generated outputs when launch path uses `--no-sync`
   - flag floating external dependency specs (for example `@latest`)
   - flag stale generated artifacts for disabled agents
5. Document pinning as required and add clear repair guidance for missing/invalid `.agent-layer/al.version`.
6. Gracefully degrade GitHub API update checks: when rate-limited (HTTP 403/429), suppress the warning silently or emit a one-line note instead of a multi-line block.
7. Add launch-impact preview (`al launch-plan <client>` or equivalent) that shows whether launching will modify files before executing sync.

### Phase 2: Make upgrades safe and reversible

1. Add automatic snapshot/rollback for managed files during upgrade operations.
2. Replace binary `--force` semantics with explicit flags:
   - `--apply-managed-updates`
   - `--apply-memory-updates`
   - `--apply-deletions`
3. Require explicit confirmation for deletions unless `--yes --apply-deletions` is provided.
4. Add `al upgrade rollback <snapshot-id>`.
5. Add CI-safe non-interactive apply mode: `al upgrade --yes --apply-managed-updates` applies managed template updates without deleting unknowns, bridging the gap between interactive `al upgrade` and destructive `al upgrade --force`.
6. Add explicit sync modes for launch commands across clients (`apply`, `check`, `off`) with default mode `check` so users can choose between mutation, verification-only, or no sync.

### Phase 3: Add real migration support (root cause for long-term pain)

1. Introduce migration manifests per release for:
   - file rename/delete mapping
   - config key rename/default transform
   - generated artifact transitions
2. Execute migrations idempotently before template write.
3. Emit deterministic migration report with before/after rationale.
4. Add compatibility shims plus deprecation periods for renamed commands/flags.
5. Add migration manifest entry for the launch-sync default change (Locked Decision 3B): deprecate implicit `apply` mode, warn for N releases, then switch default to `check`.
6. Add migration guidance/rules for env key transitions (for example non-`AL_` to `AL_`).
7. Add template-source metadata and pinning rules so non-default template repositories can be upgraded deterministically.

### Phase 4: Reduce cross-client surprise

1. Add “expected behavior” notes directly in warnings with concrete remediation.
2. Make template MCP dependency behavior deterministic by default:
   - pin default MCP tool versions (avoid floating `@latest` in seeded templates)
   - offer explicit opt-in auto-update lanes for teams that want newest tool releases
3. Add stale-output guardrails for launcher-driven `--no-sync` flows.
4. Add managed cleanup for disabled-agent outputs so stale client artifacts are removed or clearly reported during upgrades.

### Phase 5: Improve default human workflow

1. New recommended path:
   - `al upgrade plan`
   - `al upgrade` (interactive apply)
   - `al doctor`
2. Link this path from README/reference/troubleshooting/changelog.
3. Provide one-page upgrade checklist for teams/CI, including a non-interactive CI path: `al upgrade --yes --apply-managed-updates && al sync && al doctor` (depends on Phase 2, item 5 delivering `--yes --apply-managed-updates`; until then, CI must use `al upgrade --force`).
4. Validate all upgrade call-to-action text against real CLI acceptance (Phase 0a ensures initial fix; this phase adds regression tests and a linter for message constants).
5. Provide platform-specific upgrade quick paths for macOS/Linux shells, including completion expectations.
6. Document the pin-file format and parser behavior clearly (including support for comment/blank-line handling and auto-fix behavior). Add this to README and reference docs.

### Phase 6: Close remaining high/medium long-tail UX gaps

1. Installation and environment preflight:
   - Add `al doctor --upgrade-preflight` checks for unsupported platform/arch, missing installer prerequisites, PATH setup, shell completion support, and proxy/mirror readiness.
   - Provide copy-paste remediation commands for each failed check.
2. Dispatch and pinning resilience:
   - Add explicit effective-version/source output (`pin`, `AL_VERSION`, `current`) and a warning when `AL_VERSION` overrides the repo pin.
   - Add offline/cache remediation hints, optional version prefetch, retry/backoff, and tunable download timeout.
   - Implement pin-file parsing that supports comments/blank lines and preserve actionable auto-fix diagnostics for malformed version lines.
3. Init and template lifecycle ergonomics:
   - Unify managed + memory overwrite prompts into one categorized review screen.
   - Extend stale-file detection reporting to managed docs/memory outputs, not only `.agent-layer`.
   - Add `al upgrade --repair-gitignore-block` (or equivalent) to self-heal invalid block state.
4. Config/env preflight and migration guards:
   - Add checks for unresolved placeholders, process-env/.env collisions, ignored-empty env assignments, and path expansion anomalies.
   - Add strict unknown-key detection mode with actionable diagnostics.
5. Sync/doctor control and signal quality:
   - Add configurable exit policy (`strict`, `warn`, `report`) for sync/doctor; default to `strict` for backward compatibility.
   - Improve MCP discovery with caching, parallelism, and tunable timeouts.
6. Security/trust and documentation consolidation:
   - Add secret-risk lint/scan for generated artifacts before commit.
   - Add explicit safe-secret-placement onboarding in upgrade flow.
   - Add docs consistency checks so upgrade guidance and accepted CLI syntax cannot drift.

### Phase 7: Deferred lowest-priority UX refinements (absolute last)

1. Add per-client projection preview in the upgrade plan.
2. Add policy lints for risky patterns:
   - secrets in URLs
   - unsupported Codex header placeholder forms
   - client capability mismatch for enabled features
3. Add warning-source classification (`internal`, `external dependency`, `network`) and noise controls.
4. VS Code and launcher hardening:
   - Add launcher preflight for `al`/`code`, `CODEX_HOME` behavior messaging, and managed-block conflict diagnostics.
   - Add first-launch performance profiling and optimization work.
5. Wizard hardening:
   - Add non-interactive wizard profile mode.
   - Preserve comments where possible or show rewrite preview before apply.
   - Show explicit "servers that will be disabled due to missing secrets" summary before save.
   - Add backup retention policy and cleanup command.

## Target User Experience (what "easy and expected" should mean)

1. Upgrades are previewable before any write.
2. Users always know which changes are theirs vs upstream.
3. Destructive operations are explicit, scoped, and reversible.
4. Legacy repos are migrated, not broken, when possible.
5. Errors are actionable and name exact file/key/next-step.
6. Same input yields predictable output across all supported clients.
7. Upgrade instructions shown in warnings/docs are executable exactly as written.
8. External MCP tool behavior is stable by default unless users explicitly opt into floating latest.
9. Pinning a version that doesn't exist fails immediately with a clear error, not on next use.
10. Recovery from a broken pin file never requires manual file deletion; `al upgrade` is always a valid recovery path.
11. Every long-running operation (binary download, MCP discovery) shows progress.
