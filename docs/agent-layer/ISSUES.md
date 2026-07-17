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

- Issue 2026-07-13 corrupt-run-record-blocks-delete-cancel: A corrupt run record leaves its mapping undeletable and uncancellable
    Priority: Low. Area: internal/agentdispatch/operations.go (Delete, Cancel) + state.go retention
    Description: Delete and Cancel deliberately fail loud on a corrupt referenced run record (tested behavior consistent with the retention stance of never hiding corrupt evidence), so the only recovery is manual removal of the run directory; history already skips-and-warns, but the mapping stays blocked and retention never expires nonterminal corrupt records.
    Next step: Human to decide whether Delete should treat a provably corrupt record as inactive (releasing the mapping while preserving the evidence) or whether manual cleanup stays the contract.
    Notes: Dispatch is unreleased; blast radius today is developer-local state.

- Issue 2026-07-13 dispatch-redesign-performance-observation: Representative post-cutover workflow timing is not yet observed
    Priority: Medium. Area: full-workflow rollout evidence
    Description: The redesign implementation can be verified locally with fixtures, race tests, and repository checks, but this implementation run is explicitly prohibited from shipping itself or running another full workflow; planning, quality-stage, pull-request-open, shipping-overhead, and merge-continuation targets therefore lack three post-cutover representative observations.
    Next step: After user review and rollout, observe at least one tiny/local, one normal, and one concurrency/cross-cutting workflow; record controlled time separately from external wait and compare each phase to its target.
    Notes: This is the only intentionally deferred acceptance measurement; it does not authorize weakening functional gates. Known optimization candidate if targets are missed: the per-event run-record read in runner.go `consume` used for cooperative cancellation detection.

- Issue 2026-07-13 dispatch-host-yield-descendant-proof: Dispatch cannot control chat-host yielding or universally prove provider-native descendant terminality
    Priority: Critical. Area: agent dispatch host integration and provider lineage
    Description: Synchronous dispatch now separates progress/results, excludes concurrent resume, preserves recovery/history, reconciles owned processes, supports cancellation, and exposes factual activity; however a chat host may still yield the terminal command without a completion callback, and providers do not uniformly expose descendant lineage sufficient to prove every native child terminal.
    Next step: Add host-native completion wake-up only when the host offers a supported callback/tool boundary, and extend lineage claims provider-by-provider from authoritative events; do not infer terminality or poll inspect.
    Notes: The Claude background-wait ceiling is already fixed and is explicitly excluded.

- Issue 2026-07-10 antigravity-malformed-native-aborts-all-sync: A malformed native Antigravity settings.json fails the entire multi-client sync
    Priority: Low. Area: internal/sync/antigravity.go (readAntigravitySettings/mergeAntigravitySettings) + sync.go step ordering
    Description: WriteAntigravitySettings fails loud with no data loss on malformed/non-object/shape-conflict native settings.json, but the error bubbles up as a hard sync step, so one odd native value blocks generation for Claude/Codex/VS Code too. Overlay-only merge plus empty-file tolerance shrank the failure surface, but the blast radius is unchanged.
    Next step: Human to decide whether a single client's settings failure should warn-and-skip that client instead of aborting the whole run.
    Notes: Codex second-opinion flagged this as a separate product-level decision; intentionally not changed in the overlay-preserve change (decision antigravity-settings-overlay-preserve).

- Issue 2026-07-10 antigravity-managed-values-not-reclaimed: Agent Layer never reclaims managed Antigravity settings it previously wrote
    Priority: Low. Area: internal/sync/antigravity.go (overlay-only mergeAntigravitySettings)
    Description: settings.json is overlay-patched and never delete-on-absent, so a managed value (model, permissions.allow, agent_specific) removed from config.toml — or left behind when Antigravity is disabled — lingers in the file, indistinguishable from native state, and is never cleaned by Agent Layer. This is the accepted tradeoff of preserving native state.
    Next step: If reclaiming stale Agent Layer-written keys becomes a demonstrated need, add provenance tracking (a managed-key manifest/marker) so removed keys can be pruned without touching native values.
    Notes: Deferred by decision antigravity-settings-overlay-preserve (2026-07-10); provenance was judged too much persistent state for a preservation fix.

- Issue 2026-06-22 secret-in-url-precision-recall: Secret-in-URL detection precision vs recall tradeoff (needs human decision)
    Priority: Medium. Area: internal/warnings/policy.go (`looksLikeSecretQueryKey` and helpers)
    Description: Status quo (retained): secret-bearing query-param keys are matched by SUBSTRING — good recall (flags camelCase keys like `accessToken`/`authToken`/`apiToken`/`clientSecret` because e.g. "token" is a substring) but FALSE POSITIVES on benign keys containing a secret word (`?author=`, `?authority=`, `?tokenizer=`, `?passwordless=`), which currently fail `al sync` with a CRITICAL `POLICY_SECRET_IN_URL` finding. The obvious fix (word-segment matching) removes those false positives but REGRESSES recall: glued/camelCase secret keys (`accessToken`, `authtoken`, `clientSecret`) would no longer be flagged — a genuine security precision/recall tradeoff, not a clear win. An /improve-codebase rewrite of this logic was reverted pending this decision.
    Next step: Human to choose: (A) keep substring matching + add an explicit exclusion list of known-benign keys (preserves recall, surgical); (B) word-segment matching PLUS camelCase boundary splitting (fixes most false positives, restores camelCase recall, still misses fully-glued lowercase like `authtoken`); (C) accept the recall loss and ship pure word-segment matching.
    Notes: Recommendation: Option B is strongest if pursued, but this needs the maintainer's call because it changes a security control's detection semantics.

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
