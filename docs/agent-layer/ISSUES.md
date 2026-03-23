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
- Issue YYYY-MM-DD short-slug: Short title
    Priority: Critical | High | Medium | Low. Area: <area>
    Description: <observed problem or risk>
    Next step: <smallest concrete next action>
    Notes: <optional dependencies/constraints>
```

## Open issues

<!-- ENTRIES START -->

- Issue 2026-03-22 init-subfolder-of-repo: `al init` cannot install in a subfolder of another git repo
    Priority: Medium. Area: internal/root / init.
    Description: `FindRepoRoot` first calls `FindAgentLayerRoot` (walks upward for `.agent-layer/`) then falls back to walking upward for `.git`. Both walks return the first ancestor match, so running `al init` in a subfolder of a repo that already has agent-layer installed always resolves to the parent. Even `git init` in the subfolder doesn't help because the `.agent-layer` check takes priority. No clean workaround exists.
    Next step: Add a flag or heuristic so `al init` can target the current directory when the user intends to install in a subfolder (e.g. `--here` flag, or prompt when cwd != detected root).

- Issue 2026-03-02 color-gating-consistency: Centralize CLI color gating beyond fatih/color built-in detection
    Priority: Low. Area: CLI output / color handling.
    Description: `color.YellowString()` gates ANSI output via fatih/color's global `NoColor` flag (terminal detection + `NO_COLOR` env var), but `shouldColorizeDiffOutput()` uses a separate `isTerminal() && !color.NoColor` check for unified-diff rendering with custom `color.New()` objects. The two gating mechanisms are inconsistent — `color.YellowString()` self-gates while diff colors require explicit gating. If Cobra's `io.Writer` is redirected (e.g., `cmd.SetOut()` in tests or embedding), `color.YellowString()` still checks `os.Stdout` for terminal detection, which could emit ANSI codes to non-terminal outputs.
    Next step: Audit all CLI color usage and evaluate whether a unified color-gating helper (wrapping both `color.YellowString`-style calls and `color.New()`-style calls) is warranted, or whether fatih/color's built-in detection is sufficient for all current surfaces.
    Notes: Deferred from PR #85 review. Current codebase uses `color.YellowString()` consistently in `doctor.go`, `upgrade.go` summary/readiness/breaking sections. The diff renderer is the only surface with explicit gating via `shouldColorizeDiffOutput()`.
