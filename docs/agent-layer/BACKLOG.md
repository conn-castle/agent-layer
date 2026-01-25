# Backlog

Note: This is an agent-layer memory file. It is primarily for agent use.

## Features and tasks (not scheduled)

<!-- ENTRIES START -->

- Backlog 2026-01-25 8b9c2d1: Define migration strategy for renamed/deleted template files
    Priority: High. Area: lifecycle management
    Description: Define how to handle template files that are renamed or deleted in future versions so they do not remain as stale orphans in user repos.
    Acceptance criteria: A clear design/decision is documented for how to detect and clean up obsolete template files during upgrades.
    Notes: Currently, al init only adds/updates; it does not remove files that vanished from the binary.

- Backlog 2026-01-25 9e3f1a2: Improve CLI output readability and formatting
    Priority: Medium. Area: developer experience
    Description: Enhance the human readability of CLI outputs (wizard, init, doctor, etc.) by adding colors, improved formatting, and better use of whitespace.
    Acceptance criteria: CLI commands consistently use semantic coloring and spacing to make reports and interactive prompts easier to interpret.
    Notes: Focus on making high-impact messages (errors, warnings, successes) visually distinct.
