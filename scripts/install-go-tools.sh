#!/usr/bin/env bash
# Install pinned Go tools into TOOL_BIN using exact version stamps so checkout
# mtime resets cannot trigger redundant rebuilds on cache hits and partial
# caches cannot serve stale tool versions.
set -euo pipefail

if [[ $# -lt 5 ]]; then
  echo "usage: $0 <tool-bin> <go-cache> <go-mod-cache> <golangci-lint-version> <tool> [tool ...]" >&2
  exit 2
fi

tool_bin="$1"
go_cache="$2"
go_mod_cache="$3"
golangci_lint_version="$4"
shift 4

mkdir -p "$tool_bin" "$go_cache" "$go_mod_cache"

go_version="$(go env GOVERSION 2>/dev/null || go version)"

resolve_spec() {
  local tool="$1"
  local mod_version
  case "$tool" in
    goimports)
      mod_version="$(go list -m -f '{{.Version}}' golang.org/x/tools)"
      [[ -n "$mod_version" ]] || { echo "Failed to resolve golang.org/x/tools version from go.mod" >&2; return 1; }
      printf '%s|%s\n' "golang.org/x/tools/cmd/goimports@${mod_version}" "${go_version}|${mod_version}"
      ;;
    deadcode)
      mod_version="$(go list -m -f '{{.Version}}' golang.org/x/tools)"
      [[ -n "$mod_version" ]] || { echo "Failed to resolve golang.org/x/tools version from go.mod" >&2; return 1; }
      printf '%s|%s\n' "golang.org/x/tools/cmd/deadcode@${mod_version}" "${go_version}|${mod_version}"
      ;;
    gotestsum)
      mod_version="$(go list -m -f '{{.Version}}' gotest.tools/gotestsum)"
      [[ -n "$mod_version" ]] || { echo "Failed to resolve gotestsum version from go.mod" >&2; return 1; }
      printf '%s|%s\n' "gotest.tools/gotestsum@${mod_version}" "${go_version}|${mod_version}"
      ;;
    golangci-lint)
      [[ -n "$golangci_lint_version" ]] || { echo "Missing golangci-lint version" >&2; return 1; }
      printf '%s|%s\n' "github.com/golangci/golangci-lint/v2/cmd/golangci-lint@${golangci_lint_version}" "${go_version}|${golangci_lint_version}"
      ;;
    govulncheck)
      mod_version="$(go list -m -f '{{.Version}}' golang.org/x/vuln)"
      [[ -n "$mod_version" ]] || { echo "Failed to resolve golang.org/x/vuln version from go.mod" >&2; return 1; }
      printf '%s|%s\n' "golang.org/x/vuln/cmd/govulncheck@${mod_version}" "${go_version}|${mod_version}"
      ;;
    *)
      echo "Unknown pinned Go tool: $tool" >&2
      return 1
      ;;
  esac
}

pids=()
tools_installing=()

for tool in "$@"; do
  spec="$(resolve_spec "$tool")"
  pkg_spec="${spec%%|*}"
  stamp_val="${spec#*|}"
  bin_path="${tool_bin}/${tool}"
  stamp_path="${tool_bin}/.${tool}.version"

  if [[ -x "$bin_path" && -f "$stamp_path" ]] && [[ "$(cat "$stamp_path")" == "$stamp_val" ]]; then
    continue
  fi

  (
    GOBIN="$tool_bin" GOCACHE="$go_cache" GOMODCACHE="$go_mod_cache" go install "$pkg_spec"
    tmp_stamp="${stamp_path}.tmp.$$"
    printf '%s\n' "$stamp_val" > "$tmp_stamp"
    mv -f "$tmp_stamp" "$stamp_path"
  ) &
  pids+=("$!")
  tools_installing+=("$tool")
done

failed=0
if [[ ${#pids[@]} -gt 0 ]]; then
  for i in "${!pids[@]}"; do
    if ! wait "${pids[$i]}"; then
      echo "ERROR: failed to install pinned tool ${tools_installing[$i]}" >&2
      failed=1
    fi
  done
fi

exit "$failed"
