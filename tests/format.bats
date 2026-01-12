#!/usr/bin/env bats

# Tests for the formatting script behavior.
# Load shared helpers for temp roots and stub binaries.
load "helpers.bash"

# Test: format.sh runs shfmt and prettier
@test "format.sh runs shfmt and prettier" {
  local root stub_bin bash_bin shfmt_log prettier_log
  root="$(create_isolated_parent_root)"
  stub_bin="$root/stub-bin"
  bash_bin="$(command -v bash)"
  shfmt_log="$root/shfmt.log"
  prettier_log="$root/prettier.log"

  mkdir -p "$stub_bin"
  cat >"$stub_bin/shfmt" <<EOF
#!/usr/bin/env bash
printf "%s\n" "\$@" >> "$shfmt_log"
exit 0
EOF
  chmod +x "$stub_bin/shfmt"

  mkdir -p "$root/.agent-layer/node_modules/.bin"
  cat >"$root/.agent-layer/node_modules/.bin/prettier" <<EOF
#!/usr/bin/env bash
printf "%s\n" "\$@" >> "$prettier_log"
exit 0
EOF
  chmod +x "$root/.agent-layer/node_modules/.bin/prettier"

  printf "#!/usr/bin/env bash\necho format\n" >"$root/.agent-layer/sample.sh"

  run "$bash_bin" -c "cd '$root' && PATH='$stub_bin:/usr/bin:/bin' '$root/.agent-layer/dev/format.sh'"
  [ "$status" -eq 0 ]

  run rg -n -- "-w" "$shfmt_log"
  [ "$status" -eq 0 ]
  run rg -n -- "--write" "$prettier_log"
  [ "$status" -eq 0 ]

  rm -rf "$root"
}
