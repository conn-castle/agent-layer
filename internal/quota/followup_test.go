package quota

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/config"
)

func TestQuotaCodexLockoutWireReasons(t *testing.T) {
	source := subscriptionsFixture(t)[0]
	fixture := readFixture(t, "codex-lockout")
	for _, tc := range []struct {
		kind  string
		clear int64
	}{
		{"rate_limit_reached", 1795716000},
		{"workspace_owner_credits_depleted", 0},
		{"workspace_member_credits_depleted", 0},
		{"workspace_owner_usage_limit_reached", 0},
		{"workspace_member_usage_limit_reached", 1795720000},
		{"future_enum_value", 1795716000},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			body := strings.Replace(fixture, "workspace_owner_credits_depleted", tc.kind, 1)
			if tc.kind == "workspace_member_usage_limit_reached" {
				body = strings.Replace(body, `"plan_type": "plus",`, `"plan_type": "plus", "spend_control":{"reached":true,"individual_limit":{"reset_at":1795720000}},`, 1)
			}
			h := fixtureHTTP(func(*http.Request) (*http.Response, error) { return response(http.StatusOK, body) })
			out := query(t.Context(), Request{Sources: []Source{source}}, h)
			require.Equal(t, ResponseComplete, out.Status)
			result := out.Results[0]
			assert.Equal(t, ptr(true), result.Blocked)
			assert.Equal(t, unixSeconds(tc.clear), result.ClearsAt)
			if tc.kind == "future_enum_value" {
				assert.Empty(t, result.BlockReason)
			} else {
				assert.Equal(t, tc.kind, result.BlockReason)
			}
			assert.Equal(t, unixSeconds(1795716000), result.Pools[0].ClearsAt)
			assert.Equal(t, "codex_special", result.Pools[1].ID)
			assert.Equal(t, "Special model", result.Pools[1].Label)
			assert.Equal(t, result.ClearsAt, findModel(t, result, "special-model").ClearsAt)
		})
	}
}

func TestQuotaCodexWorkspaceLockoutIndependentOfAllowedWindow(t *testing.T) {
	source := subscriptionsFixture(t)[0]
	body := `{"account_id":"workspace-fixture","rate_limit":{"allowed":true,"primary_window":{"used_percent":20,"reset_at":1795701600}},"rate_limit_reached_type":{"type":"workspace_member_credits_depleted"}}`
	h := fixtureHTTP(func(*http.Request) (*http.Response, error) { return response(http.StatusOK, body) })
	out := query(t.Context(), Request{Sources: []Source{source}}, h)
	require.Equal(t, ResponseComplete, out.Status)
	assert.Equal(t, ptr(false), out.Results[0].Pools[0].Blocked)
	assert.Equal(t, ptr(true), out.Results[0].Blocked)
	assert.Nil(t, out.Results[0].ClearsAt)
}

func TestQuotaCodexNoAdditionalLimits(t *testing.T) {
	source := subscriptionsFixture(t)[0]
	source.Models = []string{"gpt-6.2-sol"}
	for _, tc := range []struct {
		name, field string
		blocked     *bool
	}{
		{"null", `,"additional_rate_limits":null`, ptr(false)},
		{"empty list", `,"additional_rate_limits":[]`, ptr(false)},
		{"omitted", "", nil},
		{"unbound limit", `,"additional_rate_limits":[{"limit_id":"unknown","rate_limit":{"allowed":false}}]`, nil},
		{"missing scoped reading", `,"additional_rate_limits":[{"normal_model_slug":"gpt-6.2-sol","rate_limit":null}]`, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := `{"account_id":"workspace-fixture","rate_limit":{"allowed":true,"limit_reached":false,"primary_window":{"used_percent":70,"limit_window_seconds":604800}},"model_usage":{"gpt-6.2-sol":{},"gpt-6-astra":{"available":true}}` + tc.field + "}"
			h := fixtureHTTP(func(*http.Request) (*http.Response, error) { return response(http.StatusOK, body) })
			out := query(t.Context(), Request{Sources: []Source{source}}, h)
			require.Equal(t, ResponseComplete, out.Status)
			assert.Equal(t, tc.blocked, findModel(t, out.Results[0], "gpt-6.2-sol").Blocked)
			assert.Equal(t, ptr(false), findModel(t, out.Results[0], "gpt-6-astra").Blocked)
		})
	}
}

