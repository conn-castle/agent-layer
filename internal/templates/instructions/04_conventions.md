# Project Conventions

- **Packages (latest compatible stable versions):** Determine package versions using the package manager and official tools or documentation, not memory. Prefer the latest stable compatible versions. Avoid unstable or pre-release versions.
- **UTC-only internals:** Store, compute, and transport time in UTC; local time display is presentation-only.
- **No system Python:** Never use system Python. Use the project virtual environment's Python. If no virtual environment exists, ask whether to create one.
