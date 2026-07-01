# Agent Dispatch

Agent Dispatch lets one Agent Layer-managed agent call another agent through a small, scriptable interface. It is for focused, headless tasks where a caller needs a second agent's model, tools, skills, or review perspective without leaving the current workflow.

This document is about agent-to-agent dispatch through `al dispatch`. It is unrelated to Agent Layer's internal version dispatch, which forwards `al` invocations to a repo-pinned binary.

## CLI

Use `al dispatch` with prompt text, a skill, or both:

```bash
al dispatch --agent codex "Review this plan artifact set."
al dispatch --agent random --skill review-plan "Review this plan artifact set."
al dispatch --agent claude --skill finish-task
al dispatch --agent antigravity "Check whether this implementation plan is complete."
```

For longer prompts, pass the prompt on standard input:

```bash
cat prompt.md | al dispatch --agent claude
```

Use `--model` and `--reasoning-effort` for per-run overrides when the selected target supports that override:

```bash
al dispatch --agent codex --model <model-name> --reasoning-effort high "Review this plan artifact set."
al dispatch --agent antigravity --model "Gemini 3.1 Pro (High)" "Review this plan artifact set."
```

Dispatch streams the target's human-readable output as supported by the target CLI. Target answer text is written to standard output. Dispatch status, selected-target notices, compact progress, and errors are written to standard error.

When a target CLI exposes partial output only through an official structured stream, Dispatch may consume that documented stream internally and emit readable text to the caller. Dispatch must not expose raw event streams as its public output and must not scrape interactive terminal interfaces.

Verified target behavior:

- Claude streams partial answer text through its official print-mode structured stream.
- Antigravity streams readable answer text through `agy --print`.
- Codex uses stable `codex exec`. It streams lifecycle/progress events, but assistant answer text is emitted only as a final message when the run completes.

`--agent` selects the target CLI/runtime. Supported values are `codex`, `claude`, `antigravity`, and `random`. If `--agent` is omitted, Agent Layer must know the calling agent. If the caller is unknown, dispatch fails and asks for `--agent`.

`--model` and `--reasoning-effort` are optional. If omitted, dispatch uses the same Agent Layer and client defaults that normal `al <agent>` launches use. Override support is target-specific and reported by `al dispatch options`; unsupported override fields fail before launch with exit `64`. Antigravity supports per-run `--model` overrides and otherwise uses `agents.antigravity.model` when configured. Antigravity does not expose separate `--reasoning-effort` overrides because agy encodes reasoning level in the model display string.

For targets whose Agent Layer field catalog accepts custom values, out-of-catalog values are passed to the target CLI verbatim. If the target rejects the value, dispatch exits `70` and preserves the target's error text on standard error.

`al dispatch options` lists available dispatch targets, dispatch capability, random-selection eligibility, configured model and reasoning values, known suggestions, whether custom values are accepted, detected caller, and the current random pool.

Use `al dispatch options --json` for structured output. The JSON shape is stable in v1: existing fields keep their meaning, and future versions may add fields. The top-level object contains:

- `caller`: whether the caller is known and the caller agent when known
- `random`: the current random-selection pool, whether caller exclusion is active, and whether the pool is empty
- `targets`: one entry per target, including enablement, install status, dispatch capability, random eligibility, streaming capability, model metadata, reasoning-effort metadata, and unavailable reasons

The `unavailable_reasons` field draws from a fixed v1 vocabulary: `disabled`, `binary_not_found`, `unauthenticated`, `unsupported_for_dispatch`, and `configuration_error`. v1 currently emits only `disabled` and `binary_not_found`; the additional values `unauthenticated`, `unsupported_for_dispatch`, and `configuration_error` are reserved per spec for future detection paths and may begin appearing in subsequent v1 revisions without breaking the JSON contract.

## Generated Instructions

Agent Layer generated instructions describe `al dispatch` so agents can discover and use the command during normal tool execution. Agents can run `al dispatch options` when they need to inspect the currently available targets, models, reasoning efforts, caller detection, or random-selection behavior.

## Skills

Dispatch uses the current project's canonical Agent Layer skill source, `.agent-layer/skills/`. Agent Layer projects those source skills into each target's native skill location during normal sync/launch preparation. Caller-side skill availability is irrelevant; `--agent codex --skill review-plan` validates `review-plan` against Agent Layer's source skills and the selected target's skill support before launch.

Use `--skill` to request a skill by its portable Agent Layer name:

```bash
al dispatch --agent codex --skill review-plan "Review this."
```

Dispatch validates that the skill exists, formats the target agent's native skill reference, and sends the prompt as:

```text
$review-plan
Review this.
```

The skill reference uses the target agent's native invocation syntax:

- Codex: `$skill-name`
- Claude and Antigravity: `/skill-name`

Unsupported target skill delivery, missing skills, and unsynced skill projections are configuration errors, not silent rewrites.

If `skill` is provided without prompt text, dispatch sends only the skill reference.

## Environment

Agent Layer sets the caller marker when it launches an agent:

```bash
AL_DISPATCH_CALLER_AGENT=claude
```

Allowed values are `codex`, `claude`, and `antigravity`.

The caller marker is a routing hint, not a security boundary. Users and tests can spoof environment variables; permission and trust decisions must not rely on `AL_DISPATCH_CALLER_AGENT` as proof of identity.