func TestQuotaAntigravityGeminiGroupsAndDisplayIDs(t *testing.T) {
	source := subscriptionsFixture(t)[3]
	source.Models = []string{"gemini-4-neon-high", "gemini-4-neon-xhigh", "gemini-4-hydrogen-xhigh", "Gemini 4 Neon (High)"}
	routes := allFixtureRoutes(t)
	h := fixtureHTTP(func(req *http.Request) (*http.Response, error) {
		if strings.HasPrefix(req.Header.Get(userAgentHeader), "antigravity/cli/") {
			switch req.URL.String() {
			case agyBase + "retrieveUserQuotaSummary":
				return response(http.StatusOK, `{"groups":[{"displayName":"Gemini Models","description":"Models within this group: Gemini 4 Neon, Gemini 4 Hydrogen","buckets":[
					{"bucketId":"gemini-weekly","window":"weekly","remainingFraction":0.837,"resetTime":"2026-12-03T12:00:00Z"},
					{"bucketId":"gemini-5h","window":"5h","remainingFraction":0.935,"resetTime":"2026-11-26T16:00:00Z"}
				]}]}`)
			case agyBase + "fetchAvailableModels":
				return response(http.StatusOK, `{"models":{
					"gemini-4-neon-high":{"displayName":"Gemini 4 Neon (High)"},
					"gemini-4-hydrogen-xhigh":{"displayName":"Gemini 4 Hydrogen (XHigh)"}
				},"groupedModels":{"gemini-4-neon":{"displayName":"Gemini 4 Neon","supportedThinkingLevels":[{"thinkingSuffix":"xhigh"}]}}}`)
			}
		}
		return response(http.StatusOK, routes[req.URL.String()])
	})
	out := query(t.Context(), Request{Sources: []Source{source}}, h)
	require.Equal(t, ResponseComplete, out.Status)
	result := out.Results[0]
	pool := result.Pools[0]
	require.Equal(t, "Gemini Models", pool.ID)
	require.Len(t, pool.Windows, 2)
	assert.Equal(t, "gemini-weekly", pool.Windows[0].ID)
	assert.Equal(t, "gemini-5h", pool.Windows[1].ID)
	assert.InDelta(t, 16.3, *pool.Windows[0].UsedPercent, 0.0001)
	assert.InDelta(t, 6.5, *pool.Windows[1].UsedPercent, 0.0001)
	for _, id := range source.Models {
		m := findModel(t, result, id)
		assert.Equal(t, coverageComplete, m.Coverage, id)
		assert.Equal(t, []string{pool.ID}, m.Pools, id)
		assert.Equal(t, ptr(false), m.Blocked, id)
	}
}

