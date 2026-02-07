@echo off
setlocal EnableExtensions
set "PARENT_ROOT=%~dp0.."
cd /d "%PARENT_ROOT%"

where al >nul 2>&1
if %ERRORLEVEL% neq 0 (
  echo Error: 'al' command not found.
  echo Install Agent Layer and ensure 'al' is on your PATH.
  pause
  exit /b 1
)

where code >nul 2>&1
if %ERRORLEVEL% equ 0 (
  al vscode --no-sync
) else (
  echo Error: 'code' command not found.
  echo To install: Open VS Code, press Ctrl+Shift+P, type 'Shell Command: Install code command in PATH', and run it.
  pause
)
