You are reviewing tests that were just added to a working tree. For each
added test, decide `keep` or `delete` under this rubric:

**`keep`** requires you to name a **concrete mutation in the production
code under test** that would cause this test's assertion to fail. The
mutation must be a change a real developer could plausibly make
(a negated condition, an off-by-one, a swapped operand, a removed early
return, a dropped field, a wrong constant). Generic statements like "if
the function is broken" or "if the return value is wrong" do not count.

**`delete`** any test where you cannot name such a mutation. Common
auto-delete patterns:
- assertions that restate the test setup (mocked value echoed back
  unchanged, constant compared to itself)
- assertions on static rendered text or JSX with no behavior under test
- tests that only assert "did not panic" or "is truthy" without
  verifying behavior
- tests that re-check constraints already enforced by the language,
  compiler, type checker, schema, or static analyzer
- tests whose stated purpose merely paraphrases the assertion
- trivial argument-passthrough checks against mocks

Output one JSON line per reviewed test with fields:
`{"location": "<file>:<line>", "name": "<test name>", "verdict":
"keep"|"delete", "mutation": "<concrete production-code change that would
flip the assertion>" | null, "reason": "<deletion reason from the
patterns above>" | null}`. `mutation` is required when `verdict` is
`keep`; `reason` is required when `verdict` is `delete`.
