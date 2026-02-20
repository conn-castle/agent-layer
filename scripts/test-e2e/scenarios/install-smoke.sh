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
  if [[ $rc -ne 0 ]]; then
    fail "copied binary --version exited with code $rc"
  fi
  assert_output_equals "$version_out" "$AL_E2E_VERSION" "copied binary version matches"

  # --- Installer install ---
  local install_prefix="$E2E_TMP_ROOT/installer-prefix-install"
  assert_exit_zero "al-install.sh runs successfully" \
    bash "$ROOT_DIR/al-install.sh" \
      --version "$AL_E2E_VERSION" \
      --prefix "$install_prefix" \
      --no-completions \
      --asset-root "$E2E_DIST_DIR"

  assert_file_exists "$install_prefix/bin/al" "installer created bin/al"

  if [[ -x "$install_prefix/bin/al" ]]; then
    pass "installed binary is executable"
  else
    fail "installed binary is not executable"
  fi

  local inst_version_out inst_rc=0
  inst_version_out="$(cd "$safe_cwd" && PATH="$install_prefix/bin:$PATH" al --version 2>&1)" || inst_rc=$?
  if [[ $inst_rc -ne 0 ]]; then
    fail "installer-installed binary --version exited with code $inst_rc"
  fi
  assert_output_equals "$inst_version_out" "$AL_E2E_VERSION" "installer-installed binary version matches"
}
