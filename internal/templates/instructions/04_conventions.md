# Project Conventions

These conventions are project-specific defaults. Edit, remove, or add entries
to match your project's tech stack and practices. This file is never
overwritten during upgrades; new conventions may be appended via `al upgrade`
migrations.

## Architecture
- **Frontend is presentation, not business rules:** The frontend must not contain business rules, authoritative computations, or metric derivations. Allowed: presentation formatting, input validation, view-state management, and sorting for display.

## Code Quality
- **Test coverage integrity:** Do not reduce the minimum allowed code coverage threshold to make tests pass. Write tests and fix the code instead.
- **Packages (latest compatible stable versions):** Determine package versions using the package manager and official tooling/docs, not memory. Prefer the latest stable compatible versions. Avoid unstable or pre-release versions. If the latest stable version introduces breaking changes, ask for confirmation and then do the compatibility work.
- **Strict typing and documentation:** Python code must use type hints. TypeScript/JavaScript must use strict types. Public functions and non-trivial internal functions must include docstrings describing arguments and return values.

## Data Safety
- **Schema safety:** Never modify the database schema via raw SQL or direct tool access. Always generate a proper migration file using the project's migration system, and ask the user to apply it.

## Time & Data
- **UTC-only internals:** Store, compute, and transport time in UTC; local time display is presentation-only.

## Environment
- **No system Python:** Never use system Python. Always prefer the project virtual environment Python, and if no virtual environment exists, ask the user if you should create one.
