package wizard

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBuildSummaryIncludesDisabledMCPServers(t *testing.T) {
	choices := NewChoices()
	choices.ApprovalMode = ApprovalAll
	choices.DisabledMCPServers["github"] = true
	choices.DefaultMCPServers = []DefaultMCPServer{{ID: "github"}}

	summary := buildSummary(choices)

	assert.Contains(t, summary, "Disabled MCP Servers (missing secrets):")
	assert.Contains(t, summary, "- github")
}

func TestBuildSummaryIncludesRestoredMCPServers(t *testing.T) {
	choices := NewChoices()
	choices.ApprovalMode = ApprovalAll
	choices.MissingDefaultMCPServers = []string{"context7"}
	choices.RestoreMissingMCPServers = true
	choices.DefaultMCPServers = []DefaultMCPServer{{ID: "context7"}}

	summary := buildSummary(choices)

	assert.Contains(t, summary, "Restored Default MCP Servers:")
	assert.Contains(t, summary, "- context7")
}

func TestApprovalModeLabelForValue_NotFound(t *testing.T) {
	label, ok := approvalModeLabelForValue("unknown")
	assert.False(t, ok)
	assert.Equal(t, "", label)
}
