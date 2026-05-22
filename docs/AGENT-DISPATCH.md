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

Use `--model` and `--reasoning-effort` for per-run overrides when a task needs a specific model behavior:

```bash
al dispatch --agent codex --model <model-name> --reasoning-effort high "Review this plan artifact set."
```

Dispatch streams the target's human-readable output as supported by the target CLI. Target answer text is written to standard output. Dispatch status, selected-target notices, compact progress, and errors are written to standard error.

When a target CLI exposes partial output only through an official structured stream, Dispatch may consume that documented stream internally and emit readable text to the caller. Dispatch must not expose raw event streams as its public output and must not scrape interactive terminal interfaces.

Verified target behavior:

- Claude streams partial answer text through its official print-mode structured stream.
- Antigravity streams readable answer text through `agy --print`.
- Codex uses stable `codex exec`. It streams lifecycle/progress events, but assistant answer text is emitted only as a final message when the run completes.

`--agent` selects the target CLI/runtime. Supported values are `codex`, `claude`, `antigravity`, and `random`. If `--agent` is omitted, Agent Layer must know the calling agent. If the caller is unknown, dispatch fails and asks for `--agent`.

`--model` and `--reasoning-effort` are optional. If omitted, dispatch uses the same Agent Layer and client defaults that normal `al <agent>` launches use. Custom model strings are accepted for targets whose Agent Layer config accepts custom model values; unsupported override flags fail clearly instead of being ignored.

`al dispatch options` lists available dispatch targets, dispatch capability, random-selection eligibility, configured model and reasoning values, known suggestions from Agent Layer's field catalog, whether custom values are accepted, detected caller, and the current random pool. Use `--json` for structured output.

## Generated Instructions

Agent Layer generated instructions describe `al dispatch` so agents can discover and use the command during normal tool execution. Agents can run `al dispatch options` when they need to inspect the currently available targets, models, reasoning efforts, caller detection, or random-selection behavior.

## Skills

Dispatch uses the current project's canonical Agent Layer skill source, `.agent-layer/skills/`. Caller-side skill availability is irrelevant; `--agent codex --skill review-plan` validates `review-plan` against Agent Layer's source skills and the selected target's skill support before launch.

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

Dispatch sets a recursion guard while running a target agent:

```bash
AL_DISPATCH_ACTIVE=1
```

Dispatch supports depth 1. If `AL_DISPATCH_ACTIVE=1` is already present, dispatch fails to prevent nested agent-call chains.

## Config

Dispatch defaults are configured under the calling agent:

```toml
[agents.claude.dispatch]
default_agent = "random"

[agents.codex.dispatch]
default_agent = "random"

[agents.antigravity.dispatch]
default_agent = "random"
```

`default_agent` accepts `random`, `codex`, `claude`, or `antigravity`. If omitted for a known caller, the built-in default is `random`.

For reproducible scripts and continuous integration, pass a concrete `--agent` or set `agents.<caller>.dispatch.default_agent` to a concrete target. `random` is intended for conversational second-opinion use, not deterministic automation.

## Resolution Rules

1. If `agent` or `--agent` is provided, dispatch uses that value.
2. If the provided value is `random`, dispatch chooses randomly from enabled dispatch-capable agents. If the caller is known, the caller is excluded.
3. If `agent` is omitted and the caller is known, dispatch uses `agents.<caller>.dispatch.default_agent`, defaulting to `random`.
4. If `agent` is omitted and the caller is unknown, dispatch fails.
5. Explicit same-agent dispatch is allowed. If the caller is `claude` and `--agent claude` is provided, dispatch treats that as intentional.
6. If random selection has no eligible targets after exclusions, dispatch fails clearly instead of falling back to the caller.

## Scope

Agent Dispatch is intentionally text-first:

- prompt input is plain text from an argument or standard input
- prompt text is optional when a skill is provided
- output is streamed human-readable text where the target's official headless interface supports it
- no persistent run artifacts are created by default
- the target run uses the current repository's Agent Layer configuration
- dispatch does not target other repositories or machines
- supported target agents are Codex, Claude, and Antigravity
- dispatch uses one CLI implementation path

It is not a multi-agent chat UI, shared workspace, queueing system, or long-lived orchestration layer.

## Runtime Notes

Dispatch should preserve the target agent's normal Agent Layer behavior, including generated instructions, skills, approvals, sandbox settings, and client-specific configuration. This means dispatch can grant the target agent the capabilities configured for that target even when the caller is more restricted; `al dispatch` is not a permission-containment boundary.

Headless target runs must be launched with non-interactive target CLI modes. Dispatch cannot answer approval prompts on behalf of the caller; the selected target must be configured with the approvals and sandbox settings needed for the intended work.

Dispatch forwards cancellation signals to the target process and waits for it to exit before returning.
