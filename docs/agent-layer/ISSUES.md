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

- Issue 2026-02-14 upg-force-api: install.Options.Force is now dead for production upgrade path
    Priority: Low. Area: install / API hygiene.
    Description: `cmd/al/upgrade.go` always passes `Force: false` and drives apply behavior through category-aware prompts, while `install.Options.Force` remains in the public install API and is still used mostly by tests.
    Next step: Decide whether to remove `Force` from `install.Options` (with test helper replacements) or explicitly document it as legacy/internal-only.

- Issue 2026-02-14 upg-snapshot-size: No per-snapshot size guard for upgrade snapshots
    Priority: Low. Area: install / storage.
    Description: `internal/install/upgrade_snapshot.go` stores full file contents for rollback entries and retains up to 20 snapshots, but does not warn or cap snapshot size in unusually large repos.
    Next step: Add snapshot-size budget checks (warning and/or configurable cap) and document retention sizing guidance.

- Issue 2026-02-12 mcp-env: os.Environ() leaks credentials to MCP child processes
    Priority: Critical. Area: doctor / security.
    Description: `internal/doctor/mcp_connector.go:90-93` passes `os.Environ()` to MCP child processes, leaking all parent env vars (API keys, tokens, credentials) to potentially untrusted MCP servers.
    Next step: Replace `os.Environ()` with an explicit allowlist of safe environment variables, filtering out sensitive prefixes.

- Issue 2026-02-12 wiz-rollback: Incomplete rollback in wizard apply.go on partial write failure
    Priority: High. Area: wizard / data integrity.
    Description: `internal/wizard/apply.go:68-74` — if config write succeeds but env write fails, config is left modified with no rollback despite backups being in place. User is left in inconsistent state.
    Next step: Add config restore from backup in the env-write error path.

- Issue 2026-02-12 sys-bypass: System interface bypassed in dispatch and sync packages
    Priority: Medium. Area: dispatch, sync / testability.
    Description: `internal/dispatch/cache.go:140-142` (`noNetwork()` calls `os.Getenv` directly) and `internal/sync/prompts.go` (`os.ReadDir`/`os.Remove` directly) bypass their respective System interfaces, making these paths harder to test.
    Next step: Route through the System interface in both locations.

- Issue 2026-02-12 dl-unbounded: Unbounded download size in dispatch cache
    Priority: Medium. Area: dispatch / reliability.
    Description: `internal/dispatch/cache.go:160` uses `io.Copy` with no size limit. A malicious or misconfigured server could serve an arbitrarily large response. Mitigated partially by 30-second HTTP timeout.
    Next step: Wrap with `io.LimitReader(resp.Body, maxDownloadSize)`.

- Issue 2026-02-12 flock-hang: Blocking flock with no timeout in dispatch lock
    Priority: Medium. Area: dispatch / reliability.
    Description: `internal/dispatch/lock.go:57-58` uses `unix.Flock(LOCK_EX)` with no timeout. A crashed process holding the lock causes the caller to hang indefinitely with no diagnostic output.
    Next step: Switch to non-blocking `LOCK_NB` in a polling loop with timeout and status messages.

- Issue 2026-02-12 mcp-dup-id: Duplicate MCP server IDs not validated
    Priority: Medium. Area: config / correctness.
    Description: `internal/config/validate.go:57-104` checks for empty and reserved MCP server IDs but not duplicates. Multiple servers with the same ID silently overwrite each other in downstream map-keyed code.
    Next step: Add a seen-ID set in the validation loop and return an error on duplicates.

- Issue 2026-02-12 codex-perm: Codex config written 0644 with resolved secrets
    Priority: Medium. Area: sync / security.
    Description: `internal/sync/codex.go:34` writes the Codex config (header says "MAY CONTAIN SECRETS") with 0644 (world-readable) permissions. Should be 0600, consistent with env file handling elsewhere.
    Next step: Change permission to 0600.

- Issue 2026-02-12 rt-mutate: headerTransport.RoundTrip mutates original request
    Priority: Medium. Area: doctor / correctness.
    Description: `internal/doctor/mcp_connector.go:201-208` modifies `req.Header` directly instead of cloning the request first, violating the `http.RoundTripper` contract.
    Next step: Clone request via `req.Clone(req.Context())` before setting headers.

