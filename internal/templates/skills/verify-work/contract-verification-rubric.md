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

Reuse prior evidence only when its command, result, covered state, and relevance
remain known; otherwise run the narrowest credible check.

Try to falsify material completion claims where contract or risk supplies a
credible failure path. Do not expand into general review or low-signal cases.

For each finding, capture the contract item, status (`complete`, `partial`,
`missing`, `unverified`, `undocumented_deviation`, or `scope_drift`), evidence,
severity, and smallest correction. The skill defines presentation.

Do not infer intent. If the contract promises X and the diff delivers Y, record
`undocumented_deviation` even when Y appears reasonable.
