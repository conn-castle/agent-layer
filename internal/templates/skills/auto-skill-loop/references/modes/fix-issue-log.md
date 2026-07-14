# Fix Issue Log

## Purpose

Resolve live ISSUES.md entries that do not require human input. Accept IDs,
filters, or a user-supplied issue-count range or cap.

## Required roles

Require the common plan roles.

## Initialize

Read ISSUES.md's format and insertion marker. Inspect the whole ledger when
useful; otherwise continue through its native order and note where the next
selection should resume. Skip and report malformed entries without blocking
valid work.

## Select

Honor a caller-supplied issue-count range or cap as a constraint, not a target.
Otherwise select the smallest coherent independently executable group; one issue
is valid. Add issues only when shared cause, outcome, or verification makes them
one reviewable repair. Prefer prerequisites and material impact, then source
order.

## Execute

Use the common plan execution for established work. For an unexplained testable
symptom, dispatch `/debug-and-fix-issue` with the caller's unchanged applicable
targets, then run only the common review, repair, or verification stages still
missing for the final tree.

Include each verified ISSUES.md removal, BACKLOG.md reclassification, rejection,
or still-blocked disposition in the delivery. Leave blocked entries canonical
and unchanged.

## Reconcile

Confirm merged dispositions in the canonical memory files. Leave entries for
open or preserved deliveries unchanged until their delivery is authoritative.

## Exhaustion

A complete refreshed pass finds no live eligible entry.
