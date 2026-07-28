This run is fully autonomous. If any human decision is required, answer and proceed.

The instructions for this run are saved at `.agent-layer/tmp/instructions.md`.

Execute the following skills sequentially.

1. $plan-work
instructions: .agent-layer/tmp/instructions.md
plan_reviewers ({plan_reviewer_count}): {plan_reviewers}
Every plan-reviewer dispatch must use the `review-plan` skill.

2. $fully-implement-plan using the exact plan, task, and context artifacts returned by step 1
implementer: {implementer}
code_reviewer: {code_reviewer}
fixer: {fixer}
The implementer dispatch must use the `implement-plan` skill and the code-reviewer dispatch must use the `review-uncommitted-code` skill. The benchmark accepts a skills treatment only when evidence records each of those required role skills.
