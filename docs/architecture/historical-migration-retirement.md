# Historical migration retirement: v1.1.0 implementation plan

Status: source inventory complete; implementation and independent plan review pending. No release metadata has been changed and no release publication is authorized.

The accepted decision requires projects to finish v1.0.0 conversion before v1.1.0. Installing a v1.0.0 binary alone does not prove optional adoption completed. Reject unfinished legacy state before mutation and direct users to complete conversion/recovery with v1.0.0. Preserve user data, ongoing import transactions, local edits, crash recovery and immutable published manifests.

## Source inventory and boundaries

| Current paths | Work | Retained behavior |
| --- | --- | --- |
| internal/install/upgrade_migrations_skills.go and migrate_skills_format branches in upgrade_migrations.go | Remove flat-to-directory migration execution and its dedicated tests; preflight reject slash-commands/flat files and unsupported old installation state | General current-version upgrade planning, dry-run, snapshots and data preservation |
| internal/wizard/apply_cli_skills.go, apply.go, legacy instruction previews | Remove historical adoption previews and execution after replacing with a before-mutation rejection | Ordinary catalog selection/install and explicit instruction order handling |
| internal/skillimport/catalog.go, instructions.go, transaction.go, AddOptions.AdoptLegacy, RetireLocal | Remove legacy-local retirement/adoption capability and tests | Normal imports, pull/push/reset/remove, selector identity, imported edits, imported-to-user ownership transfer |
| internal/skilljournal/skilljournal.go AdoptionVersion=2, LocalRetirements, LocalBackupPrefix and legacy instruction map | Reject unfinished adoption-v2 recovery with explicit v1.0.0 instructions, before any journal or tree mutation; remove legacy retirement executor | Ordinary journal-v1 transactions, lock acquisition, crash recovery and fail-closed evidence validation |
| internal/skillmigration/safeguard.go; install, wizard/profile, versiondispatch and rollback callers | Replace legacy-slot checks with supported compatibility floor and small unfinished-state/incompatible-recovery checks | Shared project lock and normal recovery wrappers must survive independently |
| internal/install/upgrade_plan.go and upgrade_rollback.go converted-slot/config protection | Replace migration-only checks with general compatible snapshot/version boundary, retaining ordinary ownership protections | Snapshots remain untouched on refusal; completed compatible snapshots can roll back |
| internal/templates/manifests/*.json and migrations/*.json | Preserve every published document byte-identically; add new versioned contract only after exact-version authorization | Historical release provenance stays immutable |
| scripts/test-e2e upgrade scenarios | Route earlier supported fixtures through released v1.0.0 then completed conversion before candidate; cover explicit refusal for direct legacy transition | Do not silently retain obsolete executor just to keep old direct-upgrade tests green |

## Implementation order

1. Approve the exact v1.1.0 preparation version before editing release-versioned contract files (docs/agent-layer/COMMANDS.md Release approval gate). No tag, release dispatch or publishing is included. Confirm implementation dispatch target if the planned implement workflow requires one.
2. Independently review the boundary using requested Claude Opus high and Grok once authentication is restored. Resolve which state must reject before acquiring a mutating recovery path; explicitly cover active v2 journal, flat skills, old instructions, mixed converted/unconverted state, imported local edits and old snapshots.
3. Add the small boundary detector and generic floor first. Make every relevant mutation entry point check it before its first write or recovery mutation, without treating a version pin alone as conversion evidence.
4. Remove historical execution and dedicated tests, while keeping normal transaction recovery APIs. Refactor wrappers rather than deleting shared lock/recovery behavior with the legacy package.
5. Prepare only the authorized new release contract and current documentation. Keep published manifests and old changelog entries unchanged.
6. Validate rejection fixtures by exact directory/byte/mode comparison before and after each mutation attempt; prove completed imported projects still operate and ordinary journal-v1 crash recovery succeeds. Check compatible/incompatible rollbacks and CLI delegation separately.
7. Run complete make ci and current upgrade contracts. Obtain requested independent code review; fix accepted findings; open/update a focused PR with the unsupported-source behavior and v1.0.0 recovery instructions.

## Acceptance checklist

- [ ] Exact target preparation version authorized (no publication authorization inferred)
- [ ] Independent boundary review completed
- [ ] No one-time adoption or flat-format transition executor remains
- [ ] All unfinished/mixed legacy and adoption-v2 states reject before mutation with v1.0.0 guidance
- [ ] General compatibility floor replaces migration-specific downgrade and rollback checks
- [ ] Normal imports/local edits and journal-v1 crash recovery retain coverage
- [ ] Published manifests byte-identical
- [ ] Earlier supported installations migrate through released v1.0.0 in E2E coverage
- [ ] Full verification and independent final code review pass

This plan is preparation, not a claim that the debt is fixed or a merge-ready implementation.
