package messages

// TransitionRecoveryGuidance directs users to the old executable because upgrade bypasses dispatch.
const TransitionRecoveryGuidance = "Install or run the Agent Layer v1.0.0 executable itself. Use it to run al upgrade, then its al wizard to adopt or recover, then retry with this version"

// Transition boundary refusal and recovery guidance.
const (
	TransitionUnsupportedSourceFmt = "source version %s (%s) is unsupported: this Agent Layer no longer converts pre-1.0.0 projects. " + TransitionRecoveryGuidance
	TransitionPristineSkill        = "this Agent Layer no longer adopts this shipped local skill. " + TransitionRecoveryGuidance + " Alternatively, rename or remove its directory"
	TransitionPristineInstruction  = "this Agent Layer no longer adopts this shipped instruction. " + TransitionRecoveryGuidance + " Alternatively, rename the file and its [[instructions.local]] selector"
	AdoptionJournalFmt             = "%s: this Agent Layer no longer recovers adoption transitions; preserve journals and backups. " + TransitionRecoveryGuidance
	JournalRegularFileFmt          = "%s must be a regular unlinked journal; preserve recovery evidence and repair the node"
	TransitionConfigFmt            = "%s contains pre-1.0.0 configuration: this Agent Layer no longer converts it. " + TransitionRecoveryGuidance
	TransitionRollbackFmt          = "snapshot %s captures unsupported source version %s; install or run the Agent Layer v1.0.0 or v1.1.0 executable itself to perform this rollback"
	TransitionRetiredOperationFmt  = "operation %s (%s) requires an unsupported source: this Agent Layer no longer executes it. " + TransitionRecoveryGuidance
	ConfigUnorderedInstructionsFmt = "unconfigured instruction source %s: add explicit [[instructions.local]] entries with unique orders in .agent-layer/config.toml or run al wizard; projects created before v1.0.0 must first complete conversion. " + TransitionRecoveryGuidance
)

// Local source conflicts during ordinary import operations.
const (
	CatalogLocalConflictFmt      = "local skill %s already owns this name; rename or remove it before importing"
	RetiredLocalSkillConflictFmt = "local skill %s already owns the name %q; preserve its complete local tree and rename or remove the local copy before importing. To adopt instead: " + TransitionRecoveryGuidance
	LegacyInstructionConflictFmt = "local instruction %s already exists; rename the file and its [[instructions.local]] selector or remove both before importing. To adopt instead: " + TransitionRecoveryGuidance
)

// DoctorLegacyConfigPathFmt explains versioned config recovery.
const DoctorLegacyConfigPathFmt = "Convert %s before retrying. " + TransitionRecoveryGuidance

// TransitionRealDirectoryFmt rejects linked or non-directory source ancestors.
const TransitionRealDirectoryFmt = "%s must be a real unlinked directory"

// Catalog import and wizard preview messages.
const (
	CatalogSelectorUnsupportedFmt = "%q is not a catalog selector"
	CatalogMemberProblemFmt       = "catalog %s: %s"
	WizardImportMissingFmt        = "Import missing %s from %s (network required)"
	WizardImportInstructionFmt    = "Import %s from %s instructions/%s (network required)"
)
