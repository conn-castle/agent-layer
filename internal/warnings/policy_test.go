package warnings

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/testutil"
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

func TestCheckPolicy_CodexRejectsEmbeddedPlaceholderInOrdinaryHeader(t *testing.T) {
	enabled := true
	project := &config.ProjectConfig{Config: config.Config{
		Agents: config.AgentsConfig{Codex: config.CodexConfig{Enabled: &enabled}},
		MCP: config.MCPConfig{Servers: []config.MCPServer{{
			ID:        "srv",
			Enabled:   &enabled,
			Transport: config.TransportHTTP,
			URL:       "https://example.com/mcp",
			Headers:   map[string]string{"X-API-Key": "prefix-${AL_TOKEN}"},
		}}},
	}}
	results := CheckPolicy(project)
	require.Len(t, results, 1)
	require.Equal(t, CodePolicyCodexHeaderForm, results[0].Code)
}

func TestCheckPolicy_YOLOModeNoWarning(t *testing.T) {
	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeYOLO},
		},
	}

	results := CheckPolicy(project)
	require.Nil(t, results, "YOLO mode should not produce policy warnings")
}

func TestCheckPolicy_AgentSpecificOverrideWarnings(t *testing.T) {
	root := t.TempDir()
	absRoot, err := filepath.Abs(root)
	require.NoError(t, err)

	project := &config.ProjectConfig{
		Root: root,
		Config: config.Config{
			Agents: config.AgentsConfig{
				Codex: config.CodexConfig{
					AgentSpecific: map[string]any{
						"approval_policy": "never",
						"features": map[string]any{
							"multi_agent": true,
						},
						"projects": map[string]any{
							absRoot: map[string]any{
								"trust_level": "trusted",
							},
						},
					},
				},
				Claude: config.ClaudeConfig{
					AgentSpecific: map[string]any{
						"permissions": map[string]any{
							"allow": []string{"Bash(ls:*)"},
						},
					},
				},
			},
		},
	}

	results := CheckPolicy(project)
	require.Len(t, results, 2)
	require.Equal(t, CodePolicyAgentSpecificOverrides, results[0].Code)
	require.Equal(t, "agents.codex.agent_specific", results[0].Subject)
	require.Equal(t, []string{"overridden keys: approval_policy, projects"}, results[0].Details)
	require.Equal(t, CodePolicyAgentSpecificOverrides, results[1].Code)
	require.Equal(t, "agents.claude.agent_specific", results[1].Subject)
	require.Equal(t, []string{"overridden keys: permissions.allow"}, results[1].Details)
}

func TestCheckPolicy_CodexAgentSpecificProjectsDifferentRootDoesNotWarn(t *testing.T) {
	root := t.TempDir()
	otherRoot, err := filepath.Abs(t.TempDir())
	require.NoError(t, err)

	project := &config.ProjectConfig{
		Root: root,
		Config: config.Config{
			Agents: config.AgentsConfig{
				Codex: config.CodexConfig{
					AgentSpecific: map[string]any{
						"projects": map[string]any{
							otherRoot: map[string]any{
								"trust_level": "trusted",
							},
						},
					},
				},
			},
		},
	}

	require.Nil(t, CheckPolicy(project))
}

func TestCheckPolicy_CodexAgentSpecificProjectsNonMapWarns(t *testing.T) {
	project := &config.ProjectConfig{
		Root: t.TempDir(),
		Config: config.Config{
			Agents: config.AgentsConfig{
				Codex: config.CodexConfig{
					AgentSpecific: map[string]any{
						"projects": "trusted",
					},
				},
			},
		},
	}

	results := CheckPolicy(project)
	require.Len(t, results, 1)
	require.Equal(t, CodePolicyAgentSpecificOverrides, results[0].Code)
	require.Equal(t, []string{"overridden keys: projects"}, results[0].Details)
}

func TestCheckPolicy_ClaudeAgentSpecificPermissionsDenyDoesNotWarn(t *testing.T) {
	project := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				Claude: config.ClaudeConfig{
					AgentSpecific: map[string]any{
						"permissions": map[string]any{
							"deny": []string{"AskUserQuestion"},
						},
					},
				},
			},
		},
	}

	require.Nil(t, CheckPolicy(project))
}

