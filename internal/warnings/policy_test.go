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
	require.Equal(t, Warning{
		Code:     CodePolicyAgentSpecificOverrides,
		Subject:  "agents.antigravity.agent_specific",
		Message:  "agent-specific antigravity config overrides Agent Layer-managed keys",
		Fix:      "Remove the override if you want Agent Layer to manage those keys, or keep it to take full control.",
		Details:  []string{"overridden keys: permissions.allow"},
		Source:   SourceInternal,
		Severity: SeverityWarning,
	}, results[0])
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

func TestCheckPolicy_SecretURLWithPlaceholderHostOrUserinfo(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want bool
	}{
		{name: "literal query secret with placeholder host", url: "https://${AL_MCP_HOST}/mcp?api_key=sk-live", want: true},
		{name: "literal password with placeholder host", url: "https://user:literal@${AL_HOST}/mcp", want: true},
		{name: "literal username only with placeholder host", url: "https://token@${AL_HOST}/mcp", want: true},
		{name: "literal query secret beside placeholder password", url: "https://user:${AL_P}@example.com/mcp?token=literal", want: true},
		{name: "query secret partly literal", url: "https://example.com/mcp?token=abc${AL_X}", want: true},
		{name: "placeholder host and query secret", url: "https://${AL_HOST}/mcp?api_key=${AL_K}", want: false},
		{name: "placeholder userinfo and host", url: "https://${AL_U}:${AL_P}@${AL_HOST}/mcp", want: false},
		{name: "literal username with placeholder password", url: "https://user:${AL_P}@example.com/mcp", want: false},
		{name: "at sign after the authority", url: "https://example.com/mcp?q=a@b", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			project := &config.ProjectConfig{
				Config: config.Config{
					MCP: config.MCPConfig{
						Servers: []config.MCPServer{{
							ID:      "srv",
							Enabled: testutil.BoolPtr(true),
							URL:     tt.url,
						}},
					},
				},
			}
			results := CheckPolicy(project)
			if !tt.want {
				require.Nil(t, results)
				return
			}
			require.Len(t, results, 1)
			require.Equal(t, CodePolicySecretInURL, results[0].Code)
			require.Equal(t, SeverityCritical, results[0].Severity)
		})
	}
}

func TestCheckPolicy_SecretURLDelimiterBoundaries(t *testing.T) {
	const userinfoDetail = "URL contains inline userinfo credentials"
	tests := []struct {
		name       string
		url        string
		wantDetail string
	}{
		{name: "query marker in fragment", url: "https://example.com/mcp#docs?token=literal"},
		{name: "review fragment example", url: "https://example.com/mcp#docs?token=example"},
		{name: "fragment after safe query", url: "https://example.com/mcp?depth=1#docs?token=literal"},
		{name: "literal query before fragment", url: "https://example.com/mcp?token=literal#docs?api_key=other", wantDetail: `query parameter "token" contains a literal secret-like value`},
		{name: "scheme relative password", url: "//user:secret@example.com/mcp", wantDetail: userinfoDetail},
		{name: "scheme relative password only", url: "//:secret@example.com/mcp", wantDetail: userinfoDetail},
		{name: "scheme relative username only", url: "//user@example.com/mcp", wantDetail: userinfoDetail},
		{name: "scheme relative empty password", url: "//user:@example.com/mcp", wantDetail: userinfoDetail},
		{name: "scheme relative placeholder host", url: "//user:secret@${AL_HOST}/mcp", wantDetail: userinfoDetail},
		{name: "scheme relative partly literal password", url: "//user:abc${AL_P}@example.com/mcp", wantDetail: userinfoDetail},
		{name: "scheme relative placeholder password", url: "//user:${AL_P}@example.com/mcp"},
		{name: "scheme relative placeholder userinfo and host", url: "//${AL_U}:${AL_P}@${AL_HOST}/mcp"},
		{name: "scheme relative placeholder username", url: "//${AL_U}@example.com/mcp"},
		{name: "scheme relative empty userinfo", url: "//:@example.com/mcp"},
		{name: "at sign in scheme relative path", url: "//example.com/user:secret@other"},
		{name: "at sign in scheme relative query", url: "//example.com?user:secret@other"},
		{name: "at sign in scheme relative fragment", url: "//example.com#user:secret@other"},
		{name: "scheme delimiter in scheme relative path", url: "//example.com/https://user:secret@other"}, // #nosec G101 -- invented credentials test that the path is not scanned as userinfo.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			project := &config.ProjectConfig{
				Config: config.Config{
					MCP: config.MCPConfig{
						Servers: []config.MCPServer{{
							ID:      "srv",
							Enabled: testutil.BoolPtr(true),
							URL:     tt.url,
						}},
					},
				},
			}
			results := CheckPolicy(project)
			if tt.wantDetail == "" {
				require.Nil(t, results)
				return
			}
			require.Len(t, results, 1)
			require.Equal(t, CodePolicySecretInURL, results[0].Code)
			require.Equal(t, SeverityCritical, results[0].Severity)
			require.Equal(t, []string{tt.wantDetail}, results[0].Details)
		})
	}
}
