package messages

// Sync messages for the sync command.
const (
	// SyncUse is the sync command name.
	SyncUse                                         = "sync"
	SyncShort                                       = "Regenerate client outputs from .agent-layer"
	SyncCompletedWithWarnings                       = "sync completed with warnings"
	SyncAgentEnabledFlagMissingFmt                  = "agent %s is missing enabled flag in config"
	SyncAgentDisabledFmt                            = "agent %s is disabled in config"
	SyncMarshalMCPConfigFailedFmt                   = "failed to marshal mcp config: %w"
	SyncCreateDirFailedFmt                          = "failed to create %s: %w"
	SyncWriteFileFailedFmt                          = "failed to write %s: %w"
	SyncMarshalClaudeSettingsFailedFmt              = "failed to marshal claude settings: %w"
	SyncMarshalVSCodeSettingsFailedFmt              = "failed to marshal vscode settings: %w"
	SyncMarshalVSCodeMCPConfigFailedFmt             = "failed to marshal vscode mcp config: %w"
	SyncMarshalCodexAgentSpecificFailedFmt          = "failed to marshal codex agent-specific config: %w"
	SyncCodexTrustRootRequired                      = "repo root required for codex trust stanza"
	SyncCodexTrustRootResolveFailedFmt              = "failed to resolve repo root for codex trust stanza %q: %w"
	SyncCodexTrustRootControlCharFmt                = "repo root %q for codex trust stanza contains a control character (U+%04X) that cannot be encoded as a valid TOML key; move the repository to a path without control characters"
	SyncCodexTrustRootInvalidUTF8Fmt                = "repo root %q for codex trust stanza contains invalid UTF-8 bytes that cannot be encoded as a valid TOML key; move the repository to a path with valid UTF-8 characters"
	SyncGrokTrustRootRequired                       = "repo root required for grok folder trust"
	SyncGrokTrustRootResolveFailedFmt               = "failed to resolve repo root for grok folder trust %q: %w"
	SyncGrokTrustReadFailedFmt                      = "failed to read grok folder trust %s: %w"
	SyncGrokTrustParseFailedFmt                     = "failed to parse grok folder trust %s: %w"
	SyncCodexStatuslineInvalidTOMLFmt               = "%s: invalid codex statusline TOML: %w"
	SyncCodexStatuslineOnlyStatusLineFmt            = "%s: codex statusline fragment may contain only [tui].status_line; put unrelated Codex config under agents.codex.agent_specific"
	SyncCodexStatuslineStatusLineMissingFmt         = "%s: codex statusline fragment must define [tui].status_line"
	SyncCodexStatuslineStatusLineTypeFmt            = "%s: [tui].status_line must be an array of strings"
	SyncCodexStatuslineTUITableConflict             = "agents.codex.agent_specific.tui must be a table to merge managed status_line; set agents.codex.statusline = false or define agents.codex.agent_specific.tui.status_line explicitly"
	SyncCodexExistingConfigInvalidFmt               = "%s: invalid existing Codex config TOML; fix the file or move it aside before running al sync: %w"
	SyncCodexExistingConfigShapeConflictFmt         = "%s: existing Codex config has incompatible shape at %s; fix that value before running al sync"
	SyncCodexAgentSpecificProjectsTableConflict     = "agents.codex.agent_specific.projects must be a table; fix or remove that passthrough before running al sync"
	SyncCodexAgentSpecificProjectEntryTableFmt      = "agents.codex.agent_specific.projects.%q must be a table; fix or remove that passthrough before running al sync"
	SyncClaudeQuestionToolKeyTableConflictFmt       = "agents.claude.agent_specific.%s must be a table to merge the managed AskUserQuestion block; remove or fix that override, or set agents.claude.disable_question_tool = false"
	SyncClaudeQuestionToolListConflictFmt           = "agents.claude.agent_specific.%s must be a list to merge the managed AskUserQuestion block; remove or fix that override, or set agents.claude.disable_question_tool = false"
	SyncLegacyAgentSpecificChimeFmt                 = "%s contains an Agent Layer chime hook; remove that per-agent hook and set notifications.chime = true under [notifications]"
	SyncChimeKeyTableConflictFmt                    = "%s must be a table to merge the managed notifications chime hook; remove or fix that override, or set notifications.chime = false"
	SyncChimeListConflictFmt                        = "%s must be a list to merge the managed notifications chime hook; remove or fix that override, or set notifications.chime = false"
	SyncChimePathConflictFmt                        = "%s must be a real file inside the repository while cleaning Agent Layer chime hooks"
	SyncCodexChimeMarkerConflictFmt                 = "%s: Agent Layer-managed Codex chime hook markers are incomplete or ambiguous; remove the marked chime block before running al sync"
	SyncCodexChimeOwnershipConflictFmt              = "%s contains an augmented or ambiguous Agent Layer chime hook; remove that hook before running al sync"
	SyncAntigravityChimePluginConflictFmt           = "%s already exists and is not the Agent Layer-managed chime plugin; remove or rename it before running al sync"
	SyncGrokChimeHookConflictFmt                    = "%s already exists and is not the Agent Layer-managed Grok chime hook; remove or rename it before running al sync"
	SyncClaudeStatuslineSourceMissingFmt            = "agents.claude.statusline is true but %s is missing; run `al wizard` to create the source file, run interactive `al upgrade` to review statusline sources, or create the file manually"
	SyncCodexStatuslineSourceMissingFmt             = "agents.codex.statusline is true but %s is missing; run `al wizard` to create the source file, run interactive `al upgrade` to review statusline sources, or create the file manually"
	SyncMarshalAntigravitySettingsFailedFmt         = "failed to marshal antigravity settings: %w"
	SyncMarshalAntigravityMCPConfigFailedFmt        = "failed to marshal antigravity MCP config: %w"
	SyncMarshalCopilotMCPConfigFailedFmt            = "failed to marshal copilot mcp config: %w"
	SyncInvalidVSCodeSettingsFmt                    = "invalid vscode settings %s: %w"
	SyncReadTemplateFailedFmt                       = "failed to read template %s: %w"
	SyncReadFailedFmt                               = "failed to read %s: %w"
	SyncRemoveFailedFmt                             = "failed to remove %s: %w"
	SyncRenameFailedFmt                             = "failed to rename %s to %s: %w"
	SyncMCPServerErrorFmt                           = "mcp server %s: %w"
	SyncMCPServerArgFailedFmt                       = "mcp server %s arg: %w"
	SyncCodexHeaderPlaceholderUnsupportedFmt        = "codex header %s must be literal or use ${VAR}"
	SyncCodexAuthorizationPlaceholderUnsupportedFmt = "authorization header must be literal, ${VAR}, or Bearer ${VAR}"
	SyncSystemRequired                              = "sync system is required"
	SyncProjectRequired                             = "sync project is required"
	SyncConfigFSRequired                            = "sync config filesystem is required"
	SyncFailedReadGitignoreBlockFmt                 = "failed to read gitignore block %s: %w"
	SyncOpenLockFmt                                 = "failed to open sync lock %s: %w"
	SyncLockFmt                                     = "failed to lock sync %s: %w"
	SyncLockTimeoutFmt                              = "timed out after %s waiting for sync lock %s; another sync may still be generating files. Wait for it to finish, then retry"
	SyncUnlockFmt                                   = "failed to unlock sync %s: %w"
	SyncCloseLockFmt                                = "failed to close sync lock %s: %w"

	MCPServerResolveFmt              = "mcp server %s: %w"
	MCPServerURLFmt                  = "mcp server %s url: %w"
	MCPServerHeaderFmt               = "mcp server %s header %s: %w"
	MCPServerCommandFmt              = "mcp server %s command: %w"
	MCPServerArgFmt                  = "mcp server %s arg %s: %w"
	MCPServerEnvFmt                  = "mcp server %s env %s: %w"
	MCPUnsupportedTransportFmt       = "unsupported transport %s"
	MCPServerUnsupportedTransportFmt = "mcp server %s: unsupported transport %s"
)