Dispatch sets the current active dispatch depth while running a target agent:

```bash
AL_DISPATCH_ACTIVE=1
```

`AL_DISPATCH_ACTIVE` is a depth counter for active dispatch ancestors. A top-level `al dispatch` call starts from depth 0 and launches its target with `AL_DISPATCH_ACTIVE=1`. A nested dispatch is allowed only while the current depth is lower than `dispatch.max_depth`; the nested target receives the incremented value.

## Config

Dispatch has a repo-wide depth limit plus optional per-caller target defaults:

```toml
[dispatch]
max_depth = 1

[agents.claude.dispatch]
default_agent = "random"

[agents.codex.dispatch]
default_agent = "random"

[agents.antigravity.dispatch]
default_agent = "random"
```

`dispatch.max_depth` is the repo-wide maximum depth, including the initial dispatch call. The default is `1`, which means a dispatched target cannot call `al dispatch` again. Set it to `2` to allow one nested dispatch level, `3` for two nested levels, and so on. Values must be positive integers.

`default_agent` accepts `random`, `codex`, `claude`, or `antigravity`. If omitted for a known caller, the built-in default is `random`.

For reproducible scripts and continuous integration, pass a concrete `--agent` or set `agents.<caller>.dispatch.default_agent` to a concrete target. `random` is intended for conversational second-opinion use, not deterministic automation.

Fresh single-target installs should also use a concrete target. If the only dispatch-capable target is the known caller, the built-in `random` default excludes that caller and fails with no eligible target instead of silently dispatching back to the same agent.

## Resolution Rules

1. If `--agent` is provided, dispatch uses that value.
2. If the provided value is `random`, dispatch chooses randomly from enabled dispatch-capable agents. If the caller is known, the caller is excluded.
3. If `--agent` is omitted and the caller is known, dispatch uses `agents.<caller>.dispatch.default_agent`, defaulting to `random`.
4. If `--agent` is omitted and the caller is unknown, dispatch fails.
5. Explicit same-agent dispatch is allowed. If the caller is `claude` and `--agent claude` is provided, dispatch treats that as intentional.
6. The selected target is surfaced: the CLI writes it to standard error when the target was selected implicitly (from the caller's configured default) or randomly. The literal forms are `Dispatch target: <agent> (from agents.<caller>.dispatch.default_agent)` for implicit selection and `Dispatch target: <agent> (random selection)` for random selection. Quiet mode (`--quiet` or `warnings.noise_mode = "quiet"`) suppresses this notice along with other informational output.
7. If random selection has no eligible targets after exclusions, dispatch fails clearly instead of falling back to the caller.

## Exit Status

Dispatch uses stable wrapper-owned exit categories:

- `0`: the selected target completed successfully
- `64`: usage or target-resolution failure, including missing prompt/skill, unknown target, unknown caller with omitted `--agent`, or unsupported override field for the selected target
- `65`: Agent Layer configuration or state failure, including missing source skill, stale/missing target skill projection, malformed config, or disabled target
- `69`: target unavailable, including missing target binary, unauthenticated/unusable target CLI detected before launch, or no eligible target in the random-selection pool
- `70`: target or adapter failure, including target subprocess non-zero exit, target rejection of a custom override value, changed structured stream shape, unreadable target output, or internal dispatch error
- `75`: nested dispatch blocked by `dispatch.max_depth` or invalid `AL_DISPATCH_ACTIVE`
- `130`: interrupted by `SIGINT`
- `143`: terminated by `SIGTERM`

Target-agent exit codes are not propagated directly. If a target exits non-zero, dispatch exits `70` and writes the target exit code to standard error.

## Scope

Agent Dispatch is intentionally text-first:

- prompt input is plain text from an argument or standard input
- prompt text is optional when a skill is provided
- output is streamed human-readable text where the target's official headless interface supports it
- dispatch creates a per-run temporary directory at `.agent-layer/tmp/runs/<id>` (`0o700`) consistent with normal `al claude`/`al codex`/`al agy` launches; no other persistent artifacts (prompt files, output logs, run manifests) are written by default
- the target run uses the current repository's Agent Layer configuration
- dispatch does not target other repositories or machines
- supported target agents are Codex, Claude, and Antigravity
- dispatch uses one CLI implementation path

It is not a multi-agent chat UI, shared workspace, queueing system, or long-lived orchestration layer.

## Runtime Notes

Dispatch should preserve the target agent's normal Agent Layer behavior, including generated instructions, skills, approvals, sandbox settings, and client-specific configuration. This means dispatch can grant the target agent the capabilities configured for that target even when the caller is more restricted; `al dispatch` is not a permission-containment boundary.

Headless target runs must be launched with non-interactive target CLI modes. Dispatch cannot answer approval prompts on behalf of the caller; the selected target must be configured with the approvals and sandbox settings needed for the intended work.

For final-answer-only targets such as Codex v1, callers should expect sparse stderr output while the answer text remains unavailable until the target run completes. Dispatch forwards target stream events as compact stderr lines when they arrive and stays silent otherwise; it does not synthesize heartbeats. Genuine target silence is reported as silence — dispatch cannot distinguish a long inference from a frozen subprocess.

Dispatch forwards cancellation signals to the target process and waits for it to exit before returning.
