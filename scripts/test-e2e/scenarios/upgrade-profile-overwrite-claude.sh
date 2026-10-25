#!/usr/bin/env bash
# Upgrade a supported project with transport-incompatible MCP fields, then
# verify a clean wizard profile repairs those fields. This does not exercise
# the interactive wizard sanitizer path.

run_scenario_upgrade_profile_overwrite_claude() {
  section "Upgrade + wizard profile repairs polluted MCP fields + al claude"

  if skip_if_no_oldest_binary; then return; fi
  E2E_UPGRADE_SCENARIO_COUNT=$((E2E_UPGRADE_SCENARIO_COUNT + 1))

  local repo_dir
  repo_dir="$(setup_scenario_dir)"

  setup_old_version_via_binary "$repo_dir" "$E2E_OLDEST_BINARY"
  assert_al_version_content "$repo_dir" "$E2E_OLDEST_VERSION"

  # Inject transport-incompatible fields into config.toml to simulate a
  # polluted state:
  #   - context7 (stdio): add HTTP-only fields (headers, url)
  #   - github (HTTP): add stdio-only fields (command, args)
  local config="$repo_dir/.agent-layer/config.toml"

  # Bare 1.0.0 installs have no MCP servers. Seed the fixture explicitly.
  cat >> "$config" <<'MCP_EOF'

[[mcp.servers]]
id = "context7"
enabled = false
transport = "stdio"
command = "npx"
args = ["-y", "@upstash/context7-mcp@2.1.1"]
env = { CONTEXT7_API_KEY = "${AL_CONTEXT7_API_KEY}" }

[[mcp.servers]]
id = "github"
enabled = false
transport = "http"
http_transport = "streamable"
url = "https://api.githubcopilot.com/mcp/"
headers = { Authorization = "Bearer ${AL_GITHUB_PERSONAL_ACCESS_TOKEN}" }
MCP_EOF

  # Inject HTTP-only fields into context7 (stdio server)
  sed -i.bak '/^id = "context7"/,/^\[\[mcp/ {
    /^env = /a\
# SYNTHETIC: transport-incompatible fields (HTTP on stdio) for overwrite-repair testing\
headers = { Authorization = "Bearer ${AL_CONTEXT7_API_KEY}" }\
url = "https://example.com/context7"
  }' "$config"

  # Inject stdio-only fields into github (HTTP server)
  sed -i.bak '/^id = "github"/,/^\[\[mcp/ {
    /^headers = /a\
# SYNTHETIC: transport-incompatible fields (stdio on HTTP) for overwrite-repair testing\
command = "npx"\
args = ["-y", "example-github-mcp"]
  }' "$config"
  rm -f "${config}.bak"

  # Verify the injected fields are present
  assert_file_contains "$config" \
    'headers = { Authorization' \
    "injected transport-incompatible headers on context7 (stdio)"
  assert_file_contains "$config" \
    'url = "https://example.com/context7"' \
    "injected transport-incompatible url on context7 (stdio)"
  assert_file_contains "$config" \
    'command = "npx"' \
    "injected transport-incompatible command on github (HTTP)"
  assert_file_contains "$config" \
    'args = ["-y", "example-github-mcp"]' \
    "injected transport-incompatible args on github (HTTP)"

  local upgrade_output upgrade_rc=0
  upgrade_output=$(cd "$repo_dir" && al upgrade --yes --apply-managed-updates --apply-memory-updates --apply-deletions 2>&1) || upgrade_rc=$?
  if [[ $upgrade_rc -eq 0 ]]; then
    pass "al upgrade from $E2E_OLDEST_VERSION"
  else
    fail "al upgrade from $E2E_OLDEST_VERSION (exit code: $upgrade_rc)"
    echo "  output (first 10 lines):"
    echo "$upgrade_output" | head -10 | sed 's/^/    /'
  fi
  assert_no_crash_markers "$upgrade_output" "no crash markers in MCP-clean upgrade output"
  assert_output_contains "$upgrade_output" "Created upgrade snapshot" \
    "MCP-clean upgrade output mentions snapshot creation"
  assert_output_contains "$upgrade_output" "Running sync" \
    "MCP-clean upgrade output says sync ran"
  assert_output_contains "$upgrade_output" "Upgrade successful." \
    "MCP-clean upgrade output says successful"

  assert_al_version_content "$repo_dir" "$AL_E2E_VERSION_NO_V"
  # Write env values needed for MCP server resolution
  cat > "$repo_dir/.agent-layer/.env" <<'ENVEOF'
AL_CONTEXT7_API_KEY=e2e-test
AL_GITHUB_PERSONAL_ACCESS_TOKEN=e2e-test
AL_TAVILY_API_KEY=e2e-test
ENVEOF

  # Profile mode writes validated profile bytes verbatim; it does not exercise
  # the interactive sanitizer path. Build a scenario-local clean profile so this
  # e2e proves a profile overwrite can recover from polluted upgraded config.
  # Unit tests cover sanitizeMCPServerBlock and the interactive patch path.
  local overwrite_profile="$repo_dir/.agent-layer/.overwrite-profile.toml"
  cp "$E2E_DEFAULTS_TOML" "$overwrite_profile"
  cat >> "$overwrite_profile" <<'PROFILE_EOF'

