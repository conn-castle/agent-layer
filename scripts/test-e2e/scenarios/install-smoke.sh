#!/usr/bin/env bash
# Install smoke tests: copy binary to prefix, installer script, version checks.

run_scenario_install_smoke() {
  section "Install smoke"

  local safe_cwd="$E2E_TMP_ROOT/safe-cwd-install"
  mkdir -p "$safe_cwd"

  # --- Copy install ---
  local copy_prefix="$E2E_TMP_ROOT/copy-prefix-install"
  mkdir -p "$copy_prefix/bin"
  cp "$E2E_BIN" "$copy_prefix/bin/al"
  chmod +x "$copy_prefix/bin/al"

  local version_out rc=0
  version_out="$(cd "$safe_cwd" && PATH="$copy_prefix/bin:$PATH" al --version 2>&1)" || rc=$?
  [[ $rc -eq 0 ]] || fail "copied binary --version exited with code $rc"
  assert_output_equals "$version_out" "$AL_E2E_VERSION" "copied binary version matches"

  # --- Installer install ---
  local install_prefix="$E2E_TMP_ROOT/installer-prefix-install"
  local installer=(bash "$ROOT_DIR/al-install.sh" --prefix "$install_prefix" --version "$AL_E2E_VERSION" --asset-root "$E2E_DIST_DIR")
  mkdir -p "$install_prefix/bin"
  ln -s "$safe_cwd" "$install_prefix/bin/al"
  assert_exit_nonzero "installer refuses a symlinked directory" "${installer[@]}" --no-completions
  rm "$install_prefix/bin/al"
  ln -s "$copy_prefix/bin/al" "$install_prefix/bin/al"
  assert_exit_nonzero "installer rejects an unsupported shell" "${installer[@]}" --shell tcsh
  assert_output_equals "$(readlink "$install_prefix/bin/al")" "$copy_prefix/bin/al" "installer preserved the regular-file symlink after shell validation failed"
  assert_exit_zero "al-install.sh runs successfully" "${installer[@]}" --no-completions --shell tcsh

  assert_exit_zero "installer replaced the regular-file symlink" test ! -L "$install_prefix/bin/al"

  assert_exit_zero "installed binary is executable" test -x "$install_prefix/bin/al"

  local inst_version_out inst_rc=0
  inst_version_out="$(cd "$safe_cwd" && PATH="$install_prefix/bin:$PATH" al --version 2>&1)" || inst_rc=$?
  [[ $inst_rc -eq 0 ]] || fail "installer-installed binary --version exited with code $inst_rc"
  assert_output_equals "$inst_version_out" "$AL_E2E_VERSION" "installer-installed binary version matches"
  local race_bin="$E2E_TMP_ROOT/installer-race-bin"
  mkdir -p "$race_bin"
  cat > "$race_bin/mv" <<'MV'
#!/usr/bin/env bash
set -euo pipefail
if [[ "$1" == */.al.*/al ]]; then
  "$REAL_MV" "$RACE_TARGET" "$RACE_TARGET.old"
  mkdir "$RACE_TARGET"
  printf 'user data\n' > "$RACE_TARGET/sentinel"
fi
exec "$REAL_MV" "$@"
MV
  chmod +x "$race_bin/mv"
  assert_exit_nonzero "installer refuses a directory introduced before publication" env REAL_MV="$(command -v mv)" RACE_TARGET="$install_prefix/bin/al" PATH="$race_bin:$PATH" "${installer[@]}" --no-completions
  assert_output_equals "$(cat "$install_prefix/bin/al/sentinel")" "user data" "installer preserved conflicting directory data"
  assert_output_equals "$(ls -A "$install_prefix/bin/al")" "sentinel" "installer left no binary inside the conflicting directory"
  assert_exit_zero "installer preserved the previous binary" cmp "$install_prefix/bin/al.old" "$E2E_BIN"
  assert_output_equals "$(ls -A "$install_prefix/bin")" $'al\nal.old' "installer cleaned its staging paths"
}
