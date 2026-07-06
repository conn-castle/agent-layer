# Interface Audit Update Workflow

Use this file only when `/interface-audit` is invoked with `--update`.

Update one report in place, then return to the parent skill's final
recommendation gate.

## Select The Report

If the user supplied an explicit report path, use that path. If it does not
exist, stop and report that the requested update target is missing.

If no path was supplied, select the newest report with:

```bash
find .agent-layer/tmp -maxdepth 1 -type f -name 'interface-audit.[0-9]*.md' -print | LC_ALL=C sort -r | head -n 1
```

If the command returns no path, stop. Tell the user no existing interface audit
report was found and ask whether to run a fresh `/interface-audit`.

Before editing, read enough of the selected report to preserve its structure.
Do not create a parallel update report.

## Establish Update Boundary

Read `Last updated UTC:` from metadata. This is the update boundary.

If `Last updated UTC:` is missing or malformed, stop. Tell the user the report
does not have a reliable update boundary and recommend running a fresh audit.

Record the current UTC timestamp. Use it as the new `Last updated UTC:` only
after the report has actually been updated.

## Inspect Local Changes

Run read-only local checks:

```bash
git status --porcelain
git diff --stat
git diff --name-only
```

If local changes touch files that affect existing or potential interface rows,
inspect the relevant diffs. Treat the dirty working tree as current repository
state, but label uncommitted evidence as local changes in the report update log.

Ignore unrelated local changes except for the brief working-tree summary in
metadata.

## Inspect Recently Merged PRs

Use GitHub CLI to query candidate merged PRs from the date portion of
`Last updated UTC:`:

```bash
gh pr list --state merged --search 'merged:>=<LAST_UPDATED_DATE>' --limit 100 --json number,title,mergedAt,url,headRefName,baseRefName,author
```

Replace `<LAST_UPDATED_DATE>` with the report's `YYYY-MM-DD`. Then filter JSON
to PRs whose `mergedAt` is strictly after the full `Last updated UTC:`. Do not
rely on date-only search as the final boundary.

If `gh` is unavailable or PR data cannot be retrieved, stop and report the exact
failure. Do not infer merged PR context from local git history unless the user
explicitly approves a local-git-only update.

Count the filtered PRs:
- If 0-10 PRs are found, inspect all of them.
- If more than 10 PRs are found, stop before editing. Warn the user with the
  count, explain that the update would be capped at 10 PRs, and recommend
  rerunning a fresh interface audit instead of updating the old report. Ask
  whether they want a fresh audit or an update capped to the 10 most recent PRs.
- If the candidate query returns exactly 100 PRs, treat the count as truncated,
  stop, and recommend a fresh audit unless the user explicitly approves a capped
  update.

For each inspected PR:

```bash
gh pr view <number> --json number,title,body,mergedAt,url,files,commits
gh pr diff <number> --name-only
```

Read changed files only where they affect interface boundaries, score evidence,
tests, product requirements, or the proposed next spec.

## Update The Report

Update the selected report in place using `report-structure.md` as the contract.
Refresh metadata, changed rows, affected supporting sections, the proposed next
spec, and the update log. The report body describes current code; historical
change notes belong only in the update log.

## Required Post-Update Checks

Before final handoff, re-read each changed row against current evidence and
confirm the changed claims satisfy the parent skill's audit rules.

## Final Step

Do not continue into planning or implementation. Complete the final
recommendation gate from the parent skill.
