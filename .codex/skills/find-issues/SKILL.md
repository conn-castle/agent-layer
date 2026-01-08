---
name: find-issues
description: proactive code quality audit to find over-engineering, bad practices, and band-aids.
---

<!--
  GENERATED FILE - DO NOT EDIT DIRECTLY
  Source: .agentlayer/workflows/find-issues.md
  Regenerate: node .agentlayer/sync/sync.mjs
-->

# find-issues

proactive code quality audit to find over-engineering, bad practices, and band-aids.

1. **Narrow Audit**
   - Look through all of the files that have changed since the last commit.
   // turbo
   - git status && git diff
   - Identify over-complication or over-engineering.
   - Find places where best practices are not followed or abstractions are poor.
   - Find places where fallbacks or defaults are used.
   - Identify "band-aids" or quick-and-dirty fixes.
   - Compare findings to `README.md` and `ROADMAP.md` to ensure future development goals are respected.

2. **Deep Dive**
   - Dig one layer deeper based on initial findings, looking across the entire codebase.
   - Look for related band-aids and DRY (Don't Repeat Yourself) violations.
   - Look for poor architecture or abstraction, and find places where extensibility suffers.

3. **Broad Audit**
   - Look through the rest of the codebase
   - Identify architectural issues
   - Identify over-complication or over-engineering.
   - Find places where best practices are not followed or abstractions are poor.
   - Find places where fallbacks or defaults are used.
   - Identify "band-aids" or quick-and-dirty fixes.
   - Compare findings to `README.md` and `ROADMAP.md` to ensure future development goals are respected.

4. **Report and Review**
   - Present findings to the user for review.
   - **Do NOT** automatically add issues to `docs/ISSUES.md`.
   - Discuss with the user to decide which findings should be documented.
