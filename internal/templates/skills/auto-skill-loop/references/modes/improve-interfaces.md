# Improve Interfaces

This is one iteration of a repeated loop whose purpose is to improve interfaces,
one PR at a time. Require an existing interface-audit report path as input; do
not run a fresh audit. Select the highest-value coherent improvement that fits
in one PR and can be implemented without a human decision. After implementation
succeeds, use a fresh subagent to run
`/interface-audit --update <report-path>` before returning. Stop when the current
report shows diminishing returns.
