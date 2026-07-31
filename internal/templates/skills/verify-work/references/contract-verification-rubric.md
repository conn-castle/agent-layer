# Contract Verification Rubric

Check whether completed work delivers the agreed contract. Use only:

1. Fresh plan/task/context files or the explicit request as the contract.
2. The current diff and touched files in their final state.
3. Observed command or inspection evidence covering that state.
4. Caller-supplied supplemental obligations as additive targets only.

Compare point-by-point:

- in-scope implementation and exit criteria
- promised tests, docs, and memory updates
- working-code evidence for touched behavior
- diff content not justified by the contract
- undocumented differences from the contract
- supplemental obligations, reported separately

Do not expand into general review or low-signal cases.

For each finding, capture the contract item, current status (`complete`,
`partial`, `missing`, `unverified`, `undocumented_deviation`, or `scope_drift`),
evidence, and severity.