- Issue 2026-02-12 resolve-default: resolveSingleServer switch missing default branch
    Priority: Medium. Area: clients / correctness.
    Description: `internal/clients/resolvers.go:72-135` handles only `TransportHTTP` and `TransportStdio`. Unknown transport types silently fall through, returning an entry with empty Transport.
    Next step: Add a default case that returns an error for unknown transport types.

- Issue 2026-02-12 gen-marker: Broad "GENERATED FILE" marker match in sync
    Priority: Medium. Area: sync / reliability.
    Description: `internal/sync/prompts.go:241-250` uses a broad substring match for detecting generated files that could match non-agent-layer files containing similar text.
    Next step: Use a more specific marker that includes "agent-layer" or "al sync".

- Issue 2026-02-12 cli-cobra: CLI commands bypass Cobra patterns (stdout and context)
    Priority: Medium. Area: CLI / testability.
    Description: `cmd/al/doctor.go` writes to global `fmt.Println`/`os.Stdout` instead of `cmd.OutOrStdout()`, and multiple commands (`doctor.go`, `mcp_prompts.go`) use `context.Background()` instead of `cmd.Context()`, preventing graceful cancellation.
    Next step: Replace global stdout with `cmd.OutOrStdout()` and `context.Background()` with `cmd.Context()`.

- Issue 2026-02-12 env-empty: Empty string treated as missing env var
    Priority: Medium. Area: config / correctness.
    Description: `internal/config/env_subst.go:49-56` treats `AL_SECRET_FOO=""` (explicitly set to empty) the same as an unset variable, preventing users from intentionally setting a secret to an empty string.
    Next step: Distinguish between unset and empty-string env vars.

- Issue 2026-02-12 wiz-globals: Mutable exported globals and dead code in wizard package
    Priority: Low. Area: wizard / maintainability.
    Description: `internal/wizard/catalog.go:15-95` has mutable exported slice variables (e.g., `MCPServerCatalog`) that any caller can modify. Also, `approval_modes.go` and `helpers.go` contain unreferenced functions (`ApprovalModes`, `approvalModeHelpText`, `commentForLine`, `inlineCommentForLine`).
    Next step: Convert exported catalog variables to functions returning fresh copies; remove confirmed dead code.

- Issue 2026-02-12 wiz-secret-loop: No escape from secret input loop in wizard
    Priority: Low. Area: wizard / UX.
    Description: `internal/wizard/wizard.go:262-280` prompts for secrets in a retry loop with no mechanism for the user to skip or cancel. If a secret fails validation, the user is stuck until Ctrl+C.
    Next step: Add a "skip" or "cancel" option to the secret prompt loop.

- Issue 2026-02-12 stub-dup: writeStubWithExit test helper duplicated across 5+ packages
    Priority: Low. Area: testing / DRY.
    Description: The `writeStubWithExit` test helper (writes an executable stub script) is duplicated across `cmd/al`, `cmd/publish-site`, and other test packages with minor variations.
    Next step: Consolidate into `internal/testutil` alongside `boolPtr` and `withWorkingDir`.

- Issue 2026-02-12 envfile-asym: Asymmetric envfile encode/decode
    Priority: Low. Area: envfile / correctness.
    Description: `internal/envfile/envfile.go:113-130` handles quoting and escaping differently on encode vs. decode paths, meaning a round-trip (write then read) may not preserve values with embedded quotes, newlines, or special characters.
    Next step: Add round-trip property tests and align encode/decode paths.

- Issue 2026-02-12 pin-silent: Corrupt pin file silent fallback in dispatch
    Priority: Low. Area: dispatch / observability.
    Description: When the pin version file contains invalid or corrupt content, dispatch silently falls back to default behavior instead of reporting the malformed file, violating the "fail loudly" principle.
    Next step: Return an explicit error or warning when pin file content is unparseable.

- Issue 2026-02-12 gh-retry: No retry for transient GitHub API failures in update check
    Priority: Low. Area: doctor / reliability.
    Description: GitHub API calls for update checks have no retry logic. Transient network errors cause the check to fail silently. Recent work (commit 39fab73) added graceful degradation for rate limits, but transient connection errors still go unretried.
    Next step: Add a single retry with short backoff for transient HTTP errors.

