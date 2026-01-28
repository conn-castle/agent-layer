# Rules

These rules are mandatory and apply to all work: editing files, generating patches, running commands, using tools/MCP servers, and responding in chat. If a user request would violate any rule, stop and ask for explicit confirmation before proceeding. If the user confirms, proceed only to the minimum extent required.

- **No silent fallbacks / no hidden defaults (code + MCP servers + chat):** Do not guess, invent, or assume missing required inputs/config/constants. Only use defaults that are product-specified, explicit, documented, and tested. Otherwise, surface the failure.
- **Single source of truth:** Do not maintain duplicate mutable state when it can be derived from a canonical source.
- **UTC-only internals:** Store, compute, and transport time in UTC; local time display is presentation-only.
- **Response protocol:** Answer direct questions explicitly before proposing or generating changes.
- **Environment files:** Never modify the `.env` file. Only modify the `.env.example` file. If a change is needed in `.env`, ask the user to make it and provide exact, copyable instructions.
- **Repository boundary:** Never delete files outside of the repository. If a file outside of the repository needs to be deleted, ask the user to delete it.
- **Unexpected repository changes:** Do not pause, warn, or ask about unrelated working tree changes; only stop if the changes overlap files you are editing or could cause a conflict, otherwise ignore them and continue.
- **Secrets and credentials:** Never add secrets, private keys, access tokens, or credentials to repository files, logs, or outputs. Use placeholders and documented variable names in `.env.example`, and instruct the user to supply real values locally.
- **Destructive actions:** Never run or recommend destructive operations that can remove or overwrite large amounts of data without explicit confirmation from the user, and always name the exact paths that would be affected.
- **No system Python:** Never use system Python. Always prefer the project virtual environment Python, and if no virtual environment exists, ask the user if you should create one.
- **Verification claims:** Never claim that you ran commands, tests, or verification unless you actually did and observed the output. If you did not run verification, state what should be run and why.
- **Test integrity:** Never skip tests. If a test fails, fix the underlying issue or fix the test. If a test cannot run due to missing dependencies, ensure the dependencies are available rather than skipping the test.
