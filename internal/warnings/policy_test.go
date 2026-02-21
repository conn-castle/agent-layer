package warnings

import (
	"fmt"
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
				Antigravity: config.EnableOnlyConfig{Enabled: &enabled},
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

func TestCheckPolicy_YOLOModeNoWarning(t *testing.T) {
	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: "yolo"},
		},
	}

	results := CheckPolicy(project)
	require.Nil(t, results, "YOLO mode should not produce policy warnings")
}

func TestCheckPolicy_NilAndDisabledServer(t *testing.T) {
	require.Nil(t, CheckPolicy(nil))

	enabled := false
	project := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				Codex: config.CodexConfig{Enabled: boolPtr(true)},
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
				Codex: config.CodexConfig{Enabled: boolPtr(true)},
			},
			MCP: config.MCPConfig{
				Servers: []config.MCPServer{{
					ID:      "srv",
					Enabled: boolPtr(true),
					URL:     "https://example.com/mcp",
					Headers: map[string]string{
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
				Codex: config.CodexConfig{Enabled: boolPtr(false)},
			},
			MCP: config.MCPConfig{
				Servers: []config.MCPServer{{
					ID:      "srv",
					Enabled: boolPtr(true),
					Clients: []string{"gemini"},
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
				Codex: config.CodexConfig{Enabled: boolPtr(true)},
			},
			MCP: config.MCPConfig{
				Servers: []config.MCPServer{{
					ID:      "srv1",
					Enabled: boolPtr(true),
					URL:     "https://example.com/mcp?api_key=${AL_TOKEN}",
				}, {
					ID:      "srv2",
					Enabled: boolPtr(true),
					URL:     "https://example.com/mcp?api_key=",
				}, {
					ID:      "srv3",
					Enabled: boolPtr(true),
					URL:     "://not-valid-url",
				}},
			},
		},
	}
	require.Nil(t, CheckPolicy(project))
}

func TestFindSecretInURL(t *testing.T) {
	t.Run("userinfo username", func(t *testing.T) {
		detail, ok := findSecretInURL("https://user@example.com/mcp")
		require.True(t, ok)
		require.Contains(t, detail, "userinfo")
	})

	t.Run("literal secret query", func(t *testing.T) {
		detail, ok := findSecretInURL("https://example.com/mcp?access_token=abc123456")
		require.True(t, ok)
		require.Contains(t, detail, "access_token")
	})

	t.Run("placeholder and blank", func(t *testing.T) {
		for _, raw := range []string{
			"https://example.com/mcp?token=${AL_TOKEN}",
			"https://example.com/mcp?token=",
			"",
		} {
			detail, ok := findSecretInURL(raw)
			require.False(t, ok, raw)
			require.Empty(t, detail)
		}
	})
}

func TestFindUnsupportedCodexHeaderForm(t *testing.T) {
	cases := []struct {
		name    string
		headers map[string]string
		want    bool
	}{
		{
			name: "literal",
			headers: map[string]string{
				"Authorization": "Bearer static-token",
			},
			want: false,
		},
		{
			name: "exact placeholder",
			headers: map[string]string{
				"X-Api-Key": "${AL_TOKEN}",
			},
			want: false,
		},
		{
			name: "bearer placeholder",
			headers: map[string]string{
				"Authorization": "Bearer ${AL_TOKEN}",
			},
			want: false,
		},
		{
			name: "unsupported authorization format",
			headers: map[string]string{
				"Authorization": "Token ${AL_TOKEN}",
			},
			want: true,
		},
		{
			name: "unsupported mixed placeholder",
			headers: map[string]string{
				"X-Api-Key": fmt.Sprintf("prefix-%s", "${AL_TOKEN}"),
			},
			want: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			detail, ok := findUnsupportedCodexHeaderForm(tc.headers)
			require.Equal(t, tc.want, ok)
			if tc.want {
				require.NotEmpty(t, detail)
			} else {
				require.Empty(t, detail)
			}
		})
	}
}

func TestPolicyHelpers(t *testing.T) {
	require.False(t, isEnabled(nil))
	require.True(t, isEnabled(boolPtr(true)))
	require.False(t, isEnabled(boolPtr(false)))

	require.True(t, isClientTargeted(nil, "codex"))
	require.True(t, isClientTargeted([]string{"codex"}, "codex"))
	require.False(t, isClientTargeted([]string{"claude"}, "codex"))

	require.False(t, isExplicitClientTargeted(nil, "codex"))
	require.True(t, isExplicitClientTargeted([]string{"codex"}, "codex"))
	require.False(t, isExplicitClientTargeted([]string{"claude"}, "codex"))

	require.True(t, looksLikeSecretQueryKey("api_key"))
	require.True(t, looksLikeSecretQueryKey("my-access_token-value"))
	require.False(t, looksLikeSecretQueryKey("page"))

	require.True(t, hasEnvPlaceholder("Bearer ${AL_TOKEN}"))
	require.False(t, hasEnvPlaceholder("Bearer literal"))

	require.True(t, isLiteralHeaderValue("literal"))
	require.False(t, isLiteralHeaderValue("${AL_TOKEN}"))

	require.True(t, isExactEnvPlaceholder("${AL_TOKEN}"))
	require.False(t, isExactEnvPlaceholder("prefix-${AL_TOKEN}"))

	require.True(t, isBearerEnvPlaceholder("Bearer ${AL_TOKEN}"))
	require.True(t, isBearerEnvPlaceholder("bearer ${AL_TOKEN}"))
	require.False(t, isBearerEnvPlaceholder("Token ${AL_TOKEN}"))
}

func TestDedupePolicyWarningsAndAntigravityEnabled(t *testing.T) {
	items := []Warning{
		{
			Code:    CodePolicyCapabilityMismatch,
			Subject: "same",
			Message: "duplicate",
		},
		{
			Code:    CodePolicyCapabilityMismatch,
			Subject: "same",
			Message: "duplicate",
		},
		{
			Code:    CodePolicyCapabilityMismatch,
			Subject: "other",
			Message: "duplicate",
		},
	}
	out := dedupePolicyWarnings(items)
	require.Len(t, out, 2)
	require.Nil(t, dedupePolicyWarnings(nil))

	require.True(t, onlyAntigravityEnabled(config.AgentsConfig{
		Antigravity: config.EnableOnlyConfig{Enabled: boolPtr(true)},
	}))
	require.False(t, onlyAntigravityEnabled(config.AgentsConfig{
		Antigravity: config.EnableOnlyConfig{Enabled: boolPtr(true)},
		Codex:       config.CodexConfig{Enabled: boolPtr(true)},
	}))
}

func boolPtr(v bool) *bool {
	return &v
}
