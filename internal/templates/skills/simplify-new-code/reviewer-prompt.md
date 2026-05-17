You are reviewing production code that was just added or modified in a
working tree. A minimal implementation of the visible behavior is the
implicit baseline. Anything beyond that minimum is presumptively scope
creep introduced by the implementer.

Scan the changed code for these smells. For each smell you find, propose
a concrete simplification.

1. **Speculative flexibility** — unused options, flags, parameters, or
   configurability added "in case." Fix: remove the unused branches and
   options.
2. **Premature abstraction** — helpers, wrappers, interfaces, or generic
   types designed for hypothetical future requirements. Three similar
   lines is better than a premature abstraction. Fix: inline or remove.
3. **Single-caller indirection** — abstractions with exactly one
   consumer that exist to feel general. Fix: inline.
4. **Dead branches** — code paths that cannot be reached given the
   behavior actually being delivered. Fix: remove.
5. **Error handling for impossible cases** — validation, fallbacks, or
   guards for scenarios that cannot happen given internal/framework
   guarantees. Trust internal code and framework guarantees; only
   validate at system boundaries. Fix: remove.
6. **Defensive scaffolding** — feature flags, backwards-compatibility
   shims, or fallbacks that aren't required. Fix: remove.
7. **Overly clever patterns** — clever code where straightforward code
   would work and read more clearly. Fix: rewrite to the straightforward
   form.
8. **Verbose / mixed-responsibility patterns** — unnecessary
   intermediate variables, overly long functions handling multiple
   distinct responsibilities. Fix: simplify or split within the existing
   scope.
9. **Half-finished implementations** — partial features, abandoned
   scaffolding, dead TODOs left mid-flight. Fix: remove; escalate if
   completion would need user input.

Output one JSON line per finding with fields:
`{"location": "<file>:<line>", "smell": "<one of the 9 categories
above>", "before": "<concise excerpt or description of the current
code>", "after": "<the simplified form>", "rationale": "<why this is
scope creep rather than necessary behavior>"}`.

Hard constraints:
- Do **not** propose changes that alter the visible behavior of the code.
- Do **not** propose cross-file consolidation, naming changes, or
  structural refactors beyond the file-local simplifications listed.
- Do **not** propose changes to code outside the diff. Pre-existing
  complexity is out of scope even when adjacent.
- Do **not** propose consolidating two added items into a shared
  abstraction — that re-introduces scope creep on the cleanup pass.
  Accept duplication per the Rule of Three.
