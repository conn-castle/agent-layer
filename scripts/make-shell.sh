#!/usr/bin/env bash
# Recipe shell for the Makefile. GNU Make 3.81 (macOS /usr/bin/make) ignores
# .SHELLFLAGS, so strict mode is applied here for every Make version.
exec bash -euo pipefail "$@"
