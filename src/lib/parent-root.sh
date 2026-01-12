#!/usr/bin/env bash

# Spec-compliant parent root resolution for agent-layer.
# Root Selection Specification: README.md (Parent Root Resolution)
# Precedence (first match wins):
#   1) --parent-root flag
#   2) --temp-parent-root flag
#   3) PARENT_ROOT from .env
#   4) discovery (only when basename is .agent-layer)
#   5) error (dev repo or renamed dir)

ROOTS_HELPER_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
TEMP_PARENT_ROOT_HELPER="$ROOTS_HELPER_DIR/temp-parent-root.sh"

roots_die() {
  local msg="$1"
  printf "%s\n" "$msg" >&2
  return 2
}

trim_whitespace() {
  local value="$1"
  value="${value#"${value%%[![:space:]]*}"}"
  value="${value%"${value##*[![:space:]]}"}"
  printf "%s" "$value"
}

strip_quotes() {
  local value="$1"
  local first="${value:0:1}"
  local last="${value: -1}"

  if [[ "$first" == "\"" || "$first" == "'" ]]; then
    if [[ ${#value} -lt 2 || "$last" != "$first" ]]; then
      return 1
    fi
    value="${value:1:${#value}-2}"
  fi

  printf "%s" "$value"
}

# Resolve AGENT_LAYER_ROOT from script location.
# Args: none (uses ROOTS_HELPER_DIR; ROOTS_AGENT_LAYER_ROOT_OVERRIDE for tests only)
# Returns: 0 on success with AGENT_LAYER_ROOT exported; 2 on error.
resolve_agent_layer_root() {
  local override="${ROOTS_AGENT_LAYER_ROOT_OVERRIDE:-}"

  if [[ -n "$override" ]]; then
    if [[ ! -d "$override" ]]; then
      roots_die "ERROR: Agent layer root override does not exist: $override"
      return 2
    fi
    AGENT_LAYER_ROOT="$(cd "$override" && pwd -P)"
  else
    AGENT_LAYER_ROOT="$(cd "$ROOTS_HELPER_DIR/../.." && pwd -P)"
  fi

  export AGENT_LAYER_ROOT
  return 0
}

resolve_path_from_base() {
  local base="$1"
  local path="$2"

  if [[ "$path" == /* ]]; then
    printf "%s" "$path"
    return 0
  fi

  if [[ "$base" == "/" ]]; then
    printf "/%s" "$path"
    return 0
  fi

  printf "%s/%s" "${base%/}" "$path"
}

# Read PARENT_ROOT from a .env file without sourcing it.
# Args: env_file path
# Returns: 0 (prints value), 1 (not found), 2 (invalid entry)
read_parent_root_env() {
  local env_file="$1"
  local line value found="0"

  while IFS= read -r line || [[ -n "$line" ]]; do
    case "$line" in
      "" | [[:space:]]\#* | \#*)
        continue
        ;;
    esac

    if [[ "$line" =~ ^[[:space:]]*PARENT_ROOT ]]; then
      if [[ ! "$line" =~ ^[[:space:]]*PARENT_ROOT= ]]; then
        roots_die "$(
          cat << EOF
ERROR: Invalid PARENT_ROOT entry in $env_file

Line: $line

Fix:
  - Use PARENT_ROOT=<path>
  - Use simple KEY=value pairs (no spaces around '=')
EOF
        )"
        return 2
      fi
    fi

    if [[ "$line" =~ ^[[:space:]]*PARENT_ROOT= ]]; then
      if [[ "$found" == "1" ]]; then
        roots_die "$(
          cat << EOF
ERROR: Multiple PARENT_ROOT entries found in $env_file

Fix:
  - Keep only one PARENT_ROOT entry
EOF
        )"
        return 2
      fi

      value="${line#*=}"
      value="$(trim_whitespace "$value")"
      if ! value="$(strip_quotes "$value")"; then
        roots_die "$(
          cat << EOF
ERROR: Invalid PARENT_ROOT entry in $env_file

Line: $line

Fix:
  - Remove unmatched quotes
  - Use simple KEY=value pairs
EOF
        )"
        return 2
      fi

      if [[ -z "$value" ]]; then
        roots_die "$(
          cat << EOF
ERROR: Invalid PARENT_ROOT entry in $env_file

Line: $line

Fix:
  - Use PARENT_ROOT=<path>
EOF
        )"
        return 2
      fi

      found="1"
    fi
  done < "$env_file"

  if [[ "$found" == "1" ]]; then
    printf "%s" "$value"
    return 0
  fi

  return 1
}

message_parent_root_missing() {
  local path="$1" source="$2"
  cat << EOF
ERROR: Parent root path does not exist: $path

Source: $source

Fix:
  - Create the directory, or
  - Use a different path, or
  - Use temp parent root for testing: --temp-parent-root
EOF
}

message_parent_root_missing_agent_layer() {
  local path="$1"
  cat << EOF
ERROR: Parent root must contain .agent-layer/ (dir or symlink): $path

Found directory but no .agent-layer/ inside.

Fix:
  - Install agent-layer in that directory
  - Use a different path that contains .agent-layer/
  - Use temp parent root for testing: --temp-parent-root
EOF
}

message_parent_root_consistency() {
  local agent_layer_real="$1"
  local parent_agent_layer_real="$2"
  local parent_root="$3"
  cat << EOF
ERROR: Parent root .agent-layer/ does not match script location.

Resolved script location: $agent_layer_real
Resolved parent config:   $parent_agent_layer_real

These must point to the same location. You are running scripts from one
agent-layer installation but trying to configure a different one.

Fix:
  - Use scripts from ${parent_root}/.agent-layer/
  - Or adjust --parent-root to match script location
EOF
}

message_agent_layer_root_is_root() {
  cat << 'EOF'
ERROR: Agent layer root is the filesystem root (/).

This is invalid. The agent layer root must be a directory (e.g., .agent-layer)
inside a parent repo.

Fix:
  - Reinstall agent-layer in a valid subdirectory
EOF
}

message_conflicting_flags() {
  cat << 'EOF'
ERROR: Conflicting flags: --parent-root and --temp-parent-root

You provided both flags but they are mutually exclusive.
Choose one:
  - Use --parent-root <path> for explicit parent root
  - Use --temp-parent-root to create temporary parent root
EOF
}

message_temp_parent_root_failed() {
  local agent_layer_root="$1"
  cat << EOF
ERROR: Failed to create temporary parent root directory.

Attempted:
  1. ${TMPDIR:-/tmp}/agent-layer-temp-parent-root.XXXXXX
  2. ${agent_layer_root}/tmp/agent-layer-temp-parent-root.XXXXXX
  3. Manual creation (if mktemp unavailable)

Possible causes:
  - Disk full (check: df -h)
  - No write permission to temp directories
  - \$TMPDIR points to non-existent location

Fix:
  - Free disk space
  - Set TMPDIR to writable location: export TMPDIR=/writable/path
  - Use explicit parent root instead: --parent-root <path>
EOF
}

message_temp_parent_root_symlink_failed() {
  local temp_dir="$1"
  local agent_layer_root="$2"
  cat << EOF
ERROR: Failed to create .agent-layer symlink in temp parent root.

Path: ${temp_dir}/.agent-layer -> ${agent_layer_root}

Possible causes:
  - Filesystem doesn't support symlinks (e.g., FAT32, some network mounts)
  - Path already exists at ${temp_dir}/.agent-layer
  - Permission denied

Fix:
  - Use filesystem that supports symlinks (ext4, APFS, HFS+)
  - Or use explicit parent root: --parent-root <path>
EOF
}

message_dev_repo_requires_parent_root() {
  local agent_layer_root="$1"
  cat << EOF
ERROR: Running from agent-layer repo requires explicit parent root configuration.

Context: Agent-layer development repo

The agent-layer repo cannot auto-discover a parent root because it doesn't have
".agent-layer" as its directory name. You must explicitly specify how to set up
the test environment.

Options (choose one):
  1. Use temporary parent root (recommended for testing/CI):
     ./setup.sh --temp-parent-root
     ./tests/run.sh --temp-parent-root

  2. Specify explicit parent root (if you have a test consumer repo):
     # NOTE: The test repo must have a symlink .agent-layer -> <this-repo>
     ./setup.sh --parent-root /path/to/test-repo
     ./tests/run.sh --parent-root /path/to/test-repo

  3. Set PARENT_ROOT in ${agent_layer_root}/.env for persistent config:
     echo "PARENT_ROOT=/path/to/test-repo" > .env
EOF
}

message_renamed_agent_layer_dir() {
  local agent_layer_root="$1"
  local name="$2"
  cat << EOF
ERROR: Cannot discover parent root - agent layer directory name is not ".agent-layer"

Current name: ${name}
Expected: .agent-layer

Discovery is only allowed when the agent layer root is named ".agent-layer".
If you renamed it, discovery will not work.

Options:
  1. Rename directory to .agent-layer (if this is an installed agent layer)
  2. Use explicit parent root: --parent-root <path>
  3. Use temp parent root: --temp-parent-root
  4. Set PARENT_ROOT in ${agent_layer_root}/.env
EOF
}

# Scenario 2: explicit parent root via CLI or .env.
# Args: input_path, base_dir (resolve relative paths), source_label
# Returns: 0 on success; 2 on validation/consistency errors.
resolve_explicit_parent_root() {
  local input_path="$1"
  local base_dir="$2"
  local source_label="$3"

  local resolved_path
  resolved_path="$(resolve_path_from_base "$base_dir" "$input_path")"

  if [[ ! -d "$resolved_path" ]]; then
    roots_die "$(message_parent_root_missing "$resolved_path" "$source_label")"
    return 2
  fi

  local parent_root_real
  parent_root_real="$(cd "$resolved_path" && pwd -P)"

  if [[ ! -d "$parent_root_real/.agent-layer" && ! -L "$parent_root_real/.agent-layer" ]]; then
    roots_die "$(message_parent_root_missing_agent_layer "$parent_root_real")"
    return 2
  fi

  local agent_layer_real
  agent_layer_real="$(cd "$AGENT_LAYER_ROOT" && pwd -P)"

  local configured_agent_layer_real
  configured_agent_layer_real="$(cd "$parent_root_real/.agent-layer" && pwd -P)"

  if [[ "$configured_agent_layer_real" != "$agent_layer_real" ]]; then
    roots_die "$(message_parent_root_consistency "$agent_layer_real" "$configured_agent_layer_real" "$parent_root_real")"
    return 2
  fi

  PARENT_ROOT="$parent_root_real"
  TEMP_PARENT_ROOT_CREATED="0"
  export PARENT_ROOT TEMP_PARENT_ROOT_CREATED
  return 0
}

# Scenario 3: create a temporary parent root (always allowed).
# Returns: 0 on success; 2 on temp dir or symlink errors.
resolve_temp_parent_root() {
  if [[ -f "$TEMP_PARENT_ROOT_HELPER" ]]; then
    # shellcheck disable=SC1090
    source "$TEMP_PARENT_ROOT_HELPER"
  fi

  if ! declare -F make_temp_parent_root > /dev/null 2>&1; then
    roots_die "ERROR: Missing src/lib/temp-parent-root.sh (expected in the agent-layer root)."
    return 2
  fi

  local temp_dir
  TEMP_PARENT_ROOT_FAILED_DIR=""
  TEMP_PARENT_ROOT_RESULT=""
  make_temp_parent_root "$AGENT_LAYER_ROOT" > /dev/null
  local status=$?
  temp_dir="$TEMP_PARENT_ROOT_RESULT"
  if [[ $status -ne 0 || -z "$temp_dir" || ! -d "$temp_dir" ]]; then
    if [[ $status -eq 3 ]]; then
      roots_die "$(message_temp_parent_root_symlink_failed "$TEMP_PARENT_ROOT_FAILED_DIR" "$AGENT_LAYER_ROOT")"
      return 2
    fi
    roots_die "$(message_temp_parent_root_failed "$AGENT_LAYER_ROOT")"
    return 2
  fi

  temp_dir="$(cd "$temp_dir" && pwd -P)"
  PARENT_ROOT="$temp_dir"
  TEMP_PARENT_ROOT_CREATED="1"
  export PARENT_ROOT TEMP_PARENT_ROOT_CREATED
  return 0
}

# Scenario 1: discovery when AGENT_LAYER_ROOT is named .agent-layer.
# Returns: 0 on success; 2 on consistency mismatch.
resolve_discovered_parent_root() {
  local parent_root
  parent_root="$(cd "$AGENT_LAYER_ROOT/.." && pwd -P)"

  local agent_layer_real
  agent_layer_real="$(cd "$AGENT_LAYER_ROOT" && pwd -P)"

  local configured_agent_layer_real
  configured_agent_layer_real="$(cd "$parent_root/.agent-layer" && pwd -P)"

  if [[ "$configured_agent_layer_real" != "$agent_layer_real" ]]; then
    roots_die "$(message_parent_root_consistency "$agent_layer_real" "$configured_agent_layer_real" "$parent_root")"
    return 2
  fi

  PARENT_ROOT="$parent_root"
  TEMP_PARENT_ROOT_CREATED="0"
  export PARENT_ROOT TEMP_PARENT_ROOT_CREATED
  return 0
}

# Resolve PARENT_ROOT according to the Root Selection Specification.
# Returns: 0 on success; 2 on any spec-defined error.
resolve_parent_root() {
  TEMP_PARENT_ROOT_CREATED="0"
  export TEMP_PARENT_ROOT_CREATED

  resolve_agent_layer_root || return 2

  if [[ "$AGENT_LAYER_ROOT" == "/" ]]; then
    roots_die "$(message_agent_layer_root_is_root)"
    return 2
  fi

  local basename
  basename="$(basename "$AGENT_LAYER_ROOT")"

  if [[ "$basename" == ".agent-layer" ]]; then
    IS_CONSUMER_LAYOUT="1"
  else
    IS_CONSUMER_LAYOUT="0"
  fi
  export IS_CONSUMER_LAYOUT

  local parent_root_cli="${ROOTS_PARENT_ROOT:-}"
  local use_temp="${ROOTS_USE_TEMP_PARENT_ROOT:-0}"

  if [[ -n "$parent_root_cli" && "$use_temp" == "1" ]]; then
    roots_die "$(message_conflicting_flags)"
    return 2
  fi

  local env_parent_root=""
  local env_file="$AGENT_LAYER_ROOT/.env"
  if [[ -f "$env_file" ]]; then
    env_parent_root="$(read_parent_root_env "$env_file")" || {
      local status=$?
      if [[ $status -eq 2 ]]; then
        return 2
      fi
      env_parent_root=""
    }
  fi

  if [[ -n "$parent_root_cli" ]]; then
    resolve_explicit_parent_root "$parent_root_cli" "$(pwd -P)" "--parent-root flag" || return 2
    export AGENT_LAYER_ROOT
    return 0
  fi

  if [[ "$use_temp" == "1" ]]; then
    resolve_temp_parent_root || return 2
    export AGENT_LAYER_ROOT
    return 0
  fi

  if [[ -n "$env_parent_root" ]]; then
    resolve_explicit_parent_root "$env_parent_root" "$AGENT_LAYER_ROOT" "PARENT_ROOT in .env" || return 2
    export AGENT_LAYER_ROOT
    return 0
  fi

  if [[ "$basename" == ".agent-layer" ]]; then
    resolve_discovered_parent_root || return 2
    export AGENT_LAYER_ROOT
    return 0
  fi

  if [[ "$basename" == "agent-layer" ]]; then
    roots_die "$(message_dev_repo_requires_parent_root "$AGENT_LAYER_ROOT")"
    return 2
  fi

  roots_die "$(message_renamed_agent_layer_dir "$AGENT_LAYER_ROOT" "$basename")"
  return 2
}
