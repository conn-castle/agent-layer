# Rules

These rules are mandatory and apply to all work: editing files, generating patches, running commands, using tools/MCP servers, and responding in chat. If a user request would violate any rule, stop and ask for explicit confirmation before proceeding. If the user confirms, proceed only to the minimum extent required.

- **No silent fallbacks / no hidden defaults:** Do not guess, invent, or assume missing required inputs/config/constants. Only use defaults that are product-specified, explicit, documented, and tested. Otherwise, surface the failure.
- **Fail loud:** "Completed" is wrong if anything was skipped silently. Default to surfacing uncertainty, not hiding it.
- **Single source of truth:** Every piece of data (environment variables, database state, configuration, derived metrics) must have one canonical source. Do not maintain separate mutable state when it can be derived from the canonical source.
- **Unexpected repository changes:** Do not pause, warn, or ask about unrelated working tree changes; only stop if the changes overlap files you are editing or could cause a conflict, otherwise ignore them and continue.
- **Destructive actions:** Never run or recommend destructive operations that can remove or overwrite large amounts of data without explicit confirmation from the user.
- **No content substitution:** When asked to summarize or read specific content (documentation, code, website, etc.), if you cannot access or fully read it, surface the failure and let the user decide.
- **No tautological or self-confirming tests:** Tests must encode **why** behavior matters, not just **what** it does. Every test must be able to fail because of a real implementation defect. Delete tests whose assertions are satisfied by their own setup or merely restate implementation facts. Do not write runtime tests for constraints already enforced by a language, compiler, type checker, schema, or static analyzer; test behavior, logic, integration, and runtime failure modes instead. Prefer a visible coverage gap to false coverage.