func TestQuotaAntigravityWorkspaceModelCounter(t *testing.T) {
	var summary agySummary
	require.NoError(t, json.Unmarshal([]byte(`{"groups":[{"displayName":"All Models","buckets":[
		{"bucketId":"gemini-4-hydrogen-xhigh","displayName":"Gemini 4 Hydrogen (XHigh)","remainingFraction":0.875}
	]}]}`), &summary))
	var catalog agyCatalog
	require.NoError(t, json.Unmarshal([]byte(`{"models":{
		"gemini-4-hydrogen-xhigh":{"displayName":"Gemini 4 Hydrogen (XHigh)"},
		"gemini-4-neon-disabled":{}
	}}`), &catalog))
	r := Result{Agent: agentAntigravity}
	normalizeAntigravity(summary, agyLegacy{}, catalog, agyCredits{}, credential{}, &r)
	r.evaluateModels([]string{"Gemini 4 Hydrogen (XHigh)", "gemini-4-neon-high"})
	require.Len(t, r.Pools, 1)
	assert.Equal(t, "gemini-4-hydrogen-xhigh", r.Pools[0].ID)
	assert.Nil(t, r.Pools[0].Windows[0].DurationSeconds)
	m := findModel(t, r, "Gemini 4 Hydrogen (XHigh)")
	assert.Equal(t, coverageComplete, m.Coverage)
	assert.Equal(t, []string{"gemini-4-hydrogen-xhigh"}, m.Pools)
	assert.Equal(t, ptr(false), m.Blocked)
	assert.Nil(t, findModel(t, r, "gemini-4-neon-high").Blocked)
}

func TestQuotaAntigravityFlatSummaryAndGroupedWindows(t *testing.T) {
	source := subscriptionsFixture(t)[3]
	routes := allFixtureRoutes(t)
	routes[agyBase+"retrieveUserQuotaSummary"] = readFixture(t, "antigravity-flat-summary")
	h := fixtureHTTP(func(req *http.Request) (*http.Response, error) {
		return response(http.StatusOK, routes[req.URL.String()])
	})
	out := query(t.Context(), Request{Sources: []Source{source}}, h)
	require.Equal(t, ResponseComplete, out.Status)
	result := out.Results[0]
	require.Len(t, result.Pools, 7)
	group := result.Pools[0]
	require.Len(t, group.Windows, 2)
	assert.Equal(t, "Claude and GPT models", group.ID)
	assert.Equal(t, windowPending, group.Windows[1].State)
	assert.Equal(t, ptr(false), group.Windows[1].Disabled)
	assert.Nil(t, group.Windows[1].ResetsAt)
	flat := result.Pools[1]
	assert.Equal(t, "flat-weekly", flat.ID)
	assert.Equal(t, scopeUnknown, flat.Scope)
	assert.Equal(t, ptr(true), flat.Windows[0].Disabled)
	assert.Equal(t, ptr(false), flat.Windows[0].Blocked)
	assert.Equal(t, windowPending, result.Pools[2].Windows[0].State)
	assert.Equal(t, windowFixed, result.Pools[3].Windows[0].State)
	assert.Equal(t, windowFixed, result.Pools[4].Windows[0].State)
	assert.Nil(t, result.Pools[4].Windows[0].ResetsAt)
	flash := findModel(t, result, "gemini-flash")
	assert.Equal(t, []string{"gemini-flash"}, flash.Pools)
	assert.Nil(t, flash.Blocked)
}

func TestQuotaAntigravityPendingAndExplicitAdmission(t *testing.T) {
	var bucket agyQuota
	require.NoError(t, json.Unmarshal([]byte(`{"window":"5h","hasStarted":true,"remainingFraction":1,"blocked":true,"lockoutClearTime":"2026-12-03T12:00:00Z"}`), &bucket))
	assert.False(t, agySummaryWindowPending(bucket))
	w := agyWindow("test", bucket, hours5, windowFixed, ptr(true))
	assert.Equal(t, ptr(true), w.Blocked)
	assert.Equal(t, parseTime("2026-12-03T12:00:00Z"), w.ClearsAt)
	bucket.Started = ptr(false)
	assert.True(t, agySummaryWindowPending(bucket))
}

func TestQuotaAntigravityExhaustionWithAndWithoutCredits(t *testing.T) {
	reset := parseTime("2026-12-03T12:00:00Z")
	bucket := agyQuota{Remaining: ptr(0.0), Reset: json.RawMessage(`"2026-12-03T12:00:00Z"`)}
	withoutCredits := agyWindow("test", bucket, week, windowFixed, ptr(false))
	assert.Equal(t, knownBlocked(reset), withoutCredits.Admission)
	assert.Equal(t, knownAllowed(), agyWindow("test", bucket, week, windowFixed, ptr(true)).Admission)
	assert.Nil(t, agyWindow("test", bucket, week, windowFixed, nil).Blocked)
}

