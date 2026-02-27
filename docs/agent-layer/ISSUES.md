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

- Issue 2026-02-26 skill-validator-unicode-nfkc: Add full Unicode/NFKC skill-name validation support
    Priority: Low. Area: skills / validation.
    Description: The current validator enforces the ASCII baseline rules (`[a-z0-9-]`, no `--`, length cap) but does not yet implement Unicode lowercase alphanumeric handling with NFKC normalization from the full agentskills specification.
    Next step: Extend name validation with NFKC normalization and Unicode-aware lowercase/alphanumeric checks, then add conformance tests for mixed-script and normalized-equivalent inputs.
    Notes: Keep parse/validate separation and deterministic finding output unchanged.

- Issue 2026-02-26 skill-file-lowercase-fallback: Accept lowercase `skill.md` for directory-format compatibility
    Priority: Low. Area: skills / loader compatibility.
    Description: Directory-format skills currently require `SKILL.md`; the reference implementation also accepts lowercase `skill.md`, so imported community skills may fail validation/loading unnecessarily.
    Next step: Add a backward-compatible lowercase fallback lookup with explicit precedence (`SKILL.md` first), plus tests for mixed-case and duplicate-file edge cases.
    Notes: Non-blocking interop gap; keep uppercase `SKILL.md` as the canonical output format.

- Issue 2026-02-26 doctor-config-errors-explicit: Make `al doctor` config error guidance explicit and corrective (https://github.com/conn-castle/agent-layer/issues/80)
    Priority: High. Area: doctor / config diagnostics UX.
    Description: Current `al doctor` failures for config validation (for example unrecognized keys) are too generic and do not clearly identify the exact offending keys/sections and concrete repair path.
    Next step: Update `al doctor` config-failure output to include explicit invalid keys/paths detected, why they are invalid for the current schema/version, and direct copyable remediation options.
    Notes: Keep strict failure behavior, but improve first-pass operator clarity so users can resolve issues without trial-and-error.

- Issue 2026-02-25 go-126-deps-audit: Upgrade to Go v1.26.0 and audit outdated dependencies
    Priority: Medium. Area: build/toolchain / dependency management.
    Description: The repository should be upgraded to Go `v1.26.0` and reviewed for additional outdated packages/modules to reduce drift and compatibility risk.
    Next step: Update the Go toolchain target/version references, run dependency update checks, and prepare a scoped upgrade plan with compatibility test results.
    Notes: Prefer latest stable compatible versions; if any upgrade is breaking, document impact and sequence fixes before rollout.

- Issue 2026-02-25 playwright-headless-parity: Evaluate headless Playwright mode without functional regressions
    Priority: Medium. Area: test automation / Playwright runner UX.
    Description: Playwright running in headed mode is noisy and disruptive during normal development. We should assess whether headless can be the default while preserving behavior.
    Next step: Run representative Playwright flows in headed vs headless mode, document any behavior differences, and only switch defaults if parity is confirmed.
    Notes: Keep an explicit opt-in path for headed runs for local debugging even if headless becomes the default.

- Issue 2026-02-25 claude-auth-ignores-config-dir: Claude Code keychain ACL prevents credential persistence across restarts (upstream)
    Priority: Medium. Area: agents.claude / auth isolation.
    Description: Two upstream bugs affect per-`CLAUDE_CONFIG_DIR` credential isolation. Bug 1 (keychain namespacing) is now fixed — Claude Code creates per-config-dir entries as `"Claude Code-credentials-{sha256(path)[:8]}"`. Bug 2 (keychain ACL) is still broken and is the actual blocker: entries are created with `partition_id: apple-tool:` and decrypt authorized only for `/usr/bin/security`, so Claude Code's own binary (`com.anthropic.claude-code`, team `Q6L2SF6YDW`) cannot read them after a restart. Auth must be re-entered in every repo after each reboot.
    Next step: Track upstream issues. The fix requires Claude Code to set `partition_id` to include `teamid:Q6L2SF6YDW` (or equivalent) so it can read its own keychain entries. Upstream: `anthropics/claude-code#20553` (namespacing, fixed), `anthropics/claude-code#19456` (ACL, still open). Local: `conn-castle/agent-layer#78`.
    Notes: No workaround exists. The namespacing fix is confirmed working (each `CLAUDE_CONFIG_DIR` gets an independent OAuth session with separate tokens), but the ACL bug nullifies it by preventing reads after restart.