- Issue 2026-02-12 3c5f958: Repetitive error-wrapping in upgrade readiness checks
    Priority: Medium. Area: install / maintainability.
    Description: `internal/install/upgrade_readiness.go` contains 16 nearly identical `fmt.Errorf("readiness check failed to <verb> %s: %w", ...)` calls. A small `readinessErr(verb, path, err)` helper would reduce duplication and prevent format-string drift.
    Next step: Extract helper function and update all 16 call sites.

- Issue 2026-02-12 3c5f958b: detectDisabledAgentArtifacts is a god function
    Priority: Medium. Area: install / extensibility.
    Description: `detectDisabledAgentArtifacts` in `upgrade_readiness.go` (115 lines) handles 6 agent types with per-agent evidence strategies in deeply nested conditionals. Adding a new agent requires inserting a new code block rather than a table entry.
    Next step: Refactor to table-driven per-agent artifact definitions with a shared iteration loop.

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
    Description: The `installer` struct in `internal/install/` has 14 fields and 57 methods spread across 8 files. While logically grouped, this concentration increases coupling risk as Phase 11 continues adding features (snapshot/rollback, migration engine).
    Next step: Monitor during Phase 11 Phase 2 work. If method count exceeds ~70, extract sub-structs (e.g., `templateManager`, `ownershipClassifier`).

- Issue 2026-02-10 wiz-run: Wizard Run() is a god function
    Priority: Medium. Area: wizard / maintainability.
    Description: `internal/wizard/wizard.go` is ~337 lines total, with `Run()` mixing install gating, config loading, choice initialization, UI flow, and apply logic, making it hard to test and extend safely.
    Next step: Extract focused helpers (init choices from config, prompt secrets, prompt warnings) and keep `Run()` as a small orchestrator.

- Issue 2026-02-10 upg-ver: Cannot apply a specific intermediate release during upgrade
    Priority: Medium. Area: upgrade / version pinning / UX.
    Description: With `al upgrade` always running the currently installed binary (dispatch bypass) and no `al upgrade --version`, there is no supported way to upgrade a repo from an older pin to an intermediate version (for example 0.6.0 -> 0.6.1) when a newer version is installed (for example 0.7.0); the repo is forced to upgrade to the installed version or rely on manual pin edits/reinstalling.
    Next step: Decide on a supported workflow (`al upgrade --version X.Y.Z`, or an equivalent “dispatch/exec target version for upgrade” mechanism) and add tests + docs.

- Issue 2026-02-09 web-seo: Update website metadata, SEO, and favicon
    Priority: Medium. Area: website / marketing.
    Description: The website needs professional metadata, SEO optimization, and a proper favicon to improve visibility and professional appearance.
    Next step: Audit `site/` for missing meta tags and favicon, then implement them.

- Issue 2026-02-08 upd-msg: Ambiguous update available warning message
    Priority: Medium. Area: CLI / update / UX.
    Description: The warning message "Warning: update available: %s (current %s)" does not specify that the update is for `agent-layer`, which can be confusing to users.
    Next step: Update `internal/messages/cli.go` and `internal/messages/doctor.go` to include "agent-layer" in the message (e.g., "Warning: agent-layer update available: ...").

- Issue 2026-02-08 tmpl-mk: Slash-command templates reference non-existent Makefile targets
    Priority: Low. Area: templates / developer experience.
    Description: `finish-task.md`, `fix-issues.md`, and `cleanup-code.md` templates reference `make test-fast` and `make dead-code`. The templates already guard these with conditional language ("preferred when available", "only if already present"), but agents may still attempt them in repos that do not provide them.
    Next step: Consider whether the conditional language is sufficient, or whether a stronger guard (e.g., checking target existence before invocation) would reduce noise.

- Issue 2026-01-24 a1b2c3: VS Code slow first launch in agent-layer folder
    Priority: Low. Area: developer experience.
    GitHub: https://github.com/conn-castle/agent-layer/issues/39
    Description: Launching VS Code in the agent-layer folder takes a very long time on first use, likely due to extension initialization, indexing, or MCP server startup.
    Next step: Profile VS Code startup to identify the bottleneck (extensions, language servers, MCP servers, or workspace indexing).
