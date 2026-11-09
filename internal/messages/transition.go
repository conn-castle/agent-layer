package messages

// TransitionPreV1Steps gives exact, copy-pasteable commands for a project still
// on a release before v1.0.0. Upgrade bypasses version dispatch, so the v1.0.0
// executable is installed into a temporary prefix and run directly.
const TransitionPreV1Steps = "first upgrade the project with the v1.0.0 executable, then rerun al upgrade with this version:\n" +
	"  tmp=\"$(mktemp -d)\"\n" +
	"  curl -fsSL https://github.com/conn-castle/agent-layer/releases/download/v1.0.0/al-install.sh | bash -s -- --version v1.0.0 --prefix \"$tmp\" --no-completions\n" +
	"  \"$tmp/bin/al\" upgrade\n" +
	"  al upgrade"

// TransitionRecoveryGuidance repairs content this release no longer converts.
// Projects on v1.0.0 or later repair by hand; only pre-1.0 projects need v1.0.0.
// It ends error strings, so it has no trailing period; text appended after it
// must start a new sentence with ". ".
const TransitionRecoveryGuidance = "Repair this content by hand. Only a project still on a release before v1.0.0 needs the v1.0.0 executable; run al upgrade there to print the exact commands"

// Transition boundary refusal and recovery guidance.
const (
	TransitionUnsupportedSourceFmt   = "source version %s (%s) is unsupported: this Agent Layer no longer converts projects from before v1.0.0. To continue, " + TransitionPreV1Steps
	TransitionConvertBeforeWizardFmt = "%d unmodified shipped local copies must become Git imports first. Run al upgrade to convert them, then retry. To keep them as local content instead, rename each skill directory, or each instruction file and its [[instructions.local]] selector"
	AdoptionJournalFmt               = "%s: an interrupted v1.0.0 or v1.1.0 adoption left this journal, and this Agent Layer no longer recovers it; preserve journals and backups, run AL_VERSION=1.1.0 al sync to let v1.1.0 recover it, then retry"
	JournalRegularFileFmt            = "%s must be a regular unlinked journal; preserve recovery evidence and repair the node"
	TransitionConfigFmt              = "%s contains configuration from before v1.0.0 that this Agent Layer no longer converts. Remove or replace the reported keys by hand (al doctor lists them). If this project is still on a release before v1.0.0, " + TransitionPreV1Steps
	TransitionRollbackFmt            = "snapshot %s captures unsupported source version %s; install or run the Agent Layer v1.0.0 or v1.1.0 executable itself to perform this rollback"
	TransitionRetiredOperationFmt    = "operation %s (%s) requires an unsupported source: this Agent Layer no longer executes it. To continue, " + TransitionPreV1Steps
)

// Unmodified shipped copy conversion during al upgrade.
const (
	UpgradeConvertPristineHeaderFmt            = "%d unmodified shipped local copies will become Git imports (requires network access to github.com/nicholasjconn/skills):\n"
	UpgradeConvertPristineItemFmt              = "  - %s -> %s\n"
	UpgradeConvertPristineDeleteFmt            = "  - %s (already imported from %s; remove the leftover local copy)\n"
	UpgradeConvertPristinePromptFmt            = "Convert %d unmodified shipped copies to Git imports?"
	UpgradeConvertPristineDeclined             = "Kept the unmodified shipped copies as user-managed local content; rerun al upgrade to convert them to Git imports later."
	UpgradeConvertPristineFetchFmt             = "could not fetch github.com/nicholasjconn/skills to convert %d unmodified shipped copies: %v. Retry when online, or rerun al upgrade interactively and decline the conversion to keep them as local copies"
	UpgradeConvertPristineChangedFmt           = "%s changed during the upgrade; preserve it and rerun al upgrade"
	UpgradeConvertPristineInconsistentFmt      = "%s is an unmodified shipped copy, but its import of %s is not intact (%v); this upgrade changed nothing. Inspect it with al %s status --all, move the local copy out of the project, repair or remove the import with al %s commands, then rerun al upgrade"
	UpgradeConvertPristineEditedFmt            = "%s changed during the upgrade, so it was not deleted; your edited copy is preserved at %s and the rollback restores the unmodified copy at the original path. Move your copy back to keep your changes, then rerun al upgrade"
	UpgradeConvertPristineEditedUnpreservedFmt = "%s changed during the upgrade and could not be moved out of the rollback's reach (%v); the rollback restores the unmodified copy at that path"
	UpgradeConvertPristineNotActive            = "%s was not imported as expected; its local copy is preserved"
	UpgradeConvertPristineMoveFmt              = "could not move %s out of the project to remove it atomically (%v); its local copy is preserved"
	UpgradePlanSectionConversions              = "Unmodified shipped copies to convert to Git imports (requires network access to github.com/nicholasjconn/skills)"
	UpgradePlanConversionItemFmt               = "  - %s -> %s\n"
	UpgradePlanConversionDeleteFmt             = "  - %s (import of %s already active; remove the leftover local copy)\n"
)

// Local source conflicts during ordinary import operations.
const (
	CatalogLocalConflictFmt      = "local skill %s already owns this name; rename or remove it before importing"
	RetiredLocalSkillConflictFmt = "local skill %s already owns the name %q; preserve its complete local tree and rename or remove the local copy before importing. If it is an unmodified shipped copy, run al upgrade to convert it to this import instead"
	LegacyInstructionConflictFmt = "local instruction %s already exists; rename the file and its [[instructions.local]] selector or remove both before importing. If it is an unmodified shipped copy, run al upgrade to convert it to this import instead"
)

// ConfigUnconfiguredLocalInstructionFmt is the loader's refusal of a local
// instruction file without its own [[instructions.local]] block. Upgrade gives
// the same guidance before mutation, even for an unmodified shipped file.
const ConfigUnconfiguredLocalInstructionFmt = "unconfigured local instruction %s: declare it in .agent-layer/config.toml with [[instructions.local]], selectors = [<filename.md>], and a unique order, or remove the file"

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
