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

- Issue 2026-03-02 agent-layer-gitignore-missing-open-vscode-sh: Internal `.agent-layer` gitignore template omits `open-vscode.sh`
    Priority: Medium. Area: templates / launcher artifacts.
    Description: Generated `.agent-layer/.gitignore` does not include `open-vscode.sh`, so the repo-local launcher script may appear as an unignored file.
    Next step: Add `open-vscode.sh` to `internal/templates/agent-layer.gitignore` and cover with template/install tests that validate generated `.agent-layer/.gitignore` entries.
    Notes: Scope is template-source fix only; do not hand-edit generated `.agent-layer/.gitignore`.

- Issue 2026-03-02 upgrade-success-noop-output: `al upgrade` lacks explicit success message on no-op completion
    Priority: Medium. Area: CLI UX / upgrade command.
    Description: When `al upgrade` succeeds but has nothing to change, output can end after snapshot/rollback lines with no final confirmation, making completion status ambiguous.
    Next step: Emit a final success line for no-op and normal-complete paths (for example, `Upgrade successful.`) and cover it with command-output tests.
    Notes: Repro observed on 2026-03-02 from local run output showing snapshot creation and rollback hint only.

- Issue 2026-03-02 color-gating-consistency: Centralize CLI color gating beyond fatih/color built-in detection
    Priority: Low. Area: CLI output / color handling.
    Description: `color.YellowString()` gates ANSI output via fatih/color's global `NoColor` flag (terminal detection + `NO_COLOR` env var), but `shouldColorizeDiffOutput()` uses a separate `isTerminal() && !color.NoColor` check for unified-diff rendering with custom `color.New()` objects. The two gating mechanisms are inconsistent — `color.YellowString()` self-gates while diff colors require explicit gating. If Cobra's `io.Writer` is redirected (e.g., `cmd.SetOut()` in tests or embedding), `color.YellowString()` still checks `os.Stdout` for terminal detection, which could emit ANSI codes to non-terminal outputs.
    Next step: Audit all CLI color usage and evaluate whether a unified color-gating helper (wrapping both `color.YellowString`-style calls and `color.New()`-style calls) is warranted, or whether fatih/color's built-in detection is sufficient for all current surfaces.
    Notes: Deferred from PR #85 review. Current codebase uses `color.YellowString()` consistently in `doctor.go`, `upgrade.go` summary/readiness/breaking sections. The diff renderer is the only surface with explicit gating via `shouldColorizeDiffOutput()`.

- Issue 2026-03-02 mcp-skill-resource-gap: MCP prompt integration drops skill resource access
    Priority: High. Area: skills / MCP prompt server.
    Description: Agent Skills spec supports optional `scripts/`, `references/`, and `assets/` with on-demand loading, but agent-layer MCP prompts expose only `SKILL.md` body text and no resource-access mechanism or skill-path context.
    Next step: Extend skill loading and MCP prompt serving to include discoverable skill root/resource access (or add an internal tool/resource API) and add coverage tests using a skill with subfolders.
    GitHub: https://github.com/conn-castle/agent-layer/issues/86
    Notes: `internal/config/skills.go` parses only `SKILL.md`; `internal/mcp/prompts.go` returns `cmd.Body` only; `internal/sync/prompts.go` generates only `SKILL.md` in client skill outputs.

- Issue 2026-02-25 playwright-headless-parity: Evaluate headless Playwright mode without functional regressions
    Priority: Medium. Area: test automation / Playwright runner UX.
    Description: Playwright running in headed mode is noisy and disruptive during normal development. We should assess whether headless can be the default while preserving behavior.
    Next step: Run representative Playwright flows in headed vs headless mode, document any behavior differences, and only switch defaults if parity is confirmed.
    Notes: Keep an explicit opt-in path for headed runs for local debugging even if headless becomes the default.
