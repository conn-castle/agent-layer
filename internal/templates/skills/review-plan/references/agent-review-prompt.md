# Plan Reviewer Prompt

Review the complete plan/task/context set. Do not edit artifacts.

## Review standard

Use 1-4 fresh built-in review subagents in parallel to review the artifacts
based on risk and complexity of the plan. When there is a consequential
architecture change, one of the subagents must provide an architectural review.
Each subagent must fully review the artifacts. Do not split artifacts between
agents.

Review and assess the correctness of each review, and then combine them into a
single output report.
