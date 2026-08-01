# Project Conventions

- **Packages (latest compatible stable versions):** Determine package versions using the package manager and official tooling/docs, not memory. Prefer the latest stable compatible versions. Avoid unstable or pre-release versions.

- **UTC-only internals:** Store, compute, and transport time in UTC; local time display is presentation-only.

- **No system Python:** Never use system Python. Always prefer the project virtual environment Python, and if no virtual environment exists, ask the user if you should create one.