func TestQuotaClaudeRevocationAndUnknownRenewal(t *testing.T) {
	source := detachedFixture(t, t.TempDir(), agentClaude, agentClaude,
		`{"claudeAiOauth":{"accessToken":"fixture-token","refreshToken":"fixture-refresh"}}`)
	h := fixtureHTTP(func(*http.Request) (*http.Response, error) {
		return response(http.StatusForbidden, `{"error":{"type":"permission_error","message":"OAuth token has been revoked. private text"}}`)
	})
	out := query(t.Context(), Request{Sources: []Source{source}}, h)
	assert.Equal(t, "revoked_credentials", out.Results[0].Reason)
	assert.Equal(t, ptr(true), out.Results[0].Reauth)
	serialized, err := json.Marshal(out)
	require.NoError(t, err)
	assert.NotContains(t, string(serialized), "private text")

	h = fixtureHTTP(func(*http.Request) (*http.Response, error) {
		return response(http.StatusUnauthorized, `{}`)
	})
	out = query(t.Context(), Request{Sources: []Source{source}}, h)
	assert.Equal(t, credentialRejected, out.Results[0].Reason)
	assert.Nil(t, out.Results[0].Reauth)
}

func TestQuotaClaudeScopedLimitCoversAliases(t *testing.T) {
	var usage claudeUsageResponse
	require.NoError(t, json.Unmarshal([]byte(`{
		"seven_day_opus":{"utilization":100,"resets_at":"2026-12-03T12:00:00Z"},
		"extra_usage":{"is_enabled":false},
		"limits":[
			{"kind":"weekly_scoped","group":"weekly","percent":100,"resets_at":"2026-12-03T12:00:00Z","scope":{"model":{"id":"claude-opus-5-5"}}},
			{"kind":"weekly_scoped","group":"weekly","percent":100,"scope":{"model":{"id":"claude-fable-5-2"}}}
		]}`), &usage))
	result := Result{Agent: agentClaude}
	normalizeClaude(usage, &result)
	result.evaluateModels([]string{"opus[1m]", "fable", "claude-fable-5-2[1m]", "sonnet"})
	require.Len(t, result.Pools, 3)
	for _, id := range []string{"opus[1m]", "fable", "claude-fable-5-2[1m]"} {
		assert.Equal(t, ptr(true), findModel(t, result, id).Blocked, id)
	}
	assert.Nil(t, findModel(t, result, "sonnet").Blocked)

	second := usage.Limits[0]
	second.Scope.Model.ID, second.Percent = "claude-opus-5-3", ptr(0.0)
	usage.Limits = append(usage.Limits, second)
	result = Result{Agent: agentClaude}
	normalizeClaude(usage, &result)
	result.evaluateModels([]string{"claude-opus-5-5", "claude-opus-5-3", "opus"})
	require.Len(t, result.Pools, 4)
	assert.Equal(t, ptr(true), findModel(t, result, "claude-opus-5-5").Blocked)
	assert.Nil(t, findModel(t, result, "claude-opus-5-3").Blocked)
	assert.Nil(t, findModel(t, result, "opus").Blocked)
	assert.Equal(t, coveragePartial, findModel(t, result, "opus").Coverage)
}