func TestCheckPolicy_ClaudeAgentSpecificPermissionsAllowWarns(t *testing.T) {
	project := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				Claude: config.ClaudeConfig{
					AgentSpecific: map[string]any{
						"permissions": map[string]any{
							"allow": []string{"Bash(ls:*)"},
						},
					},
				},
			},
		},
	}

	results := CheckPolicy(project)
	require.Len(t, results, 1)
	require.Equal(t, CodePolicyAgentSpecificOverrides, results[0].Code)
	require.Equal(t, "agents.claude.agent_specific", results[0].Subject)
	require.Equal(t, []string{"overridden keys: permissions.allow"}, results[0].Details)
}

func TestCheckPolicy_AntigravityAgentSpecificPermissionsAllowWarns(t *testing.T) {
	project := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				Antigravity: config.AntigravityConfig{
					AgentSpecific: map[string]any{
						"permissions": map[string]any{
							"allow": []string{"command(rm:*)"},
						},
					},
				},
			},
		},
	}

	results := CheckPolicy(project)
	require.Len(t, results, 1)
	require.Equal(t, CodePolicyAgentSpecificOverrides, results[0].Code)
	require.Equal(t, "agents.antigravity.agent_specific", results[0].Subject)
	require.Equal(t, []string{"overridden keys: permissions.allow"}, results[0].Details)
}

func TestCheckPolicy_AntigravityAgentSpecificPermissionsDenyDoesNotWarn(t *testing.T) {
	project := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				Antigravity: config.AntigravityConfig{
					AgentSpecific: map[string]any{
						"permissions": map[string]any{
							"deny": []string{"command(rm:*)"},
						},
					},
				},
			},
		},
	}

	require.Nil(t, CheckPolicy(project))
}

func TestCheckPolicy_AntigravityAgentSpecificPermissionsNonMapWarns(t *testing.T) {
	project := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				Antigravity: config.AntigravityConfig{
					AgentSpecific: map[string]any{
						"permissions": []string{"command(rm:*)"},
					},
				},
			},
		},
	}

	results := CheckPolicy(project)
	require.Len(t, results, 1)
	require.Equal(t, CodePolicyAgentSpecificOverrides, results[0].Code)
	require.Equal(t, "agents.antigravity.agent_specific", results[0].Subject)
	require.Equal(t, []string{"overridden keys: permissions"}, results[0].Details)
}

func TestCheckPolicy_ClaudeAgentSpecificMixedPermissionsWarnsOnlyForAllow(t *testing.T) {
	project := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				Claude: config.ClaudeConfig{
					AgentSpecific: map[string]any{
						"permissions": map[string]any{
							"allow": []string{"Bash(ls:*)"},
							"deny":  []string{"AskUserQuestion"},
						},
					},
				},
			},
		},
	}

	results := CheckPolicy(project)
	require.Len(t, results, 1)
	require.Equal(t, CodePolicyAgentSpecificOverrides, results[0].Code)
	require.Equal(t, "agents.claude.agent_specific", results[0].Subject)
	require.Equal(t, []string{"overridden keys: permissions.allow"}, results[0].Details)
}

func TestCheckPolicy_ClaudeAgentSpecificNonMapPermissionsWarns(t *testing.T) {
	project := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				Claude: config.ClaudeConfig{
					AgentSpecific: map[string]any{
						"permissions": []string{"Bash(ls:*)"},
					},
				},
			},
		},
	}

	results := CheckPolicy(project)
	require.Len(t, results, 1)
	require.Equal(t, CodePolicyAgentSpecificOverrides, results[0].Code)
	require.Equal(t, "agents.claude.agent_specific", results[0].Subject)
	require.Equal(t, []string{"overridden keys: permissions"}, results[0].Details)
}

