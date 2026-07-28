This run is fully autonomous. If any human decision is required, answer and proceed.

The instructions for this run are saved at `.agent-layer/tmp/instructions.md`.

Execute the following skills sequentially.

1. $plan-work
instructions: .agent-layer/tmp/instructions.md
plan_reviewers ({plan_reviewer_count}): {plan_reviewers}

2. $fully-implement-plan using the exact plan, task, and context artifacts returned by step 1
implementer: {implementer}
code_reviewer: {code_reviewer}
fixer: {fixer}