func TestQuotaClaudeWeeklyOnlyAdmission(t *testing.T) {
	const fields = `"seven_day":{"utilization":47,"resets_at":"2026-12-03T12:00:00Z"},
		"seven_day_breakdown":{"as_of":"2026-11-26T12:00:00Z","rows":[]},
		"limits":[{"kind":"weekly_scoped","group":"weekly","percent":41,
			"scope":{"model":{"display_name":"Fable"}}}]`
	for _, tc := range []struct {
		name, window string
		count        int
		blocked      *bool
	}{
		{"inactive short window", `,"five_hour":null`, 1, ptr(false)},
		{"missing short window", "", 2, nil},
		{"unreported short utilization", `,"five_hour":{}`, 2, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var usage claudeUsageResponse
			require.NoError(t, json.Unmarshal([]byte("{"+fields+tc.window+"}"), &usage))
			result := Result{Agent: agentClaude}
			normalizeClaude(usage, &result)
			result.evaluateModels([]string{"opus", "fable"})
			require.Len(t, result.Pools, 2)
			assert.Equal(t, tc.blocked, result.Blocked)
			assert.Equal(t, tc.blocked, result.Pools[0].Blocked)
			assert.Len(t, result.Pools[0].Windows, tc.count)
			assert.Equal(t, tc.blocked, findModel(t, result, "opus").Blocked)
			assert.Equal(t, tc.blocked, findModel(t, result, "fable").Blocked)
		})
	}
}

func TestQuotaCredentialRetryDiscardsFirstAttemptMetadata(t *testing.T) {
	source := subscriptionsFixture(t)[3]
	r := Result{Agent: agentAntigravity, AttemptedAt: fixedTime()}
	h := fixtureHTTP(nil)
	observed := fixedTime().Add(time.Second)
	read := func(_ context.Context, _ *httpReader, auth credential, result *Result) failure {
		if auth.token == fakeToken {
			result.ObservedAt = ptr(fixedTime())
			result.Missing = []string{"quota_summary"}
			updated := source.File + ".new"
			writeFixture(t, updated, `{"auth_method":"consumer","token":{"access_token":"new-fixture"}}`)
			require.NoError(t, os.Rename(updated, source.File))
			fail := auth.authenticationFailure(credentialRejected, fixedTime())
			fail.remote = true
			return fail
		}
		retainRead(result, &observed, failure{}, "quota_summary")
		return failure{}
	}
	assert.Empty(t, readWithCredential(t.Context(), Request{}, source, read, h, &r).status)
	assert.Empty(t, r.Missing)
	assert.Equal(t, &observed, r.ObservedAt)
}

func TestQuotaMuseInactivePaymentSignal(t *testing.T) {
	source := subscriptionsFixture(t)[4]
	for _, tc := range []struct {
		name, body string
		blocked    *bool
	}{
		{"omitted", `{"is_subs_active":false}`, nil},
		{"not required", `{"is_subs_active":false,"require_payment":false}`, nil},
		{"required", `{"is_subs_active":false,"require_payment":true}`, ptr(true)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := fixtureHTTP(func(*http.Request) (*http.Response, error) { return response(http.StatusOK, tc.body) })
			out := query(t.Context(), Request{Sources: []Source{source}}, h)
			require.Len(t, out.Results, 1)
			assert.Equal(t, statusNonSubscription, out.Results[0].Status)
			assert.Equal(t, "subscription_inactive", out.Results[0].Reason)
			assert.Equal(t, tc.blocked, out.Results[0].Blocked)
		})
	}
}

func TestQuotaGrokBillingFailurePreservesOnlyKnownBlock(t *testing.T) {
	source := subscriptionsFixture(t)[2]
	for _, tc := range []struct {
		name, account string
		code          int
		blocked       *bool
	}{
		{"unavailable billing", grokAccountFixture, http.StatusServiceUnavailable, nil},
		{"missing billing", grokAccountFixture, http.StatusOK, nil},
		{"blocked account", `{"hasGrokCodeAccess":false}`, http.StatusServiceUnavailable, ptr(true)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := fixtureHTTP(func(req *http.Request) (*http.Response, error) {
				if strings.HasSuffix(req.URL.Path, "/billing") {
					return response(tc.code, "{}")
				}
				return response(http.StatusOK, tc.account)
			})
			out := query(t.Context(), Request{Sources: []Source{source}}, h)
			require.Equal(t, ResponseNoReadings, out.Status)
			assert.Equal(t, tc.blocked, out.Results[0].Blocked)
		})
	}
}