[[mcp.servers]]
id = "context7"
enabled = false
transport = "stdio"
command = "npx"
args = ["-y", "@upstash/context7-mcp@2.1.1"]
env = { CONTEXT7_API_KEY = "${AL_CONTEXT7_API_KEY}" }

[[mcp.servers]]
id = "github"
enabled = false
transport = "http"
http_transport = "streamable"
url = "https://api.githubcopilot.com/mcp/"
headers = { Authorization = "Bearer ${AL_GITHUB_PERSONAL_ACCESS_TOKEN}" }
PROFILE_EOF

  # Run wizard with the scenario-local profile to overwrite the polluted blocks.
  local wizard_output wizard_rc=0
  wizard_output=$(cd "$repo_dir" && al wizard --profile "$overwrite_profile" --yes 2>&1) || wizard_rc=$?
  if [[ $wizard_rc -eq 0 ]]; then
    pass "al wizard clean profile after upgrade"
  else
    fail "al wizard clean profile after upgrade (exit code: $wizard_rc)"
    echo "  output (first 10 lines):"
    echo "$wizard_output" | head -10 | sed 's/^/    /'
  fi
  assert_no_crash_markers "$wizard_output" "no crash markers in clean profile wizard output"
  assert_output_contains "$wizard_output" "Running sync" \
    "clean profile wizard output says sync ran"
  assert_output_contains "$wizard_output" "Wizard completed" \
    "clean profile wizard output says completed"

  # Verify blocks survived profile overwrite (they should be cleaned, not removed).
  assert_file_contains "$repo_dir/.agent-layer/config.toml" 'id = "context7"' \
    "context7 block still present after wizard profile overwrite"
  assert_file_contains "$repo_dir/.agent-layer/config.toml" 'id = "github"' \
    "github block still present after wizard profile overwrite"

  # Verify context7 block (stdio) no longer has HTTP-only fields.
  # Extract just the context7 block to avoid false matches from other servers.
  local context7_block
  context7_block=$(sed -n '/^id = "context7"/,/^\[\[mcp/p' "$repo_dir/.agent-layer/config.toml" | head -20)

  assert_output_contains "$context7_block" 'transport = "stdio"' \
    "context7 block keeps stdio transport after profile overwrite"
  assert_output_contains "$context7_block" 'command = "npx"' \
    "context7 block has stdio command after profile overwrite"
  assert_output_contains "$context7_block" '@upstash/context7-mcp@2.1.1' \
    "context7 block has expected stdio package after profile overwrite"
  assert_output_contains "$context7_block" 'CONTEXT7_API_KEY' \
    "context7 block has expected env after profile overwrite"
  if echo "$context7_block" | grep -Eq '^[[:space:]]*headers[[:space:]]*='; then
    fail "context7 block still has 'headers' after wizard profile overwrite"
  else
    pass "context7 block clean after profile overwrite: no headers"
  fi

  if echo "$context7_block" | grep -Eq '^[[:space:]]*url[[:space:]]*='; then
    fail "context7 block still has any url after wizard profile overwrite"
  else
    pass "context7 block clean after profile overwrite: no url"
  fi

  # Verify github block (HTTP) no longer has stdio-only fields.
  local github_block
  github_block=$(sed -n '/^id = "github"/,/^\[\[mcp/p' "$repo_dir/.agent-layer/config.toml" | head -20)

  assert_output_contains "$github_block" 'transport = "http"' \
    "github block keeps HTTP transport after profile overwrite"
  assert_output_contains "$github_block" 'http_transport = "streamable"' \
    "github block keeps streamable HTTP transport after profile overwrite"
  assert_output_contains "$github_block" 'url = "https://api.githubcopilot.com/mcp/"' \
    "github block has expected URL after profile overwrite"
  assert_output_contains "$github_block" 'Authorization' \
    "github block has expected Authorization header after profile overwrite"
  if echo "$github_block" | grep -Eq '^[[:space:]]*command[[:space:]]*='; then
    fail "github block still has any command after wizard profile overwrite"
  else
    pass "github block clean after profile overwrite: no command"
  fi

  if echo "$github_block" | grep -Eq '^[[:space:]]*args[[:space:]]*='; then
    fail "github block still has any args after wizard profile overwrite"
  else
    pass "github block clean after profile overwrite: no stdio args"
  fi

  install_mock_claude "$repo_dir"

  assert_exit_zero_in "$repo_dir" "al claude after upgrade + wizard" al claude

  assert_claude_mock_called "$MOCK_CLAUDE_LOG"

  cleanup_scenario_dir "$repo_dir"
}
