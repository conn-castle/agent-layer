#!/usr/bin/env bats

# Tests for the MCP prompt server entrypoint.
# Load shared helpers for temp roots and stub binaries.
load "helpers.bash"

# Test: prompt MCP server exposes tools/list handler
@test "prompt MCP server exposes tools/list handler" {
  local server_file
  server_file="$AGENT_LAYER_ROOT/src/mcp/agent-layer-prompts/server.mjs"

  [ -f "$server_file" ]
  grep -q "ListToolsRequestSchema" "$server_file"
  grep -q "setRequestHandler(ListToolsRequestSchema" "$server_file"
  grep -Eq "capabilities:.*tools" "$server_file"
}

# Test: prompt MCP server fails fast when workflows are missing
@test "prompt MCP server fails fast when workflows are missing" {
  local server_file
  server_file="$AGENT_LAYER_ROOT/src/mcp/agent-layer-prompts/server.mjs"

  [ -f "$server_file" ]
  grep -q "could not find .agent-layer/config/workflows" "$server_file"
  grep -q "no workflow files found" "$server_file"
}

# Test: prompt MCP server responds to list requests
@test "prompt MCP server responds to list requests" {
  local sdk_dir
  sdk_dir="$AGENT_LAYER_ROOT/src/mcp/agent-layer-prompts/node_modules/@modelcontextprotocol/sdk"

  if [[ ! -d "$sdk_dir" ]]; then
    skip "MCP server dependencies not installed."
  fi

  run node "$AGENT_LAYER_ROOT/tests/mcp-runtime.mjs"
  [ "$status" -eq 0 ]
}
