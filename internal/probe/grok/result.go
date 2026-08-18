package grok

import "time"

// CapabilityMatrix records the Grok behaviors the probe can verify.
type CapabilityMatrix struct {
	MCPToolInvoked    bool `json:"mcp_tool_invoked"`
	StreamingJSONUsed bool `json:"streaming_json_used"`
}

// Result is the JSON-serializable output of a Grok capability probe.
type Result struct {
	GrokVersion      string           `json:"grok_version,omitempty"`
	ProbedAt         time.Time        `json:"probed_at"`
	ProbeDir         string           `json:"probe_dir"`
	WorkspaceDir     string           `json:"workspace_dir"`
	GrokHomeDir      string           `json:"grok_home_dir"`
	ExitCode         int              `json:"exit_code"`
	TimedOut         bool             `json:"timed_out"`
	WallClockSeconds int              `json:"wall_clock_seconds"`
	Capabilities     CapabilityMatrix `json:"capabilities"`
	Evidence         []string         `json:"evidence,omitempty"`
	Error            string           `json:"error,omitempty"`
}
