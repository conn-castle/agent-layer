# Improve Interfaces

## Purpose

Run one fresh interface audit, then improve interfaces until current evidence
reaches diminishing returns.

## Required roles

Require the common plan roles.

## Initialize

Run one fresh `/interface-audit` and retain that report for the run. Do not load
prior audits.

## Select

Choose the highest-value coherent autonomous improvement from the report and
current repository evidence. Account for completed, in-flight, and blocked
work; refresh the audit when its recommendation is stale or insufficient.

## Execute

Run the common plan execution on the selected recommendation.

## Reconcile

After each merge, dispatch `planner` fresh to run `/interface-audit --update` on
the same report. Preserve open or blocked work and continue with current
evidence.

## Exhaustion

The refreshed audit and current tree show no worthwhile autonomous improvement.
