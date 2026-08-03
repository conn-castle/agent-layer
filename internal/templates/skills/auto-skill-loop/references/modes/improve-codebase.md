# Improve Codebase

This is one iteration of a repeated loop whose purpose is to improve the
codebase, one PR at a time. Delegate discovery to a fresh subagent to assess
current evidence across the repository and return the highest-value coherent
improvement that fits in one PR and can be implemented without a human decision.
Use that selection for this iteration. Across iterations, improve the codebase
as a whole rather than repeatedly refining one area. Cosmetic changes and
speculative abstractions do not qualify. Preserve external behavior and
contracts unless a change is authorized. Stop only when current evidence shows
diminishing returns across the entire codebase.