// Grok path messages are kept together because sync applies the same safety
// checks while generating and cleaning its project-local outputs.
const (
	SyncGrokHomePermissionsFmt         = "grok home directory must be private: %s has permissions %04o; run `chmod 700 %s` and retry"
	SyncGrokConfigDirConflictFmt       = "grok config directory must be a real directory: %s"
	SyncGrokConfigTargetConflictFmt    = "grok config target must be a regular file, not a symlink or special file: %s"
	SyncGrokConfigOwnershipConflictFmt = "refusing to overwrite user-owned Grok config %s; move managed settings into .agent-layer/config.toml or agents.grok.agent_specific first"
)

// Skill source snapshot messages. These describe ownership problems that only
// exist once Git-backed imports are configured alongside user-managed skills.
const (
	// SyncImportedSkillOrphanFmt reports imported directories with no lock entry.
	SyncImportedSkillOrphanFmt = "imported skill directories have no entry in .agent-layer/skills.lock.json: %s; move each one into .agent-layer/skills/ to adopt it as user-managed, or delete it, then run 'al skills status'"
	// SyncImportedSkillInvalidFmt reports an imported skill whose local content
	// Agent Layer cannot project faithfully.
	SyncImportedSkillInvalidFmt = "imported skill %s cannot be projected: %v; fix it in place, adopt it as user-managed, or run 'al skills pull' to restore it from its source"
	// SyncSkillTierCollisionFmt reports one skill name owned by both tiers.
	SyncSkillTierCollisionFmt = "skill %q exists in both %s and %s; Agent Layer never shadows one source with the other -- remove one directory or narrow the import selector that produced it"
)