func TestQuotaGrokModelIncludesAccountAdmission(t *testing.T) {
	source := subscriptionsFixture(t)[2]
	source.Models = []string{"grok-4.7"}
	for _, tc := range []struct {
		name, account string
		code          int
		coverage      string
		blocked       *bool
	}{
		{"unavailable entitlement", "{}", http.StatusServiceUnavailable, coveragePartial, nil},
		{"omitted entitlement", "{}", http.StatusOK, coveragePartial, nil},
		{"unknown block reason", `{"hasGrokCodeAccess":true,"teamBlockedReasons":[{}]}`, http.StatusOK, coveragePartial, nil},
		{"zero block reason", `{"hasGrokCodeAccess":true,"userBlockedReason":0}`, http.StatusOK, coverageComplete, ptr(false)},
		{"numeric block reason", `{"hasGrokCodeAccess":true,"teamBlockedReasons":[0,2]}`, http.StatusOK, coverageComplete, ptr(true)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			billing := readFixture(t, agentGrok)
			h := fixtureHTTP(func(req *http.Request) (*http.Response, error) {
				if strings.HasSuffix(req.URL.Path, "/billing") {
					return response(http.StatusOK, billing)
				}
				return response(tc.code, tc.account)
			})
			out := query(t.Context(), Request{Sources: []Source{source}}, h)
			require.Equal(t, ResponseComplete, out.Status)
			model := findModel(t, out.Results[0], source.Models[0])
			assert.Equal(t, tc.coverage, model.Coverage)
			assert.Equal(t, tc.blocked, model.Blocked)
		})
	}
}

func TestQuotaClaudeAccountAliasRequiresPersonalOrganization(t *testing.T) {
	source := subscriptionsFixture(t)[1]
	routes := allFixtureRoutes(t)
	routes[claudeOAuth+"profile"] = `{"account":{"uuid":"a1","email":"claude@example.test"},"organization":{"uuid":"team1","organization_type":"claude_team"}}`
	h := fixtureHTTP(func(req *http.Request) (*http.Response, error) {
		return response(http.StatusOK, routes[req.URL.String()])
	})
	for _, alias := range []string{"a1", "claude@example.test", "team1/a1"} {
		source.Expected = alias
		out := query(t.Context(), Request{Sources: []Source{source}}, h)
		if alias == "team1/a1" {
			assert.Equal(t, "matched", out.Results[0].AccountCheck)
		} else {
			assert.Equal(t, statusMismatch, out.Results[0].Status)
		}
	}
}

func TestQuotaClaudeSubdirectoryUsesProjectSettings(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	writeFixture(t, filepath.Join(home, claudeCredentials), `{"claudeAiOauth":{"accessToken":"fixture-token"}}`)
	req := Request{
		Project: &config.ProjectConfig{Root: root},
		WorkDir: filepath.Join(root, "subdir"),
		Env:     []string{claudeConfigEnv + "=" + home},
	}
	source := Source{Agent: agentClaude}
	writeFixture(t, filepath.Join(root, ".claude", "settings.json"), `{"env":{"ANTHROPIC_API_KEY":"fixture-api"}}`)
	_, fail := loadCredential(t.Context(), req, source)
	assert.Equal(t, statusNonSubscription, fail.status)

	req.Project.Config.Agents.Claude.AgentSpecific = map[string]any{
		"env": map[string]any{"ANTHROPIC_BASE_URL": "https://fixture.example"},
	}
	_, fail = loadCredential(t.Context(), req, source)
	assert.Equal(t, reasonCustomEndpoint, fail.reason)

	writeFixture(t, filepath.Join(root, ".claude", "settings.local.json"), `{"env":{"ANTHROPIC_BASE_URL":"","ANTHROPIC_API_KEY":"","CLAUDE_CONFIG_DIR":"other"}}`)
	_, fail = loadCredential(t.Context(), req, source)
	assert.Equal(t, reasonCustomAuth, fail.reason)
}