func TestCheckPolicy_ClaudeAgentSpecificEffortAndAllowCombinedIntoOneWarning(t *testing.T) {
	project := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				Claude: config.ClaudeConfig{
					AgentSpecific: map[string]any{
						"effortLevel": "low",
						"permissions": map[string]any{
							"allow": []string{"Bash(ls:*)"},
							"deny":  []string{"AskUserQuestion"},
						},
					},
				},
			},
		},
	}

	results := CheckPolicy(project)
	require.Len(t, results, 1)
	require.Equal(t, CodePolicyAgentSpecificOverrides, results[0].Code)
	require.Equal(t, "agents.claude.agent_specific", results[0].Subject)
	require.Equal(t, []string{"overridden keys: effortLevel, permissions.allow"}, results[0].Details)
}

func TestCheckPolicy_ClaudeReasoningEffortUnknown(t *testing.T) {
	for _, tc := range []struct {
		effort string
		want   bool
	}{
		{"xhigh", false}, {"max", false}, {"", false}, {"made-up-level", true},
	} {
		t.Run(tc.effort, func(t *testing.T) {
			project := &config.ProjectConfig{Config: config.Config{Agents: config.AgentsConfig{
				Claude: config.ClaudeConfig{Enabled: testutil.BoolPtr(true), Model: "opus", ReasoningEffort: tc.effort},
			}}}
			results := CheckPolicy(project)
			if !tc.want {
				require.Nil(t, results)
				return
			}
			require.Len(t, results, 1)
			require.Equal(t, CodePolicyClaudeReasoningUnknown, results[0].Code)
			require.Contains(t, results[0].Message, tc.effort)
		})
	}
}

func TestCheckPolicy_NilAndDisabledServer(t *testing.T) {
	require.Nil(t, CheckPolicy(nil))

	enabled := false
	project := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				Codex: config.CodexConfig{Enabled: testutil.BoolPtr(true)},
			},
			MCP: config.MCPConfig{
				Servers: []config.MCPServer{{
					ID:      "disabled",
					Enabled: &enabled,
					URL:     "https://example.com/mcp?api_key=secret",
					Headers: map[string]string{"Authorization": "Token ${AL_TOKEN}"},
				}},
			},
		},
	}
	require.Nil(t, CheckPolicy(project))
}

func TestCheckPolicy_CodexHeadersAllowedForms(t *testing.T) {
	project := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				Codex: config.CodexConfig{Enabled: testutil.BoolPtr(true)},
			},
			MCP: config.MCPConfig{
				Servers: []config.MCPServer{{
					ID:      "srv",
					Enabled: testutil.BoolPtr(true),
					URL:     "https://example.com/mcp",
					Headers: map[string]string{ //nolint:gosec // test data with placeholder syntax
						"Authorization": "Bearer ${AL_TOKEN}",
						"X-Token":       "${AL_TOKEN}",
					},
				}},
			},
		},
	}
	require.Nil(t, CheckPolicy(project))
}

func TestCheckPolicy_CodexHeaderSkippedWhenCodexNotTargetedOrDisabled(t *testing.T) {
	project := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				Codex: config.CodexConfig{Enabled: testutil.BoolPtr(false)},
			},
			MCP: config.MCPConfig{
				Servers: []config.MCPServer{{
					ID:      "srv",
					Enabled: testutil.BoolPtr(true),
					Clients: []string{"antigravity"},
					URL:     "https://example.com/mcp",
					Headers: map[string]string{
						"Authorization": "Token ${AL_TOKEN}",
					},
				}},
			},
		},
	}
	require.Nil(t, CheckPolicy(project))
}

func TestCheckPolicy_SecretURLIgnoresPlaceholderAndEmptyValues(t *testing.T) {
	project := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				Codex: config.CodexConfig{Enabled: testutil.BoolPtr(true)},
			},
			MCP: config.MCPConfig{
				Servers: []config.MCPServer{{
					ID:      "srv1",
					Enabled: testutil.BoolPtr(true),
					URL:     "https://example.com/mcp?api_key=${AL_TOKEN}",
				}, {
					ID:      "srv2",
					Enabled: testutil.BoolPtr(true),
					URL:     "https://example.com/mcp?api_key=",
				}, {
					ID:      "srv3",
					Enabled: testutil.BoolPtr(true),
					URL:     "://not-valid-url",
				}},
			},
		},
	}
	require.Nil(t, CheckPolicy(project))
}
