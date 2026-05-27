---
name: playwright-cli
description: Use playwright-cli for real-browser UI automation, snapshots, screenshots, Playwright test debugging, and interactive Playwright test generation. Trigger when a task needs browser interaction, UI inspection, end-to-end verification, or Playwright test repair. Do not use for generic test running, web search, non-browser docs, or API-only work.
license: Apache-2.0
compatibility: Requires playwright-cli and any browser, app, or Playwright test environment needed by the task. The upstream `playwright-cli` npm package was marked deprecated as of 2026-05; verify the package status before relying on a long-term install and consult Playwright release notes for the maintained replacement.
allowed-tools: Bash(playwright-cli:*) Bash(npx:*) Bash(npm:*)
---

# Browser Automation with playwright-cli

> Modified by Conn Castle Studios on 2026-05-26 from the Apache-2.0
> playwright-cli skill: reduced duplicated command reference material and
> tailored the workflow guidance for Agent Layer.

Use `playwright-cli` as the browser control surface. This skill provides
routing, safety, and workflow rules; installed CLI help provides command
syntax.

## Defaults

- Use snapshots and element refs for navigation.
- Use screenshots only when visual evidence matters.
- Use references only for Playwright test debugging or test generation.
- Create no durable artifact unless the task needs one.

## Required artifacts

- No artifact is required for ordinary inspection.
- Put agent-only scripts, state files, traces, videos, screenshots, and saved
  snapshots under `.agent-layer/tmp/`.
- Report every artifact path created.

## Global constraints

- Run `playwright-cli --help` before the first `playwright-cli` command in a
  session.
- Run `playwright-cli --help <command>` before using a non-obvious subcommand
  or flag.
- Treat installed CLI help as the source of truth for commands, arguments,
  flags, and defaults.
- Keep short help-probe lists as routing hints, not command documentation.
  Always query live help before using a listed command.
- If CLI, browser, app, or test setup is missing, stop and report the missing
  requirement; do not install packages or browsers unless the user asked for
  setup.
- Do not hard-code or expose secrets in commands, tests, storage state, traces,
  videos, screenshots, or logs.

## Human checkpoints

- Ask before installing CLI packages, installing browsers, changing browser
  configuration, or using a persistent profile outside an explicit user
  request.
- Ask before saving or reusing authentication state outside `.agent-layer/tmp/`.
- Ask before leaving a browser session running when the user did not request an
  interactive handoff.

## Workflow

1. Start or connect to the browser using the command shape from live help.
2. Capture semantic page state and use element refs for normal interaction.
3. Use screenshots only for visual verification.
4. Use selectors or Playwright locators only when refs are insufficient.
5. When refs omit needed details, choose the inspection or output mode from
   live subcommand help.
6. Use dashboard or visual-aid features from live help when the user needs to
   identify or comment on UI.
7. End, disconnect from, or report any remaining session before handoff.

## Help probes

Use these as `playwright-cli --help <command>` targets, not as syntax
documentation:

- Browser/session lifecycle: `open`, `attach`, `list`, `close`, `detach`,
  `close-all`, `kill-all`.
- Page state and inspection: `snapshot`, `screenshot`, `eval`, `console`,
  `requests`, `request`.
- UI clarification: `show`, `highlight`.
- Storage and auth state: `state-save`, `state-load`, cookie and storage
  commands from root help.
- Network mocking and advanced browser logic: `route`, `run-code`.
- Debugging evidence: `tracing-start`, `tracing-stop`, `video-start`,
  `video-stop`.

## Conditional references

- Read [references/playwright-tests.md](references/playwright-tests.md) when
  running, debugging, or healing Playwright tests, especially with
  `npx playwright test --debug=cli` sessions.
- Read [references/test-generation.md](references/test-generation.md) when
  creating or updating Playwright tests from interactive browser actions,
  generated Playwright code, or manually added assertions.

## Operational notes

- Named sessions isolate cookies, storage, cache, history, and tabs; use
  semantic names and clean them up.
- Saved storage state can contain credentials; keep it temporary and
  uncommitted.
- For failed HTTP activity, use live help to find inspection commands; do not
  invent command groups.
- Prefer built-in commands for simple mocks or browser operations; use
  code-execution from live help only when a simpler command cannot express the
  behavior.
- Use replay or recording artifacts only when they provide needed evidence.
  Clean up large artifacts.

## Guardrails

- Do not guess command names, flags, session behavior, or install commands.
- Do not use Playwright automation to bypass authentication, authorization, or
  site terms.
- Do not hard-code secrets, tokens, or real credentials into commands, test
  files, storage state, logs, screenshots, videos, or traces.
- Do not add sleeps or skip Playwright hooks to make a test pass. Diagnose the
  app state and wait for specific, testable readiness instead.

## Definition of done

- The installed CLI help was checked for each command shape used.
- Browser sessions were closed, detached, or intentionally left running with
  that state reported.
- Generated artifacts were saved only where appropriate and secrets were not
  committed or exposed.
- For Playwright test work, the relevant reference file was followed and the
  smallest meaningful test was rerun.

## Final handoff

State which pages, flows, or tests were exercised; name any files or artifacts
created; summarize cleanup; and report any verification that could not be run.