func TestQuotaCodexGlobalHomeNativeProjectAndLoginPrecedence(t *testing.T) {
	// Codex writes trust keys using the canonical project path. macOS temp
	// directories are commonly reached through the /var symlink.
	project, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	home := t.TempDir()
	req := Request{Project: &config.ProjectConfig{Root: project}, WorkDir: project}
	source := Source{Agent: agentCodex}
	userConfig := filepath.Join(home, nativeConfig)
	projectConfig := filepath.Join(project, ".codex", nativeConfig)
	idToken := "a." + base64.RawURLEncoding.EncodeToString([]byte(`{"https://api.openai.com/auth":{"chatgpt_account_id":"launch-workspace"}}`)) + ".b"
	writeFixture(t, filepath.Join(home, authFilename), fmt.Sprintf(`{"auth_mode":"chatgpt","tokens":{"id_token":%q,"access_token":"fixture-token","account_id":"launch-workspace"}}`, idToken))
	for _, tc := range []struct {
		name, homeSettings, projectSettings, trust, env, reason string
	}{
		{"trusted project workspace allows global login", "", `forced_chatgpt_workspace_id = ["another", "launch-workspace"]`, "trusted", "", ""},
		{"trusted project disallows global workspace", "", `forced_chatgpt_workspace_id = "another"`, "trusted", "", "credential_workspace_mismatch"},
		{"trusted project forces ChatGPT against ambient key", "", `forced_login_method = "chatgpt"`, "trusted", "CODEX_API_KEY=fixture-api", "codex_login_method_conflict"},
		{"trusted project forces API", "", `forced_login_method = "api"`, "trusted", "", reasonAPIKey},
		{"untrusted project restriction ignored", "", `forced_login_method = "chatgpt"`, "untrusted", "CODEX_API_KEY=fixture-api", reasonAPIKey},
		{"native denies repo routing overrides", "", `model_provider = "repo-defined"`, "trusted", "", ""},
		{"selected home custom model provider", `model_provider = "private"`, "", "trusted", "", "custom_model_provider"},
		{"unknown selected profile", `profile = "missing"`, "", "trusted", "", reasonCustomAuth},
		{"external access token", "", "", "trusted", "CODEX_ACCESS_TOKEN=fixture-external", reasonCustomAuth},
		{"blank ambient key", "", "", "trusted", "CODEX_API_KEY= ", ""},
		{"required vault does not fall through to an auth file", "cli_auth_credentials_store = \"keyring\"\n[features]\nsecret_auth_storage = true", "", "trusted", "", reasonMissingCredential},
		{"automatic store falls back when vault is absent", "cli_auth_credentials_store = \"auto\"\n[features]\nsecret_auth_storage = true", "", "trusted", "", ""},
		{"vault feature must be a boolean", "cli_auth_credentials_store = \"auto\"\n[features]\nsecret_auth_storage = \"true\"", "", "trusted", "", reasonCustomAuth},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writeFixture(t, userConfig, tc.homeSettings+fmt.Sprintf("\n[projects.%q]\ntrust_level = %q\n", project, tc.trust))
			writeFixture(t, projectConfig, tc.projectSettings)
			req.Env = []string{codexHomeEnv + "=" + home}
			if tc.env != "" {
				req.Env = append(req.Env, tc.env)
			}
			auth, fail := loadCredential(t.Context(), req, source)
			assert.Equal(t, tc.reason, fail.reason)
			if tc.reason == "" {
				assert.Equal(t, "launch-workspace", auth.account)
			} else {
				assert.Empty(t, auth.token)
			}
		})
	}
}
