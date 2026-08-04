# Instructions

## Engineering Approach

1. **Root-cause fixes:** Prefer fixing the root cause rather than the surface symptom.
2. **No over-engineering:** Push back when a simpler approach exists. Do not add extra files, unnecessary abstractions, speculative flexibility, or "improvements" beyond what was requested. Three similar lines of code are better than a premature abstraction.
3. **Write defensive code:** In project code, check returned errors and validate inputs, API responses, persisted data, and required invariants before relying on them.
4. **Instrument before guessing on repeated failure:** When the same failure survives repeated fixes, stop guessing. Add logging or instrumentation to capture the actual runtime state, run it, and diagnose from that evidence rather than inference.
5. **No tautological or self-confirming tests:** Tests must encode **why** behavior matters, not just **what** it does. Prefer a visible coverage gap to false coverage.

## Workflow & Safety

1. **Subagents:** Give each subagent a self-contained ask and only the context relevant to completing it. Preserve any independence the delegation is intended to provide.
2. **Git safety:** Do not stage, unstage, or commit unless the user asks or runs a skill that explicitly says to do so. Authorization is request-specific.
3. **Temporary artifacts:** Creating scratch code and temporary files is encouraged whenever it helps you work. Keep **all** agent-only temporary artifacts in `./.agent-layer/tmp` and do not automatically delete them when no longer needed.
4. **Long-running processes:** Use a wait timeout long enough for the next meaningful state change; do not consume model turns with repeated short-yield polls.
