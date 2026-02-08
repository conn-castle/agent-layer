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

## Client details

### 1) Gemini CLI

**File (repo-local):** `.gemini/settings.json`

**HTTP server with headers:**
```json
{
  "mcpServers": {
    "my-server": {
      "httpUrl": "https://api.example.com/mcp",
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

* Agent Layer can generate the `inputs` block automatically when a header value is configured as an env-var reference in `.agent-layer/config.toml` (see Implementation Strategy).

### 4) Codex CLI (and Codex IDE extension)

**File (repo-local):** `.codex/config.toml` (trusted projects)
**File (user):** `~/.codex/config.toml`

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

Instead, parse header values into a typed representation:

* **Literal**: no `${...}` present → safe to write as-is
* **EnvRef(VAR)**: value is exactly `${VAR}` → can be mapped to env-based mechanisms (Codex `env_http_headers`, etc.)
* **BearerEnvRef(VAR)**: `Authorization` value matches `Bearer ${VAR}` → can be mapped to Codex `bearer_token_env_var`

You may still resolve env vars at runtime for:

* validation (detect missing env vars early),
* launching clients (ensuring `.agent-layer/.env` is loaded into the process environment).

### 3) Projection pipeline (recommended split)

* `internal/projection` (resolver/normalizer):

  * Read `.agent-layer/config.toml`.
  * Produce a normalized MCP server struct (keep **raw** header strings and a parsed/typed header representation).
  * Optionally compute **resolved** values for validation only (never for writing).

* `internal/sync` (client writers):

  * Convert the normalized server struct into each client’s config format, following the rules below.

### 4) Client projection rules

Given a normalized header spec:

* **Gemini CLI**: emit `headers` map with the raw string (preserve `${VAR}`).
* **Claude Code**: emit `headers` map with the raw string (preserve `${VAR}` / `${VAR:-default}`).
* **VS Code**:

  * for literal headers: emit the literal value.
  * for secrets: generate an `inputs` entry and rewrite the header to `${input:<id>}` (plus any needed prefix like `Bearer `).
* **Codex**:

  * `Authorization: Bearer ${VAR}` → `bearer_token_env_var = "VAR"`
  * `${VAR}` on non-Authorization headers → `env_http_headers = { "<Header-Name>" = "VAR" }`
  * literal, non-secret headers → `http_headers = { ... }`

### 5) Avoid “reverse-engineering” env vars after resolution

Do not attempt to infer `VAR` from a resolved secret value.
Once `${VAR}` has been expanded, the original variable name is lost and you risk writing secrets into generated files.

Keep the raw (unresolved) header string available through the projection pipeline, and only resolve values for runtime validation/launch.
