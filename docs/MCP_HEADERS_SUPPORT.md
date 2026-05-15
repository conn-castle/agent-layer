# MCP HTTP Header Projection in Agent Layer

Agent Layer can project custom HTTP headers (e.g., `Authorization`, `X-API-Key`) into each supported MCP client configuration so HTTP-based MCP servers authenticate consistently across tools.

The intent is consistency without leakage: define headers once in `.agent-layer/config.toml`, keep secrets in `.agent-layer/.env`, and let Agent Layer translate those headers into each client’s native format without hardcoding credentials.

> Scope note: `headers` apply to **HTTP-based MCP transports** (Streamable HTTP and/or SSE). For **stdio** MCP servers, credentials are typically handled via environment variables passed to the server process (`env`).

## Client support matrix

| Client | Repo-local config (recommended) | Also supported (user/global) | Header mechanism |
|---|---|---|---|
| Gemini CLI | `.gemini/settings.json` | `~/.gemini/settings.json` | `headers` object on the server entry |
| Claude Code | `.mcp.json` | `~/.claude.json` | `headers` object on the server entry (`type: "http"`) |
| VS Code (Copilot Chat) | `.vscode/mcp.json` | user settings `mcp.json` | `headers` object (supports `${input:...}` indirection) |
| Codex CLI (+ IDE extension) | `.codex/config.toml` (trusted projects) | `~/.codex/config.toml` | `bearer_token_env_var`, `env_http_headers`, `http_headers` |
| Copilot CLI | `.copilot/mcp-config.json` | `~/.copilot/mcp-config.json` | `headers` object on the server entry |

## Client details

### 1) Gemini CLI

**File (repo-local):** `.gemini/settings.json`

**SSE HTTP server with headers (default `http_transport = "sse"`):**
```json
{
  "mcpServers": {
    "my-server": {
      "url": "https://api.example.com/mcp",
      "headers": {
        "Authorization": "Bearer ${MY_API_TOKEN}",
        "X-Custom-Header": "Value"
      }
    }
  }
}
```

Notes:

* Gemini CLI settings files support environment variable references inside string values (e.g., `${MY_API_TOKEN}`), so you can keep secrets out of the file by referencing env vars.
* Agent Layer emits `url` for SSE transport and `httpUrl` for streamable HTTP transport (`http_transport = "streamable"`).
* If your goal is to reduce tool count from a “huge” server, Gemini also supports `includeTools` / `excludeTools` on the server entry (client-side tool filtering).

### 2) Claude Code

**File (repo-local):** `.mcp.json` (project scope)

**HTTP server with headers:**

```json
{
  "mcpServers": {
    "my-server": {
      "type": "http",
      "url": "https://api.example.com/mcp",
      "headers": {
        "Authorization": "Bearer ${MY_API_TOKEN}"
      }
    }
  }
}
```

Notes:

* Claude Code supports environment variable expansion inside `.mcp.json` (including within `headers`), so the file can be shared without embedding secrets.
* Local/user scopes are stored in `~/.claude.json`; Agent Layer typically writes repo-local `.mcp.json`.

### 3) VS Code (Copilot Chat)

**File (repo-local):** `.vscode/mcp.json`

VS Code supports `headers` for HTTP MCP servers. For secrets, prefer using `inputs` so you don't hardcode tokens:

```json
{
  "servers": {
    "my-server": {
      "type": "http",
      "url": "https://api.example.com/mcp",
      "headers": {
        "Authorization": "Bearer ${input:my-server-token}"
      }
    }
  },
  "inputs": [
    {
      "id": "my-server-token",
      "type": "promptString",
      "description": "API token for my-server"
    }
  ]
}
```

Notes:

* Agent Layer currently writes headers using `${env:VAR}` placeholders in `.vscode/mcp.json` (it does not auto-generate an `inputs` block).

### 4) Copilot CLI

**File (repo-local):** `.copilot/mcp-config.json`

**HTTP server with headers:**

```json
{
  "mcpServers": {
    "my-server": {
      "type": "http",
      "url": "https://api.example.com/mcp",
      "headers": {
        "Authorization": "Bearer ${AL_MY_API_TOKEN}"
      },
      "tools": ["*"]
    }
  }
}
```

Notes:

* Copilot CLI supports environment variable placeholders inside `mcp-config.json` (e.g., `${AL_MY_API_TOKEN}`), matching the same `${VAR}` syntax used by Gemini and Claude.
* Agent Layer preserves raw `${VAR}` references in the generated file — secrets are never resolved into the config.
* The `tools` field is always set to `["*"]` (all tools enabled) because Copilot CLI does not support per-tool filtering.

