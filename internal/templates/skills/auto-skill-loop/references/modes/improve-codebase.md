# Improve Codebase

## Purpose

Improve selected repository scopes and lenses until a fresh pass finds no
material work.

## Required roles

Use only `mode_worker` for mode execution; do not require common plan roles.

## Initialize

Identify repository-native scopes and relevant quality lenses incrementally,
noting enough progress to cover all of them before declaring exhaustion.

## Select

Select one useful scope and its relevant lenses. Use the whole repository when
practical; otherwise choose one coherent component and necessary cross-cutting
boundaries.

## Execute

Dispatch `mode_worker` to run `/improve-codebase` exactly once on the selected
scope and lenses. Do not invoke another skill or the common plan flow.

## Reconcile

Record completed scope and preserve open work. Revisit only areas affected by
later changes.

## Exhaustion

All current components and relevant cross-cutting lenses have fresh coverage,
and a final wide pass reports no material finding.
