# Fix Issue Log

## Purpose

Resolve live ISSUES.md entries that do not require human input. Accept IDs,
filters, or a user-supplied issue-count range or cap.

## Required roles

Require the common plan roles.

## Initialize

Read ISSUES.md's format and insertion marker. Skip and report malformed entries
without blocking valid work.

## Select

Honor a caller-supplied issue-count range or cap as a constraint, not a target.
Otherwise select the smallest coherent independently executable group; one issue
is valid. Add issues only when shared cause, outcome, or verification makes them
one reviewable repair. Prefer prerequisites and material impact, then source
order. Treat human-decision wording, proposed alternatives, next steps, and
notes in an issue as historical evidence rather than an authoritative blocker.
Refresh them against the current tree, accepted decisions, requested scope, and
the blocker contract before excluding the issue from autonomous work.

## Execute

Use direct repair execution for established, decision-ready work with concrete
acceptance behavior and a localized boundary. Use common plan execution when
the work does not meet that contract.

Keep executing the selected objective when an approach, check, tool, or
delegation fails: diagnose it, repair it, or reroute between the direct and
planned paths. Do not turn execution difficulty or broader-than-expected work
into a new stop condition. Only a human-input condition defined by
`blocker-classification.md` can pause the item; preserve it and continue
independent eligible work until the complete pass reaches the human question.
If evidence shows every safe retry and reroute path is exhausted, preserve
useful work, record the item as still blocked, and revisit it only after its
condition changes.

Include each verified ISSUES.md removal, BACKLOG.md reclassification, rejection,
or still-blocked disposition in the delivery. Leave blocked entries canonical
and unchanged.

## Reconcile

Confirm merged dispositions in the canonical memory files. Leave entries for
open or preserved deliveries unchanged until their delivery is authoritative.

## Exhaustion

A complete refreshed pass finds no live eligible entry.
