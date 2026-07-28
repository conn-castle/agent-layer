# Fix Issue Log

## Purpose

Resolve live ISSUES.md entries that do not require human input. Accept IDs,
filters, or a user-supplied issue-count range or cap.

## Required roles

Require the common plan roles.

## Initialize

Read ISSUES.md's format and insertion marker. Skip and report malformed entries
without blocking valid work. Record the live eligible issue IDs as the active
cohort.

## Select

Honor a caller-supplied issue-count range or cap as a constraint, not a target.
Select one delivery from the active cohort. If a complete pass finds no
eligible member, refresh ISSUES.md. If live eligible issues remain, make them
the next active cohort and select one delivery from it. Report exhaustion only
when the refresh finds none.

## Execute

Use direct repair execution for established, decision-ready work with concrete
acceptance behavior and a localized boundary. Use common plan execution when
the work does not meet that contract.

Fix newly discovered work in the same delivery when it is adjacent, and
shares the delivery's change and verification boundary. Do not add it to
ISSUES.md first. Add a new entry only when the work is substantial and
independent enough to require broader design or separate verification.

Include each verified ISSUES.md removal, BACKLOG.md reclassification, rejection,
or still-blocked disposition in the delivery. Leave blocked entries canonical
and unchanged.

## Reconcile

Confirm merged dispositions in the canonical memory files. Leave entries for
open or preserved deliveries unchanged until their delivery is authoritative.

## Exhaustion

A complete refreshed pass finds no live eligible issue.
