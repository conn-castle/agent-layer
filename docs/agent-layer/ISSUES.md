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

- Issue 2026-02-25 claude-auth-ignores-config-dir: Claude Code auth ignores CLAUDE_CONFIG_DIR (upstream)
    Priority: Medium. Area: agents.claude / auth isolation.
    Description: Claude Code stores OAuth credentials in the OS credential store (macOS Keychain under service `"Claude Code-credentials"`, Linux via libsecret/gnome-keyring) using a fixed service name, regardless of `CLAUDE_CONFIG_DIR`. Per-repo login isolation does not work. Reported upstream.
    Next step: Track upstream and repo tracking issues (`https://github.com/anthropics/claude-code/issues/20553`, `https://github.com/conn-castle/agent-layer/issues/78`). The fix would need Claude Code to namespace credential-store entries by `CLAUDE_CONFIG_DIR` (e.g., `"Claude Code-credentials-<hash>"`).
    Notes: No clean workaround exists. macOS Keychain is system-wide and keyed by service name, not directory path. `XDG_CONFIG_HOME` does not affect Keychain. Symlinks are not per-repo. A `security` CLI wrapper could intercept calls but is fragile and unsupportable.
