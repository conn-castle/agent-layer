package warnings

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/config"
)

func TestCheckPolicy_SecretInURL(t *testing.T) {
	enabled := true
	project := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				Codex: config.CodexConfig{Enabled: &enabled},
			},
			MCP: config.MCPConfig{
				Servers: []config.MCPServer{
					{
						ID:        "github",
						Enabled:   &enabled,
						Transport: config.TransportHTTP,
						URL:       "https://example.com/mcp?api_key=raw_secret",
					},
				},
			},
		},
	}

	results := CheckPolicy(project)
	require.Len(t, results, 1)
	require.Equal(t, CodePolicySecretInURL, results[0].Code)
	require.Equal(t, "github", results[0].Subject)
	require.Equal(t, SeverityCritical, results[0].Severity)
	require.Equal(t, SourceInternal, results[0].Source)
}

func TestCheckPolicy_SecretInURL_PasswordOnlyUserinfo(t *testing.T) {
	enabled := true
	project := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				Codex: config.CodexConfig{Enabled: &enabled},
			},
			MCP: config.MCPConfig{
				Servers: []config.MCPServer{
					{
						ID:        "password-only-userinfo",
						Enabled:   &enabled,
						Transport: config.TransportHTTP,
						URL:       "https://:supersecret@example.com/mcp",
					},
				},
			},
		},
	}

	results := CheckPolicy(project)
	require.Len(t, results, 1)
	require.Equal(t, CodePolicySecretInURL, results[0].Code)
	require.Equal(t, "password-only-userinfo", results[0].Subject)
}

func TestCheckPolicy_CodexHeaderPolicy(t *testing.T) {
	enabled := true
	project := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				Codex: config.CodexConfig{Enabled: &enabled},
			},
			MCP: config.MCPConfig{
				Servers: []config.MCPServer{
					{
						ID:        "srv",
						Enabled:   &enabled,
						Transport: config.TransportHTTP,
						URL:       "https://example.com/mcp",
						Headers: map[string]string{
							"Authorization": "Token ${AL_TOKEN}",
						},
					},
				},
			},
		},
	}

	results := CheckPolicy(project)
	require.Len(t, results, 1)
	require.Equal(t, CodePolicyCodexHeaderForm, results[0].Code)
	require.Equal(t, "srv", results[0].Subject)
}

func TestCheckPolicy_CapabilityMismatch(t *testing.T) {
	enabled := true
	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: "all"},
			Agents: config.AgentsConfig{
				Antigravity: config.AgentConfig{Enabled: &enabled},
			},
			MCP: config.MCPConfig{
				Servers: []config.MCPServer{
					{
						ID:        "srv",
						Enabled:   &enabled,
						Clients:   []string{"antigravity"},
						Transport: config.TransportHTTP,
						URL:       "https://example.com/mcp",
					},
				},
			},
		},
	}

	results := CheckPolicy(project)
	require.Len(t, results, 2)
	require.Equal(t, CodePolicyCapabilityMismatch, results[0].Code)
	require.Equal(t, "srv", results[0].Subject)
	require.Equal(t, CodePolicyCapabilityMismatch, results[1].Code)
	require.Equal(t, "approvals.mode", results[1].Subject)
}