### 5) Codex CLI (and Codex IDE extension)

**File (repo-local):** `.codex/config.toml` (trusted projects)
**File (user):** `~/.codex/config.toml`

Agent Layer writes the current absolute repo root under `[projects."<repo root>"]` with `trust_level = "trusted"` before MCP server tables.

Codex supports three ways to send headers to HTTP MCP servers:

* `bearer_token_env_var` (recommended for `Authorization: Bearer ...`)
* `env_http_headers` (recommended for other secret headers)
* `http_headers` (static headers; use for non-secret constants)

```toml
[mcp_servers.my-server]
url = "https://api.example.com/mcp"

# Preferred: Authorization bearer token from env var (Codex adds the Authorization bearer header)
bearer_token_env_var = "MY_API_TOKEN"

# Preferred: other headers sourced from env vars (no secrets in config.toml)
env_http_headers = { "X-Api-Key" = "MY_OTHER_SECRET" }

# OK for non-secret constants
http_headers = { "X-Client" = "agent-layer" }
```

Notes:

* Codex also supports per-server tool filtering: `enabled_tools` and `disabled_tools`.

---

## Implementation strategy in Agent Layer

### 1) Source of truth

Users configure MCP servers and headers in `.agent-layer/config.toml`, and store secrets in `.agent-layer/.env`.

Example:

```toml
[[mcp.servers]]
id = "my-server"
enabled = true
transport = "http"
url = "https://api.example.com/mcp"
headers = { Authorization = "Bearer ${AL_MY_API_TOKEN}", "X-Api-Key" = "${AL_MY_OTHER_SECRET}" }
```

### 2) Normalize without leaking secrets

Do **not** resolve `${VAR}` into concrete secret values when writing generated client configs.

The projection layer uses `ClientPlaceholderResolver` (`internal/projection/resolvers.go`) to preserve placeholders in each client’s native syntax. The resolver takes a format string (e.g., `${%s}` or `${env:%s}`) and returns a function that:

* **Resolves built-in env vars** (like `AL_REPO_ROOT`) to their actual values.
* **Preserves user env vars** as client-specific placeholders (e.g., `${MY_VAR}` for most clients, `${env:MY_VAR}` for VS Code).

Codex is the exception: because Codex requires distinct config keys for different header types, `splitCodexHeaders()` (`internal/sync/codex.go`) classifies headers into a `codexHeaderSpec` struct with three categories: `BearerTokenEnvVar` (for `Authorization: Bearer ${VAR}`), `EnvHeaders` (full placeholder values), and `HTTPHeaders` (literal values).

Env vars may still be resolved at runtime for:

* validation (detect missing env vars early),
* launching clients (ensuring `.agent-layer/.env` is loaded into the process environment).

### 3) Projection pipeline

* `internal/projection` (resolver/normalizer):

  * `ClientPlaceholderResolver()` returns a per-client `EnvVarResolver` function.
  * `ResolveMCPServers()` applies the resolver to produce `ResolvedMCPServer` structs with headers, URLs, args, and env vars in the target client’s placeholder syntax.

* `internal/sync` (client writers):

  * Each writer calls `ResolveMCPServers()` with its client syntax, then converts the result into the client’s config format.

### 4) Client projection rules

Each writer receives `ResolvedMCPServer` structs with placeholders already in the target syntax:

* **Gemini CLI**: emit `headers` map with the raw string (preserve `${VAR}`).
* **Claude Code**: emit `headers` map with the raw string (preserve `${VAR}` / `${VAR:-default}`).
* **Copilot CLI**: emit `headers` map with the raw string (preserve `${VAR}`).
* **VS Code**: emit `headers` map using `${env:VAR}` placeholders (does not auto-generate an `inputs` block; see section 3 Notes).
* **Codex**:

  * `Authorization: Bearer ${VAR}` → `bearer_token_env_var = "VAR"`
  * `${VAR}` on non-Authorization headers → `env_http_headers = { "<Header-Name>" = "VAR" }`
  * literal, non-secret headers → `http_headers = { ... }`

### 5) Avoid “reverse-engineering” env vars after resolution

Do not attempt to infer `VAR` from a resolved secret value.
Once `${VAR}` has been expanded, the original variable name is lost and you risk writing secrets into generated files.

Keep the raw (unresolved) header string available through the projection pipeline, and only resolve values for runtime validation/launch.
