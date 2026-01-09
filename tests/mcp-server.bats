#!/usr/bin/env bats

load "helpers.bash"

@test "prompt MCP server exposes tools/list handler" {
  local server_file
  server_file="$AGENTLAYER_ROOT/mcp/agent-layer-prompts/server.mjs"

  [ -f "$server_file" ]
  grep -q "ListToolsRequestSchema" "$server_file"
  grep -q "setRequestHandler(ListToolsRequestSchema" "$server_file"
  grep -Eq "capabilities:.*tools" "$server_file"
}
