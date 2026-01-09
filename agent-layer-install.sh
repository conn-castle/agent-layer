#!/usr/bin/env bash
set -euo pipefail

say() { printf "%s\n" "$*"; }
die() { printf "ERROR: %s\n" "$*" >&2; exit 1; }

usage() {
  cat <<'EOF'
Usage: agent-layer-install.sh [--force] [--repo-url <url>]

Installs/updates agent-layer in the current working repo and sets up a local launcher.

Options:
  --force, -f       Overwrite ./al if it already exists
  --repo-url <url>  Override the agent-layer repo URL
  --help, -h        Show this help
EOF
}

FORCE="0"
REPO_URL_DEFAULT="https://github.com/nicholasjconn/agent-layer.git"
REPO_URL="${AGENTLAYER_REPO_URL:-$REPO_URL_DEFAULT}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --force|-f)
      FORCE="1"
      ;;
    --repo-url)
      [[ $# -ge 2 ]] || die "--repo-url requires a value"
      REPO_URL="$2"
      shift
      ;;
    --repo-url=*)
      REPO_URL="${1#*=}"
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    *)
      die "Unknown argument: $1"
      ;;
  esac
  shift
done

command -v git >/dev/null 2>&1 || die "git is required (not found)."

WORKING_ROOT="$(pwd -P)"
if [[ "$(basename "$WORKING_ROOT")" == ".agent-layer" ]]; then
  die "Run this from the working repo root (parent of .agent-layer/), not inside .agent-layer/."
fi

if git rev-parse --show-toplevel >/dev/null 2>&1; then
  GIT_ROOT="$(git rev-parse --show-toplevel)"
  if [[ "$GIT_ROOT" != "$WORKING_ROOT" ]]; then
    die "Run this from the repo root: $GIT_ROOT"
  fi
else
  say "WARNING: This directory does not appear to be a git repo."
  say "If you meant a different folder, stop now."
  say "You can continue, but hooks won't be enabled until you init git."
  if [[ -t 0 ]]; then
    read -r -p "Continue anyway? [y/N] " reply
    case "$reply" in
      y|Y|yes|YES)
        ;;
      *)
        die "Aborted."
        ;;
    esac
  else
    die "Not a git repo and no TTY available to confirm. Re-run from a TTY or after init."
  fi
fi

AGENTLAYER_DIR="$WORKING_ROOT/.agent-layer"
if [[ ! -e "$AGENTLAYER_DIR" ]]; then
  [[ -n "$REPO_URL" ]] || die "Missing repo URL (set AGENTLAYER_REPO_URL or use --repo-url)."
  say "==> Cloning agent-layer into .agent-layer/"
  git clone "$REPO_URL" "$AGENTLAYER_DIR"
else
  if [[ -d "$AGENTLAYER_DIR" ]]; then
    if git -C "$AGENTLAYER_DIR" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
      say "==> .agent-layer exists and is a git repo; leaving as-is"
    else
      die ".agent-layer exists but is not a git repo. Move it aside or remove it, then re-run."
    fi
  else
    die ".agent-layer exists but is not a directory. Move it aside or remove it, then re-run."
  fi
fi

if [[ ! -f "$AGENTLAYER_DIR/.env" ]]; then
  if [[ -f "$AGENTLAYER_DIR/.env.example" ]]; then
    cp "$AGENTLAYER_DIR/.env.example" "$AGENTLAYER_DIR/.env"
    say "==> Created .agent-layer/.env from .env.example"
  else
    die "Missing .agent-layer/.env.example; cannot create .agent-layer/.env"
  fi
else
  say "==> .agent-layer/.env already exists; leaving as-is"
fi

AL_PATH="$WORKING_ROOT/al"
write_launcher() {
  cat > "$AL_PATH" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
exec "$SCRIPT_DIR/.agent-layer/al" "$@"
EOF
  chmod +x "$AL_PATH"
}

if [[ -e "$AL_PATH" ]]; then
  if [[ "$FORCE" == "1" ]]; then
    say "==> Overwriting ./al"
    write_launcher
  else
    say "==> ./al exists; leaving as-is (use --force to overwrite)"
  fi
else
  say "==> Creating ./al"
  write_launcher
fi

GITIGNORE_PATH="$WORKING_ROOT/.gitignore"
GITIGNORE_BLOCK="$(cat <<'EOF'
# >>> agent-layer
.agent-layer/

# Agent Layer launcher
al

# Agent Layer-generated instruction shims
AGENTS.md
CLAUDE.md
GEMINI.md
.github/copilot-instructions.md

# Agent Layer-generated client configs + artifacts
.mcp.json
.codex/
.gemini/
.claude/
.vscode/mcp.json
# <<< agent-layer
EOF
)"

update_gitignore() {
  local tmp last_char last_line found inblock line
  tmp="$(mktemp)" || die "Failed to create temp file."
  found="0"
  inblock="0"
  if [[ -f "$GITIGNORE_PATH" ]]; then
    while IFS= read -r line || [[ -n "$line" ]]; do
      if [[ "$line" == "# >>> agent-layer" ]]; then
        if [[ "$found" == "0" ]]; then
          printf "%s\n" "$GITIGNORE_BLOCK" >> "$tmp"
          found="1"
        fi
        inblock="1"
        continue
      fi
      if [[ "$inblock" == "1" ]]; then
        if [[ "$line" == "# <<< agent-layer" ]]; then
          inblock="0"
        fi
        continue
      fi
      printf "%s\n" "$line" >> "$tmp"
    done < "$GITIGNORE_PATH"
  else
    : > "$tmp"
  fi

  if [[ "$found" == "0" ]]; then
    if [[ -s "$tmp" ]]; then
      last_char="$(tail -c 1 "$tmp" || true)"
      if [[ "$last_char" != $'\n' ]]; then
        printf '\n' >> "$tmp"
      fi
      last_line="$(tail -n 1 "$tmp" || true)"
      if [[ -n "$last_line" ]]; then
        printf '\n' >> "$tmp"
      fi
    fi
    printf "%s\n" "$GITIGNORE_BLOCK" >> "$tmp"
  fi

  mv "$tmp" "$GITIGNORE_PATH"
}

say "==> Updating .gitignore (agent-layer block)"
update_gitignore

if [[ -f "$AGENTLAYER_DIR/setup.sh" ]]; then
  say "==> Running setup"
  bash "$AGENTLAYER_DIR/setup.sh"
else
  die "Missing .agent-layer/setup.sh"
fi

say "==> Running sync"
if [[ -f "$AGENTLAYER_DIR/sync.mjs" ]]; then
  node "$AGENTLAYER_DIR/sync.mjs"
elif [[ -f "$AGENTLAYER_DIR/sync/sync.mjs" ]]; then
  node "$AGENTLAYER_DIR/sync/sync.mjs"
else
  die "Missing sync script (.agent-layer/sync.mjs or .agent-layer/sync/sync.mjs)."
fi

say ""
say "After completing the required manual steps above, run one of:"
say "  ./al gemini"
say "  ./al claude"
say "  ./al codex"
