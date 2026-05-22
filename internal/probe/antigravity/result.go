package antigravity

import "time"

// CapabilityMatrix records the Antigravity behaviors the probe can verify.
type CapabilityMatrix struct {
	PermissionsLoaded        bool `json:"permissions_loaded"`
	MCPConfigMigrated        bool `json:"mcp_config_migrated"`
	MCPRuntimeDiscovery      bool `json:"mcp_runtime_discovery"`
	WorkspacePermissionsRead bool `json:"workspace_permissions_read"`
	InstructionsLoaded       bool `json:"instructions_loaded"`
	SkillNamesVisible        bool `json:"skill_names_visible"`
	MCPConfigNamesVisible    bool `json:"mcp_config_names_visible"`
	SharedSkillDedupObserved bool `json:"shared_skill_dedup_observed"`
}

// Result is the JSON-serializable output of an Antigravity capability probe.
//
// AgyConfigDir is the absolute path passed to `agy --gemini_dir=…`. The
// upstream flag is named `--gemini_dir` for backward compatibility with
// the Gemini CLI lineage; Agent Layer's wire format renames the field so
// the public JSON contract does not bake in the legacy name.
type Result struct {
	AgyVersion       string           `json:"agy_version,omitempty"`
	ProbedAt         time.Time        `json:"probed_at"`
	ProbeDir         string           `json:"probe_dir"`
	WorkspaceDir     string           `json:"workspace_dir"`
	AgyConfigDir     string           `json:"agy_config_dir"`
	LogPath          string           `json:"log_path,omitempty"`
	ExitCode         int              `json:"exit_code"`
	WallClockSeconds int              `json:"wall_clock_seconds"`
	Capabilities     CapabilityMatrix `json:"capabilities"`
	Evidence         []string         `json:"evidence,omitempty"`
	Error            string           `json:"error,omitempty"`
}
