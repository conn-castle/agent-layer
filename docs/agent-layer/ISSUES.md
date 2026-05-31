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

- Issue 2026-05-31 wizard-askuserquestion-toggle-clobbers-deny-hooks: AskUserQuestion disable toggle overwrites co-listed deny/PreToolUse entries
    Priority: High. Area: wizard/patch
    Description: The wizard "Disable AskUserQuestion" toggle line-edits `agent_specific.permissions.deny` and `hooks.PreToolUse` as whole lines. Enabling replaces the array (drops co-listed user denies/hooks); answering "No" comments the entire existing line (`confirmToggle` always marks touched), silently disabling unrelated entries on a normal wizard run; the `[[...PreToolUse]]` array-of-tables form errors on enable ("key value already exists as a PreToolUse"). The line-based patcher cannot union arrays by design. Scalar toggles (IDE/memory/connectors, Codex apps/browser) are unaffected.
    Next step: Replace the two raw-array writers with a typed owned `disable_question_tool` bool (config field + fields.go) and inject the deny entry + PreToolUse hook at sync time in `buildClaudeSettings` with explicit array-union/dedup (the deep-merge replaces arrays, it does not union); always emit the hook so YOLO/bypassPermissions stays enforced.
    Notes: Supersedes the array half of DECISIONS.md `wizard-feature-disable-agent-specific` (sound for scalars, broken for array-shaped values). Unreleased, so likely no migration needed. Raised by CodeRabbit + Codex on PR #115.

- Issue 2026-05-23 dispatch-antigravity-argv-arg-max: Antigravity dispatch passes prompt on argv; very large prompts hit OS ARG_MAX
    Priority: Medium. Area: providers/antigravity
    Description: `internal/agentdispatch/adapters.go` `runAntigravity` passes the prompt as a single argv element after `--print`. The OS `execve(2)` cap (~128KB Linux, ~256KB macOS) means a multi-hundred-KB prompt fails at exec time with an opaque "argument list too long" error mapped to dispatch exit 70.
    Next step: Revisit when Antigravity's `agy --print` exposes a stdin or prompt-file path; until then, surface an explicit pre-flight length check with a typed `ExitUsage` error explaining the antigravity-specific limit.
    Notes: Claude and Codex are unaffected (both receive the prompt on stdin). Upstream `agy --help` on 2026-05-22 shows no stdin/prompt-file path; deferred pending upstream CLI support.
