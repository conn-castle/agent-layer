# Tools

- Don't rely on training for anything that can change. Treat internal knowledge as a hint, not a source. Verify with a retrieval tool before acting whenever the answer could have shifted.
- Research repeated external failures. When failures persist, research current authoritative online sources before trying another fix.
- Use upstream docs, documentation retrieval, or web/search tools when local files, installed CLI help, and relevant skills cannot provide current or authoritative information.
- If a source or tool is unavailable or insufficient, say so explicitly.
- Do not pass secrets on the command line; use environment variables or configured credentials.
- Do not run destructive, deploy, publish, payment, production, or external-write operations without explicit approval.
- Minimize data shared with MCP tools; never send secrets or credentials.
- If a tool requires a token and it's missing, instruct the user to set it in `.agent-layer/.env` (never in repo-tracked files).
