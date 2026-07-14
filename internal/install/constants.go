package install

const (
	stepWriteVSCodeLaunchers       = "writeVSCodeLaunchers" // #nosec G101 -- upgrade step identifier, not a credential.
	commandsAllowName              = "commands.allow"
	configFileName                 = "config.toml"
	instructionsDirName            = "instructions"
	docsAgentLayerDir              = "docs/agent-layer"
	issuesPath                     = "docs/agent-layer/ISSUES.md"
	backlogPath                    = "docs/agent-layer/BACKLOG.md"
	roadmapPath                    = "docs/agent-layer/ROADMAP.md"
	claudeStatuslineTemplate       = "claude-statusline.sh"
	codexStatuslineKey             = "agents.codex.statusline"
	unsetValue                     = "(unset)"
	deleteOldAntigravityAgentsOpID = "a-delete-old-agents-antigravity"
	agentAntigravity               = "antigravity"
	agentClaude                    = "claude"
)
