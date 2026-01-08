#!/usr/bin/env bats

load "helpers.bash"

@test "with-env.sh loads project .env only with --project-env" {
  local root output
  root="$(create_working_root)"

  printf "TEST_PROJECT_ENV=from-project\n" >"$root/.env"

  output="$(cd "$root/sub/dir" && "$root/.agentlayer/with-env.sh" --project-env \
    bash -c 'echo "${TEST_PROJECT_ENV:-}"')"
  status=$?
  [ "$status" -eq 0 ]
  [ "$output" = "from-project" ]

  output="$(cd "$root/sub/dir" && "$root/.agentlayer/with-env.sh" \
    bash -c 'echo "${TEST_PROJECT_ENV:-}"')"
  status=$?
  [ "$status" -eq 0 ]
  [ -z "$output" ]

  rm -rf "$root"
}
