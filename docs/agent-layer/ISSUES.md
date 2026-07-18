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

- Issue 2026-07-17 dispatch-redesign-performance-observation: Inner quality and controlled shipping timing remain unmeasured
    Priority: Medium. Area: full-workflow rollout evidence
    Description: Exact-head tiny/local (#154), normal-size (#153), and concurrency/cross-cutting (#155) outer ledgers are reconstructed, and merge continuation passed 3/3, but raw records do not timestamp implementation-to-quality transitions or separate controlled shipping coordination from checks, repair, review, and overlapping service wait; none exercised the common normal-work planning path.
    Next step: Emit first-class phase timestamps, then observe one tiny/local, one normal common-planning-flow, and one concurrency/cross-cutting exact-head delivery; reconsider closure when quality ratios and controlled shipping overhead can be computed without overlap.
    Notes: Baseline: `.agent-layer/tmp/dispatch-redesign-performance-observation.20260717-exact-head-reconstruction.report.md`; do not weaken gates or recount these deliveries as new observations.

- Issue 2026-07-13 dispatch-host-yield-descendant-proof: Dispatch cannot control chat-host yielding or universally prove provider-native descendant terminality
    Priority: Critical. Area: agent dispatch host integration and provider lineage
    Description: Claude Code 2.1.211+ now records bounded provider-authoritative lineage and derives explicit proven-terminal/unknown summaries, but a chat host may still yield the terminal command without a completion callback, and Codex and Antigravity do not expose equivalent authoritative lineage sufficient to prove every native child terminal.
    Next step: Add host-native completion wake-up only when the host offers a supported callback/tool boundary, and extend lineage claims to Codex or Antigravity only from authoritative provider events; do not infer terminality or poll inspect.
    Notes: Claude 2.1.211+ lineage proof is covered. The Claude background-wait ceiling is already fixed and is explicitly excluded.

- Issue 2026-06-22 fs-os-abstraction-four-patterns: Filesystem testability seams remain inconsistent across packages
    Priority: Low. Area: architecture/cross-cutting
    Description: install, sync, and launchers independently define `System` interfaces with `RealSystem` OS passthroughs, while doctor, wizard, and clients retain package-level function seams. The repeated approaches make filesystem testability conventions and maintenance inconsistent across package boundaries.
    Next step: When adjacent work touches these seams, compare the concrete package contracts and document preferred patterns; consolidate only overlapping behavior that has a demonstrated shared contract.
    Notes: Residual maintainability concern, not a known behavior defect; address incrementally without introducing a broad shared interface solely for uniformity.

- Issue 2026-05-23 dispatch-antigravity-argv-prompt-cap: Antigravity dispatch still caps prompt size because `agy` has no stdin/prompt-file path
    Priority: Low. Area: providers/antigravity
    Description: `internal/agentdispatch/adapters.go` `runAntigravity` must pass the prompt as a single argv element after `--print` (Antigravity exposes no stdin or prompt-file path). The opaque-failure defect is fixed: a pre-flight guard now rejects prompts over `AntigravityPromptMaxBytes` with a typed `ExitUsage` error and an actionable message (tested by `TestRunAntigravityRejectsOversizePrompt`). The residual limitation is the cap itself — Claude/Codex accept unbounded prompts on stdin, Antigravity does not.
    Next step: When upstream `agy --print` gains a stdin or `--prompt-file` path, switch to it and remove the size cap.
    Notes: Upstream `agy --help` as of 2026-05-22 shows no stdin/prompt-file path; deferred pending upstream CLI support. No code change available until then.
