#!/usr/bin/env bash
# Upgrade from old version with transport-incompatible MCP fields,
# wizard profile overwrite repairs them, al claude works.

run_scenario_upgrade_sanitize_claude() {
  section "Upgrade + wizard profile overwrite + al claude"

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

  # Verify MCP server blocks exist before injection (if they're missing,
  # the sed injections silently no-op and the "not contains" assertions
  # after wizard trivially pass — a false green).
  assert_file_contains "$config" 'id = "context7"' \
    "config has context7 block before injection"
  assert_file_contains "$config" 'id = "github"' \
    "config has github block before injection"

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

  assert_exit_zero_in "$repo_dir" "al upgrade from $E2E_OLDEST_VERSION" \
    al upgrade --yes --apply-managed-updates --apply-memory-updates --apply-deletions

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
  local sanitize_profile="$repo_dir/.agent-layer/.sanitize-profile.toml"
  cp "$E2E_DEFAULTS_TOML" "$sanitize_profile"
  cat >> "$sanitize_profile" <<'PROFILE_EOF'

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
url = "https://api.githubcopilot.com/mcp/"
headers = { Authorization = "Bearer ${AL_GITHUB_PERSONAL_ACCESS_TOKEN}" }
PROFILE_EOF

  # Run wizard with the scenario-local profile to overwrite the polluted blocks.
  assert_exit_zero_in "$repo_dir" "al wizard clean profile after upgrade" \
    al wizard --profile "$sanitize_profile" --yes

  # Verify blocks survived profile overwrite (they should be cleaned, not removed).
  assert_file_contains "$repo_dir/.agent-layer/config.toml" 'id = "context7"' \
    "context7 block still present after wizard profile overwrite"
  local github_block_present=0
  if grep -q 'id = "github"' "$repo_dir/.agent-layer/config.toml"; then
    github_block_present=1
    pass "github block still present after wizard profile overwrite"
  else
    pass "github block removed by wizard profile (disabled server)"
  fi

  # Verify context7 block (stdio) no longer has HTTP-only fields.
  # Extract just the context7 block to avoid false matches from other servers.
  local context7_block
  context7_block=$(sed -n '/^id = "context7"/,/^\[\[mcp/p' "$repo_dir/.agent-layer/config.toml" | head -20)

  if echo "$context7_block" | grep -q 'headers'; then
    fail "context7 block still has 'headers' after wizard profile overwrite"
  else
    pass "context7 block clean after profile overwrite: no headers"
  fi

  if echo "$context7_block" | grep -q 'url = "https://example.com'; then
    fail "context7 block still has transport-incompatible url after wizard profile overwrite"
  else
    pass "context7 block clean after profile overwrite: no incompatible url"
  fi

  # Verify github block (HTTP) no longer has stdio-only fields.
  if [[ "$github_block_present" -eq 1 ]]; then
    local github_block
    github_block=$(sed -n '/^id = "github"/,/^\[\[mcp/p' "$repo_dir/.agent-layer/config.toml" | head -20)

    if echo "$github_block" | grep -q '^command = "npx"'; then
      fail "github block still has 'command' after wizard profile overwrite"
    else
      pass "github block clean after profile overwrite: no command"
    fi

    if echo "$github_block" | grep -q '^args = \["-y"'; then
      fail "github block still has stdio 'args' after wizard profile overwrite"
    else
      pass "github block clean after profile overwrite: no stdio args"
    fi
  else
    pass "github stdio-only field checks skipped (block removed)"
  fi

  install_mock_claude "$repo_dir"

  assert_exit_zero_in "$repo_dir" "al claude after upgrade + wizard" al claude

  assert_claude_mock_called "$MOCK_CLAUDE_LOG"

  cleanup_scenario_dir "$repo_dir"
}
