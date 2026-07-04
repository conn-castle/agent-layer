---
name: tavily-web
description: |
  Use Tavily CLI for web search, URL extraction, site mapping, and cited research. Trigger when the user needs current web info, asks to search/read a page, provides URLs, or wants a sourced report. Do not use for browser automation, local docs, or Tavily setup.
compatibility: Requires the Tavily CLI (`tvly`), authentication, and network access.
allowed-tools: Bash(tvly:*)
---

# Tavily Web

Use the Tavily CLI as Agent Layer's web retrieval path. Route the request to the smallest useful Tavily command, then summarize the sourced result.

## Defaults

- Use `tvly search` for general web discovery when no URL or known site is provided.
- Use `tvly extract` directly when the user provides exact URLs.
- Use `tvly map` only when the user provides a known site/domain and needs URL discovery.
- Use `tvly research` only when the user wants cited synthesis, comparison, or report-style analysis.
- Prefer `--json` when the CLI supports it so results can be parsed before summarizing.

## Global constraints

- Run `tvly --help` before the first Tavily command in a session, then run
  `tvly --status` to verify readiness.
- Treat installed CLI help as the source of truth for commands, arguments,
  flags, output modes, and defaults.
- If `tvly` is missing, unauthenticated, or exits non-zero, stop and tell the user to install or authenticate the Tavily CLI.
- Do not install Tavily from this skill. Do not run `curl | bash`, package managers, or login commands unless the user explicitly asks for setup help.
- Do not use Tavily MCP or Fetch MCP as a fallback.
- Do not use `tvly crawl` in the default Agent Layer workflow.
- When saving output or scratch data, write under `.agent-layer/tmp/`.
- Before using non-obvious flags, run `tvly <command> --help` or `tvly research <subcommand> --help`; do not rely on memorized CLI syntax.

## Required artifacts

- No artifact is required for quick lookups.
- If saving intermediate retrieval output, use `.agent-layer/tmp/tavily-web.<run-id>.<type>.json` or `.agent-layer/tmp/tavily-web.<run-id>.<type>.md`.
- Final answers that depend on web retrieval must name or cite the source URLs used.

## Human checkpoints

- Ask before installing the Tavily CLI, running login/authentication commands, or changing Tavily configuration.
- Do not ask for permission merely to run read-only `tvly search`, `tvly extract`, `tvly map`, or `tvly research` commands.

## Command routing

Use `tvly search` when:

- The user does not provide a specific URL.
- The task is source discovery, current facts, recent news, or finding relevant pages.
- You need domain filters, time filters, or a small set of result snippets.

Use `tvly extract` when:

- The user provides one or more URLs.
- A search or map result identifies pages that need full content.
- You need clean page content, JavaScript-heavy page extraction, or query-focused chunks.

Use `tvly map` when:

- The user knows the site/domain but not the exact page.
- You need URL discovery or site structure without extracting page contents.
- The efficient path is map first, then extract only selected URLs.

Use `tvly research` when:

- The user asks for a report, comparison, market scan, literature-style review, or multi-source cited synthesis.
- A simple search would only discover sources, but the desired output is analysis grounded in citations.

## Workflow

1. Inspect root help with `tvly --help`, then confirm CLI readiness with
   `tvly --status`.
2. Select the smallest command that matches the request.
3. Run command-specific help if flags matter:
   - `tvly search --help`
   - `tvly extract --help`
   - `tvly map --help`
   - `tvly research run --help`
   - `tvly research status --help`
   - `tvly research poll --help`
4. Prefer `--json` for agent workflows so results can be parsed and summarized deliberately.
5. Keep queries short and search-like. Break complex prompts into focused searches instead of sending one long prompt to search.
6. For known URLs, skip search and go directly to extract.
7. For known sites with unknown pages, map first and extract only the useful URLs.
8. For long-running research, use Tavily's async status/poll flow when it prevents blocking or losing the request ID.

## Guardrails

- Do not dump large raw outputs into the conversation when a narrower query, domain filter, time filter, or selected URL list would answer the question.
- Do not ask Tavily to synthesize when the user only needs source discovery; use search and summarize the results yourself.
- Do not use search when the user gave the exact URL; use extract.
- Do not use map for general web search; use map only for known sites/domains.
- Do not use research for quick factual lookups unless the user explicitly wants a cited report.
- Do not treat Tavily output as authoritative by itself. Report source URLs and distinguish sourced facts from your synthesis.

## Definition of done

- The selected Tavily command path matches the user's retrieval intent.
- The answer names or cites source URLs when it depends on web retrieval.
- Any saved retrieval artifacts are under `.agent-layer/tmp/` using the `tavily-web.<run-id>.<type>` naming pattern.
- Missing CLI, authentication, inaccessible pages, timeouts, or incomplete coverage are reported explicitly.

## Final handoff

State which Tavily command path was used, summarize the result, include source URLs when relevant, and mention any retrieval limits.
