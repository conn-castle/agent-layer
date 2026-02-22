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

- Issue 2026-02-22 config-key-style-unify: Config key naming mixes kebab-case and snake_case
    Priority: Medium. Area: config schema / UX.
    Description: The config currently mixes naming styles (for example `agents.claude-vscode` vs `agents.codex.agent_specific` and `reasoning_effort`), which makes the schema feel inconsistent and harder to learn.
    Next step: Define one canonical key style based on TOML/client ecosystem best practice, then migrate all config keys, templates, docs, and tests to that convention.

- Issue 2026-02-20 gemini-http-transport-url: Gemini sync maps SSE servers to httpUrl instead of url
    Priority: Medium. Area: sync / Gemini MCP.
    Description: `buildGeminiSettings` in `internal/sync/gemini.go:98-103` always maps HTTP transport servers to `httpUrl` (streamable HTTP) regardless of the resolved `HTTPTransport` value. Gemini CLI distinguishes `httpUrl` (streamable HTTP) from `url` (SSE). The default `http_transport` is `"sse"` (`resolvers.go:76-78`), so any custom HTTP server without explicit `http_transport = "streamable"` would be written to the wrong field. Current seeded defaults are unaffected because `tavily` explicitly sets `http_transport = "streamable"`.
    Next step: Check `server.HTTPTransport` in the Gemini build loop: use `url` for SSE, `httpUrl` for streamable. Add test coverage for both transport sub-types.

- Issue 2026-02-20 wizard-config-order-preferences: Audit wizard config write order and align it to preferred priority
    Priority: Medium. Area: wizard / UX.
    Description: Wizard output currently follows canonical template order, and the current sequence may not match a human-friendly priority order for reading/editing `config.toml`.
    Next step: Audit the current wizard-managed section/server ordering, gather preferred order from the user, then update template/patch ordering logic and tests to enforce that sequence.

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
    Description: During upgrade flows that create a snapshot, invoking recovery does not restore the environment to the version at upgrade start; it restores only to the immediately prior version state. This violates the expectation that upgrade/rollback flows are always safe and dependable.
    Next step: E2E scenario 055 covers the basic upgrade/rollback path and asserts version restoration. Investigate edge cases where "prior version" differs from "upgrade-start version", add a dedicated test, and enforce the correct recovery target.
    Notes: Reliability contract: `al upgrade` must always work, including rollback correctness.

- Issue 2026-02-19 toml-multiline-state-dup: Multiline TOML state tracking duplicated across 7 functions in wizard/patch.go
    Priority: Low. Area: wizard / maintainability.
    Description: The pattern of checking `IsTomlStateInMultiline(state)` / `ScanTomlLineForComment(line, state)` is repeated in `extractMCPBlockKeyValue`, `removeKeyFromBlock`, `findKeyLine`, `parseKeyLineWithState`, `replaceOrInsertLine`, `findInsertIndex`, and the bracket-depth loop in `multilineValueEndIndex`. v0.8.3 added two more instances. A shared line iterator that advances state and yields parsed lines would consolidate this.
    Next step: Extract a `tomlBlockIterator` or equivalent that encapsulates state tracking and yields non-multiline-content lines, then refactor all 7 call sites.

- Issue 2026-02-18 config-new-field-checklist: Future new required config fields must include migration manifest entry
    Priority: Medium. Area: config / backwards compatibility.
    Description: v0.8.1 added `agents.claude-vscode.enabled` as required, breaking pre-v0.8.1 configs. Fixed by config resilience (lenient parsing + interactive upgrade prompts). Any future new required field must include a `config_set_default` operation in the release's migration manifest (`internal/templates/migrations/<version>.json`).
    Next step: Add a CI lint or code review checklist item that enforces: any new nil-check in `Validate()` must have a matching `config_set_default` operation in the migration manifest.

- Issue 2026-02-16 skill-standard-rename: Rename slash-commands to skills and align with standard
    Priority: High. Area: slash-commands / skills.
    Description: Slash-commands should be renamed to "skills" to align with the established skill standard. This includes supporting supplemental folders within the skill directory and updating `al doctor` to verify compatibility using the standard toolset.
    Next step: Perform a global rename of slash-command terminology and implement structural/validation updates to match the skill standard.

- Issue 2026-02-15 upg-config-toml-roundtrip: Config migrations strip user TOML comments/formatting
    Priority: Medium. Area: install / UX.
    Description: `upgrade_migrations.go` decodes `.agent-layer/config.toml` into a map and re-marshals after key/default migrations, which removes user comments and original key ordering.
    Next step: Preserve comments/order for simple key migrations (line-level edit or AST-preserving strategy), or explicitly document this destructive formatting side effect.
    Notes: Explicitly deferred in the Upgrade continuation scope; wizard/profile flows now show rewrite previews and backup files before write.

- Issue 2026-02-12 3c5f958f: installer struct accumulating responsibilities
    Priority: Low. Area: install / maintainability.
    Description: The `installer` struct in `internal/install/` has 23 fields and 57+ methods spread across 8+ files. While logically grouped, this concentration increases coupling risk as features continue to be added.
    Next step: Audit current method count (Phase 11 is complete). If it exceeds ~70, extract sub-structs (e.g., `templateManager`, `ownershipClassifier`). Scheduled in Phase 13 (maintenance).

- Issue 2026-02-09 web-seo: Update website metadata, SEO, and favicon
    Priority: Medium. Area: website / marketing.
    Description: The website needs professional metadata, SEO optimization, and a proper favicon to improve visibility and professional appearance.
    Next step: Audit `site/` for missing meta tags and favicon, then implement them.

- Issue 2026-02-08 tmpl-mk: Slash-command templates reference non-existent Makefile targets
    Priority: Low. Area: templates / developer experience.
    Description: `finish-task.md`, `fix-issues.md`, and `cleanup-code.md` templates reference `make test-fast` and other repo-specific optional targets. The templates already guard these with conditional language ("preferred when available", "only if already present"), but agents may still attempt commands in repos that do not provide them.
    Next step: Consider whether the conditional language is sufficient, or whether a stronger guard (e.g., checking target existence before invocation) would reduce noise.
    Notes: Reconfirmed by documentation audit on 2026-02-18; keep this as a template-level guardrail issue (not a repo-local Makefile requirement).
