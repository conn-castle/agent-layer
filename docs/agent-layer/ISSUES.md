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

- Issue 2026-06-02 codex-buildconfig-test-only-shim: `buildCodexConfig` survives only as a test seam
    Priority: Low. Area: sync/codex
    Description: The statusline work threaded a `System` through Codex config generation, so production now calls `buildCodexConfigWithSystem(sys, ...)` directly (`internal/sync/codex.go`). The old `buildCodexConfig(root, project)` was reduced to a one-line `RealSystem{}` delegate kept alive solely by ~24 test call sites; no production code calls it. This is a minor test-only seam, not a behavior risk.
    Next step: Migrate the ~24 `buildCodexConfig(` call sites in `internal/sync/codex_test.go` to `buildCodexConfigWithSystem(RealSystem{}, ...)` (or a test system) and delete the shim. Mechanical; deferred to keep the statusline PR scoped.
    Notes: Surfaced by the simplify-new-code pre-pass, which cannot touch tests.

- Issue 2026-06-02 unpinned-upgrade-latest-manifest-only: Unpinned upgrades run only the latest migration manifest, forcing carry-forward of legacy ops
    Priority: Medium. Area: install/upgrade-migrations
    Description: When the source version is unknown/unpinned, `inst.planUpgradeMigrations` loads only the latest manifest (`internal/install/upgrade_migrations.go:204-205`), not the full chain. So a legacy cleanup (e.g. Gemini->Antigravity) reaches unpinned users only while it lives in the latest manifest. Adding `0.11.0` forced duplicating the entire 0.10.2 Gemini op set into `0.11.0.json` so unpinned legacy users still get it — a carry-forward burden that violates single-source-of-truth and silently drops legacy migrations for unpinned users if ever forgotten.
    Next step: Change the unpinned+triggered path to run all `source_agnostic` ops across the full manifest chain (non-source-agnostic ops are already skipped under unknown source), then drop the carried-forward Gemini ops from `0.11.0.json`. Update `TestPlanUpgradeMigrations_UnpinnedMCPGeminiClientTriggersLatestManifest` and `...UnpinnedNoLegacyTriggerSkipsManifest`, which encode the current single-manifest behavior.
    Notes: This is "Option 2" deferred from the statusline work; pinned upgrades already chain correctly and are unaffected.

- Issue 2026-05-23 dispatch-antigravity-argv-arg-max: Antigravity dispatch passes prompt on argv; very large prompts hit OS ARG_MAX
    Priority: Medium. Area: providers/antigravity
    Description: `internal/agentdispatch/adapters.go` `runAntigravity` passes the prompt as a single argv element after `--print`. The OS `execve(2)` cap (~128KB Linux, ~256KB macOS) means a multi-hundred-KB prompt fails at exec time with an opaque "argument list too long" error mapped to dispatch exit 70.
    Next step: Revisit when Antigravity's `agy --print` exposes a stdin or prompt-file path; until then, surface an explicit pre-flight length check with a typed `ExitUsage` error explaining the antigravity-specific limit.
    Notes: Claude and Codex are unaffected (both receive the prompt on stdin). Upstream `agy --help` on 2026-05-22 shows no stdin/prompt-file path; deferred pending upstream CLI support.
