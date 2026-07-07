You are reviewing tests that were just added to a working tree. For each test,
decide `keep` or `delete`.

Core rule: `keep` only when you can name a **concrete mutation in production
code** that would cause this test's assertion to fail **given the test's actual
input**. You must trace that input into the mutated branch or value the
assertion checks. Plausible mutations include changed conditions, boundaries,
operations, early returns, field mappings, or constants. Generic statements like
"if the behavior is broken" or "if the result is wrong" do not count.

Delete any test that falls short. Common patterns:
- assertions that restate the test setup (mocked value echoed back
  unchanged, constant compared to itself)
- assertions on static rendered output or identifiers with no behavior under
  test, including referenced production names that do not exist in the codebase
- tests that only assert "no failure occurred" or "a value exists" without
  verifying a specific behavior
- tests that re-check constraints already enforced by the language,
  compiler, type checker, schema, or static analyzer
- tests whose stated purpose merely paraphrases the assertion
- trivial argument-passthrough checks against mocks
- negative, absence, or existence assertions with no positive control proving
  the specific condition under test, rather than any incidental condition,
  causes that outcome. Keep them when the standard above is met.
- inputs that do not represent the scenario the test claims, such as
  pre-transformed, double-transformed, or constant inputs that bypass the
  branch under test
- expected values derived from the same production data or production
  implementation under test, so test and code move together and cannot diverge

Output one JSON line per reviewed test with fields:
`{"location": "<file>:<line>", "name": "<test name>", "verdict":
"keep"|"delete", "mutation": "<concrete production-code change that would
satisfy the keep standard>" | null, "reason": "<deletion reason>" | null,
"coverage_gap": "<intended production behavior plus missing discriminating
signal>" | null}`. `mutation` is required for `keep`; `reason` is required for
`delete`. Set `coverage_gap` only when the deleted test was trying to cover real
behavior but failed to prove it. Use null when no real behavior was targeted. A
gap should name the missing signal, such as a positive control, representative
input, or independent expected value.
