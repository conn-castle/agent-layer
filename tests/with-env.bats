#!/usr/bin/env bats

# Tests for environment loading via with-env.sh.
# Load shared helpers for temp roots and stub binaries.
load "helpers.bash"

# Test: with-env.sh loads project .env only with --project-env
@test "with-env.sh loads project .env only with --project-env" {
  local root output
  root="$(create_isolated_parent_root)"

  printf "TEST_PROJECT_ENV=from-project\n" >"$root/.env"

  output="$(cd "$root/sub/dir" && "$root/.agent-layer/with-env.sh" --project-env \
    bash -c 'echo "${TEST_PROJECT_ENV:-}"')"
  status=$?
  [ "$status" -eq 0 ]
  [ "$output" = "from-project" ]

  output="$(cd "$root/sub/dir" && "$root/.agent-layer/with-env.sh" \
    bash -c 'echo "${TEST_PROJECT_ENV:-}"')"
  status=$?
  [ "$status" -eq 0 ]
  [ -z "$output" ]

  rm -rf "$root"
}

# Test: with-env.sh loads .agent-layer .env by default
@test "with-env.sh loads .agent-layer .env by default" {
  local root output
  root="$(create_isolated_parent_root)"

  printf "TEST_AGENT_ENV=from-agent\n" >"$root/.agent-layer/.env"

  output="$(cd "$root/sub/dir" && "$root/.agent-layer/with-env.sh" \
    bash -c 'echo "${TEST_AGENT_ENV:-}"')"
  status=$?
  [ "$status" -eq 0 ]
  [ "$output" = "from-agent" ]

  rm -rf "$root"
}

# Test: with-env.sh is a no-op when .agent-layer .env is missing
@test "with-env.sh is a no-op when .agent-layer .env is missing" {
  local root output
  root="$(create_isolated_parent_root)"

  output="$(cd "$root/sub/dir" && "$root/.agent-layer/with-env.sh" \
    bash -c 'echo "${TEST_AGENT_ENV_MISSING:-}"')"
  status=$?
  [ "$status" -eq 0 ]
  [ -z "$output" ]

  rm -rf "$root"
}

# Test: with-env.sh --help prints usage
@test "with-env.sh --help prints usage" {
  local root
  root="$(create_isolated_parent_root)"

  run "$root/.agent-layer/with-env.sh" --help
  [ "$status" -eq 0 ]
  [[ "$output" == *"Usage:"* ]]

  rm -rf "$root"
}
