#!/usr/bin/env bash
set -e
PARENT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "$PARENT_ROOT"

if ! command -v al >/dev/null 2>&1; then
  echo "Error: 'al' command not found."
  echo "Install Agent Layer and ensure 'al' is on your PATH."
  exit 1
fi

if ! command -v code >/dev/null 2>&1; then
  echo "Error: 'code' command not found."
  echo "To install: Open VS Code, press Cmd+Shift+P, type 'Shell Command: Install code command in PATH', and run it."
  exit 1
fi

al vscode --no-sync
