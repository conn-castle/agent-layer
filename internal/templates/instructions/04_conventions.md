# Project Conventions

- **Frontend is presentation, not business rules:** The frontend must not contain business rules, authoritative computations, or metric derivations. Allowed: presentation formatting, input validation, view-state management, and sorting for display.

- **Packages (latest compatible stable versions):** Determine package versions using the package manager and official tooling/docs, not memory. Prefer the latest stable compatible versions. Avoid unstable or pre-release versions.

- **Schema safety:** Never modify the database schema via raw SQL or direct tool access. Always generate a proper migration file using the project's migration system, and ask the user to apply it.

- **UTC-only internals:** Store, compute, and transport time in UTC; local time display is presentation-only.

- **No system Python:** Never use system Python. Always prefer the project virtual environment Python, and if no virtual environment exists, ask the user if you should create one.
