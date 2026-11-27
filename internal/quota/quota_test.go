package quota

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/config"
)

const (
	fakeToken          = "fixture-token"
	grokAccountFixture = `{"hasGrokCodeAccess":true,"subscriptionTier":"test","userId":"u1","principalId":"seat1","teamId":"team1","userBlockedReason":null,"teamBlockedReasons":[]}`
)

type fixtureTransport func(*http.Request) (*http.Response, error)

func (f fixtureTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func fixedTime() time.Time { return time.Date(2026, time.November, 26, 12, 0, 0, 0, time.UTC) }

func fixtureHTTP(transport fixtureTransport) *httpReader {
	h := newHTTPReader()
	h.client.Transport = transport
	h.now = fixedTime
	return h
}

func response(status int, body string) (*http.Response, error) {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
}

func readFixture(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name+jsonExt)) // #nosec G304 -- named quota fixture.
	require.NoError(t, err)
	return string(raw)
}

func writeFixture(t *testing.T, target, body string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o700))
	require.NoError(t, os.WriteFile(target, []byte(body), 0o600))
}

func detachedFixture(t *testing.T, dir, label, agent, data string) Source {
	t.Helper()
	path := filepath.Join(dir, label+jsonExt)
	writeFixture(t, path, data)
	return Source{Selector: label, Agent: agent, File: path}
}

func subscriptionsFixture(t *testing.T) []Source {
	t.Helper()
	dir := t.TempDir()
	return []Source{
		detachedFixture(t, dir, agentCodex, agentCodex, codexCredentialFixture("workspace-fixture")),
		detachedFixture(t, dir, agentClaude, agentClaude, `{"claudeAiOauth":{"accessToken":"fixture-token","scopes":["user:profile"]}}`),
		detachedFixture(t, dir, agentGrok, agentGrok, `{"https://auth.x.ai::public-client":{"key":"fixture-token","auth_mode":"oidc"}}`),
		detachedFixture(t, dir, agentAntigravity, agentAntigravity, `{"auth_method":"consumer","token":{"access_token":"fixture-token"}}`),
		detachedFixture(t, dir, agentMuse, agentMuse, `{"providers":{"meta":{"mechanism":"oauth","access_token":"fixture-token","api_base_url":"https://api.meta.ai/v1"}}}`),
	}
}

func codexCredentialFixture(account string) string {
	return `{"auth_mode":"chatgpt","tokens":{"access_token":"fixture-token","account_id":"` + account + `"}}`
}

func allFixtureRoutes(t *testing.T) map[string]string {
	t.Helper()
	return map[string]string{
		codexUsage:                                      readFixture(t, agentCodex),
		claudeOAuth + "usage":                           readFixture(t, agentClaude),
		claudeOAuth + "profile":                         `{"account":{"uuid":"a1","email":"claude@example.test"},"organization":{"uuid":"org1","organization_type":"claude_max"}}`,
		grokBase + "/billing?format=credits":            readFixture(t, agentGrok),
		grokBase + "/user?include=subscription":         grokAccountFixture,
		agyBase + "retrieveUserQuotaSummary":            readFixture(t, "antigravity-summary"),
		agyBase + "retrieveUserQuota":                   readFixture(t, "antigravity-legacy"),
		agyBase + "fetchAvailableModels":                readFixture(t, "antigravity-models"),
		agyBase + "loadCodeAssist":                      `{"currentTier":{"id":"free-tier"}}`,
		"https://www.googleapis.com/oauth2/v2/userinfo": `{"id":"google-user","verified_email":false,"email":"google@example.test"}`,
		museBase + "/muse-code/key":                     readFixture(t, agentMuse),
	}
}

func TestQuotaProviderFixtures(t *testing.T) {
	sources := subscriptionsFixture(t)
	sources[0].Models = []string{"special-model", "standard-model"}
	sources[1].Models = []string{"claude-fable-5", "custom-model"}
	sources[1].Expected = "CLAUDE@example.test"
	sources[2].Expected = "team1/seat1"
	sources[3].Models = []string{"unlisted-model", "former-flash"}
	routes := allFixtureRoutes(t)
	h := fixtureHTTP(func(req *http.Request) (*http.Response, error) {
		assert.Equal(t, "Bearer "+fakeToken, req.Header.Get("Authorization"))
		body, ok := routes[req.URL.String()]
		assert.True(t, ok, "unexpected fixture route %s", req.URL)
		switch {
		case req.URL.String() == museBase+"/muse-code/key":
			raw, err := io.ReadAll(req.Body)
			assert.NoError(t, err)
			assert.Equal(t, http.MethodPost, req.Method)
			assert.JSONEq(t, `{"onboard":false}`, string(raw))
		case strings.HasPrefix(req.URL.String(), agyBase):
			assert.Equal(t, http.MethodPost, req.Method)
			assert.Equal(t, agyUserAgent, req.Header.Get(userAgentHeader))
		default:
			assert.Equal(t, http.MethodGet, req.Method)
		}
		return response(http.StatusOK, body)
	})
	before, err := os.ReadDir(filepath.Dir(sources[0].File))
	require.NoError(t, err)
	beforeInfo := make([]os.FileInfo, len(before))
	for i := range before {
		beforeInfo[i], err = before[i].Info()
		require.NoError(t, err)
	}
	out := query(t.Context(), Request{Sources: sources, Env: []string{"CODEX_API_KEY=ignored", "META_API_KEY=ignored"}}, h)
	require.Equal(t, ResponseComplete, out.Status)
	require.Equal(t, SchemaVersion, out.SchemaVersion)
	assert.Equal(t, []MissingReading{}, out.Missing)
	require.Len(t, out.Results, len(sources))
	for i, r := range out.Results {
		assert.Equal(t, sources[i].Selector, r.Selector)
		assert.Equal(t, statusOK, r.Status)
		assert.Equal(t, fixedTime(), r.AttemptedAt)
		require.NotNil(t, r.ObservedAt)
		assert.Equal(t, fixedTime(), *r.ObservedAt)
		require.NotNil(t, r.Subscription)
		assert.True(t, r.Subscription.Verified)
	}
	codex, claude, grok, agy, muse := out.Results[0], out.Results[1], out.Results[2], out.Results[3], out.Results[4]
	assert.Equal(t, ptr(false), codex.Blocked)
	assert.Equal(t, ptr(float64(100)), codex.Pools[0].Windows[0].UsedPercent)
	assert.Equal(t, ptr(false), codex.Pools[0].Windows[0].Blocked)
	assert.Equal(t, ptr(true), findModel(t, codex, "special-model").Blocked)
	assert.Equal(t, []string{agentCodex, "reviewer-model-pool"}, findModel(t, codex, "special-model").Pools)
	assert.Equal(t, ptr(false), findModel(t, codex, "standard-model").Blocked)
	assert.Equal(t, "matched", claude.AccountCheck)
	require.Len(t, claude.Pools[0].Windows, 1)
	assert.Equal(t, ptr(float64(31)), claude.Pools[0].Windows[0].UsedPercent)
	assert.Equal(t, ptr(true), findModel(t, claude, "claude-fable-5").Blocked)
	assert.Nil(t, findModel(t, claude, "claude-fable-5").ClearsAt)
	assert.Equal(t, "matched", grok.AccountCheck)
	assert.Equal(t, weeklyID, grok.Pools[0].Windows[0].ID)
	assert.Equal(t, ptr(float64(79)), grok.Pools[0].Windows[0].UsedPercent)
	assert.Equal(t, ptr(int64(604800)), grok.Pools[0].Windows[0].DurationSeconds)
	assert.Equal(t, parseTime("2026-12-03T12:00:00Z"), grok.Pools[0].Windows[0].ResetsAt)
	require.Len(t, agy.Pools, 3)
	assert.Equal(t, []string{"Claude and GPT models"}, findModel(t, agy, "claude-opus-5-5-high").Pools)
	assert.Equal(t, ptr(false), findModel(t, agy, "claude-opus-5-5-high").Blocked)
	assert.Equal(t, windowPending, agy.Pools[0].Windows[1].State)
	assert.Nil(t, agy.Pools[0].Windows[1].ResetsAt)
	assert.Equal(t, []string{"gemini-flash"}, findModel(t, agy, "former-flash").Pools)
	assert.Nil(t, findModel(t, agy, "gemini-flash").Blocked)
	assert.Equal(t, scopeUnknown, findModel(t, agy, "unlisted-model").Coverage)
	assert.Equal(t, ptr(true), muse.Blocked)
	assert.Equal(t, parseTime("2026-12-03T12:00:00Z"), muse.ClearsAt)
	assert.Equal(t, parseTime("2026-11-26T12:00:00Z"), muse.Pools[0].Windows[0].StartsAt)
	raw, err := json.Marshal(out)
	require.NoError(t, err)
	for _, hidden := range []string{fakeToken, "fixture-api-key-to-discard", "workspace-fixture", "claude@example.test"} {
		assert.NotContains(t, string(raw), hidden)
	}
	after, err := os.ReadDir(filepath.Dir(sources[0].File))
	require.NoError(t, err)
	require.Len(t, after, len(before))
	for i := range before {
		assert.Equal(t, before[i].Name(), after[i].Name())
		assert.Equal(t, before[i].Type(), after[i].Type())
		a, err := after[i].Info()
		require.NoError(t, err)
		assert.Equal(t, beforeInfo[i].ModTime(), a.ModTime())
	}
}

func findModel(t *testing.T, result Result, id string) Model {
	t.Helper()
	for _, model := range result.Models {
		if model.ID == id {
			return model
		}
	}
	t.Fatalf("model %q is missing", id)
	return Model{}
}

func TestQuotaLockoutsAndWindowStates(t *testing.T) {
	started := false
	pool := makeCodexPool("included", scopeAll, nil, &codexLimit{
		Allowed:   ptr(false),
		Primary:   &codexWindow{Used: ptr(float64(100)), Duration: json.RawMessage("18000"), Reset: json.RawMessage("1795701600")},
		Secondary: &codexWindow{Used: ptr(float64(100)), Rolling: true},
	})
	assert.Equal(t, ptr(true), pool.Blocked)
	assert.Nil(t, pool.ClearsAt)
	assert.Equal(t, windowRolling, pool.Windows[1].State)
	assert.Nil(t, pool.Windows[1].StartsAt)
	assert.NotNil(t, pool.Windows[0].StartsAt)
	pool = makeCodexPool("included", scopeAll, nil, &codexLimit{
		Allowed:   ptr(false),
		Primary:   &codexWindow{Used: ptr(float64(100)), Reset: json.RawMessage("1795701600")},
		Secondary: &codexWindow{Started: &started},
	})
	assert.Equal(t, unixSeconds(1795701600), pool.ClearsAt)
	assert.Equal(t, windowPending, pool.Windows[1].State)
	assert.Nil(t, pool.Windows[1].UsedPercent)

	credits := claudeExtra{Enabled: ptr(true), CreditsCanPay: "yes"}
	account := makeClaudeWindow("included", ptr(float64(100)), week, nil, "", "", "rejected", &credits, false)
	model := makeClaudeWindow("scoped", ptr(float64(100)), week, nil, "", "", "rejected", &credits, true)
	assert.Equal(t, ptr(false), account.Blocked)
	assert.Nil(t, model.Blocked)
	assert.Equal(t, ptr(false), agyWindow("credits", agyQuota{Exhausted: ptr(true)}, week, windowFixed, ptr(true)).Blocked)
}

func TestQuotaDetachedAccountsAndPartialFailures(t *testing.T) {
	dir := t.TempDir()
	first := detachedFixture(t, dir, "first", agentCodex, codexCredentialFixture("ws1"))
	second := detachedFixture(t, dir, "second", agentCodex, codexCredentialFixture("ws2"))
	same := detachedFixture(t, dir, "same", agentCodex, codexCredentialFixture("ws1"))
	missing := Source{Selector: "missing", Agent: agentCodex, File: filepath.Join(dir, "missing")}
	bad := detachedFixture(t, dir, "malformed", agentCodex, codexCredentialFixture("bad-response"))
	first.Expected, second.Expected = "ws1", "ws1"
	h := fixtureHTTP(func(req *http.Request) (*http.Response, error) {
		id := req.Header.Get("ChatGPT-Account-Id")
		if id == "bad-response" {
			return response(http.StatusOK, "{malformed HTTP payload with raw sensitive content")
		}
		return response(http.StatusOK, `{"account_id":"`+id+`","plan_type":"plus","rate_limit":{"allowed":true,"primary_window":{"used_percent":10}}}`)
	})
	out := query(t.Context(), Request{Sources: []Source{first, second, same, missing, bad}}, h)
	assert.Equal(t, ResponsePartial, out.Status)
	assert.Equal(t, statusOK, out.Results[0].Status)
	assert.Equal(t, statusMismatch, out.Results[1].Status)
	assert.Equal(t, ptr(false), out.Results[1].Reauth)
	assert.Equal(t, out.Results[0].Subscription.ID, out.Results[2].Subscription.ID)
	assert.NotEqual(t, out.Results[0].Subscription.ID, out.Results[1].Subscription.ID)
	assert.Equal(t, reasonMissingCredential, out.Results[3].Reason)
	assert.Nil(t, out.Results[3].ObservedAt)
	assert.Equal(t, "invalid_response", out.Results[4].Reason)
	assert.Equal(t, []MissingReading{
		{Selector: "second", Agent: agentCodex, Status: statusMismatch, Reason: "unexpected_account"},
		{Selector: "missing", Agent: agentCodex, Status: statusCredentials, Reason: reasonMissingCredential},
		{Selector: "malformed", Agent: agentCodex, Status: statusUnavailable, Reason: "invalid_response"},
	}, out.Missing)
	raw, err := json.Marshal(out)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "raw sensitive content")
}

func TestQuotaCancellationAndCredentialRejection(t *testing.T) {
	dir := t.TempDir()
	success := detachedFixture(t, dir, "complete", agentCodex, codexCredentialFixture("complete"))
	wait := detachedFixture(t, dir, "waiting", agentCodex, codexCredentialFixture("waiting"))
	started := make(chan struct{})
	var inFlight atomic.Int32
	h := fixtureHTTP(func(req *http.Request) (*http.Response, error) {
		if req.Header.Get("ChatGPT-Account-Id") == "waiting" {
			inFlight.Add(1)
			defer inFlight.Add(-1)
			close(started)
			<-req.Context().Done()
			return nil, req.Context().Err()
		}
		return response(http.StatusOK, `{"account_id":"complete","rate_limit":{"primary_window":{"used_percent":10}}}`)
	})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan Response, 1)
	go func() { done <- query(ctx, Request{Sources: []Source{success, wait}}, h) }()
	<-started
	cancel()
	out := <-done
	assert.Zero(t, inFlight.Load())
	assert.Equal(t, "canceled", out.Results[1].Reason)
	assert.Nil(t, out.Results[1].ObservedAt)

	refresh := detachedFixture(t, dir, "refresh", agentCodex, `{"auth_mode":"chatgpt","tokens":{"access_token":"fixture-token","refresh_token":"fixture-refresh"}}`)
	h = fixtureHTTP(func(*http.Request) (*http.Response, error) {
		return response(http.StatusUnauthorized, `{"error":{"type":"expired_token","message":"secret raw error"}}`)
	})
	out = query(t.Context(), Request{Sources: []Source{refresh}}, h)
	assert.Equal(t, ResponseNoReadings, out.Status)
	assert.Equal(t, "access_token_expired", out.Results[0].Reason)
	assert.Nil(t, out.Results[0].Reauth)
	assert.Nil(t, out.Results[0].ObservedAt)

	h = fixtureHTTP(func(*http.Request) (*http.Response, error) {
		return response(http.StatusUnauthorized, `{"error":"session_revoked"}`)
	})
	out = query(t.Context(), Request{Sources: []Source{refresh}}, h)
	assert.Equal(t, statusLoginExpired, out.Results[0].Status)
	assert.Equal(t, credentialRevoked, out.Results[0].Reason)
	assert.Equal(t, ptr(true), out.Results[0].Reauth)
}

func TestQuotaTransportDoesNotRedirect(t *testing.T) {
	var forwarded atomic.Bool
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/forwarded" {
			forwarded.Store(true)
			return
		}
		http.Redirect(w, req, "/forwarded", http.StatusFound)
	}))
	defer server.Close()
	h := newHTTPReader()
	h.client.Transport = server.Client().Transport
	var payload any
	_, failed := h.fetch(t.Context(), credential{token: fakeToken}, http.MethodGet, server.URL, "", nil, &payload)
	assert.Equal(t, "provider_http_error", failed.reason)
	assert.False(t, forwarded.Load())
}

func TestQuotaAuthUsesLaunchEnvironmentWithoutCreatingHomes(t *testing.T) {
	root := t.TempDir()
	foreign := t.TempDir()
	project := &config.ProjectConfig{Root: root}
	project.Config.Agents.Codex.LocalConfigDir = ptr(true)
	project.Config.Agents.Claude.LocalConfigDir = ptr(true)
	codexFile := filepath.Join(foreign, authFilename)
	writeFixture(t, codexFile, codexCredentialFixture("foreign"))
	modelAuthFile := filepath.Join(root, ".grok-config", nativeConfig)
	writeFixture(t, modelAuthFile, "[model.external]\nenv_key = 'OTHER_API_KEY'\n")
	grokFile := filepath.Join(root, ".grok-config", authFilename)
	writeFixture(t, grokFile, `{"https://auth.x.ai::primary":{"key":"fixture-token","auth_mode":"oidc"}}`)
	req := Request{Project: project, WorkDir: root, Env: []string{
		codexHomeEnv + "=" + foreign, "OPENAI_API_KEY=unselected",
		"GROK_HOME=" + foreign, "XAI_API_KEY=unselected", "GEMINI_API_KEY=unselected",
	}}
	auth, fail := loadCredential(t.Context(), req, Source{Agent: agentCodex})
	assert.Empty(t, fail.status)
	assert.Equal(t, "foreign", auth.account)
	_, err := os.Stat(filepath.Join(root, ".codex"))
	assert.True(t, errors.Is(err, os.ErrNotExist))

	auth, fail = loadCredential(t.Context(), req, Source{Agent: agentGrok})
	assert.Empty(t, fail.status)
	assert.Equal(t, fakeToken, auth.token)
	_, fail = loadCredential(t.Context(), req, Source{Agent: agentGrok, ConfiguredModel: "external"})
	assert.Equal(t, statusNonSubscription, fail.status)
	_, fail = loadCredential(t.Context(), req, Source{Agent: agentClaude})
	assert.Equal(t, reasonMissingCredential, fail.reason)
	_, err = os.Stat(filepath.Join(root, ".claude-config"))
	assert.True(t, errors.Is(err, os.ErrNotExist))

	agy := filepath.Join(root, ".agy", agyProfileDirectory)
	writeFixture(t, filepath.Join(agy, "antigravity-oauth-token"), `{"auth_method":"consumer","token":{"access_token":"fixture-token"}}`)
	auth, fail = loadCredential(t.Context(), req, Source{Agent: agentAntigravity})
	assert.Empty(t, fail.status)
	assert.Equal(t, fakeToken, auth.token)
	writeFixture(t, filepath.Join(agy, "settings.json"), `{"modelProvider":"gemini"}`)
	_, fail = loadCredential(t.Context(), req, Source{Agent: agentAntigravity})
	assert.Equal(t, statusNonSubscription, fail.status)

	req.Env = append(req.Env, "CODEX_API_KEY=selected")
	_, fail = loadCredential(t.Context(), req, Source{Agent: agentCodex})
	assert.Equal(t, statusNonSubscription, fail.status)
}

func TestQuotaTimeoutKeepsCompletedReading(t *testing.T) {
	dir := t.TempDir()
	first := detachedFixture(t, dir, "done", agentCodex, codexCredentialFixture("done"))
	second := detachedFixture(t, dir, "slow", agentCodex, codexCredentialFixture("slow"))
	ctx, cancel := context.WithTimeout(t.Context(), 150*time.Millisecond)
	defer cancel()
	h := fixtureHTTP(func(req *http.Request) (*http.Response, error) {
		if req.Header.Get("ChatGPT-Account-Id") == "slow" {
			<-req.Context().Done()
			return nil, req.Context().Err()
		}
		return response(http.StatusOK, `{"account_id":"done","rate_limit":{"primary_window":{"used_percent":10,"reset_at":{"bad":true},"limit_window_seconds":"invalid"}}}`)
	})
	out := query(ctx, Request{Sources: []Source{first, second}}, h)
	require.Equal(t, ResponsePartial, out.Status)
	require.Equal(t, statusOK, out.Results[0].Status)
	assert.Nil(t, out.Results[0].Pools[0].Windows[0].ResetsAt)
	assert.Nil(t, out.Results[0].Pools[0].Windows[0].DurationSeconds)
	assert.Equal(t, "timeout", out.Results[1].Reason)
	assert.Nil(t, out.Results[1].ObservedAt)
}

func TestQuotaRereadsCredentialAfterExternalRotation(t *testing.T) {
	dir := t.TempDir()
	source := detachedFixture(t, dir, "rotating", agentCodex, codexCredentialFixture("workspace"))
	calls := 0
	h := fixtureHTTP(func(req *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			updated := filepath.Join(dir, "updated.json")
			if err := os.WriteFile(updated, []byte(strings.ReplaceAll(codexCredentialFixture("workspace"), fakeToken, "fresh-fixture")), 0o600); err != nil {
				return nil, err
			}
			if err := os.Rename(updated, source.File); err != nil {
				return nil, err
			}
			return response(http.StatusUnauthorized, `{}`)
		}
		assert.Equal(t, "Bearer fresh-fixture", req.Header.Get("Authorization"))
		return response(http.StatusOK, `{"account_id":"workspace","rate_limit":{"primary_window":{"used_percent":20}}}`)
	})
	out := query(t.Context(), Request{Sources: []Source{source}}, h)
	assert.Equal(t, ResponseComplete, out.Status)
	assert.Equal(t, 2, calls)
}

func TestQuotaGrokAccountScopeAndCreditRules(t *testing.T) {
	source := subscriptionsFixture(t)[2]
	source.Expected = "u1"
	billing := `{"config":{"creditUsagePercent":100,
		"currentPeriod":{"type":"USAGE_PERIOD_TYPE_WEEKLY","end":"2026-12-03T12:00:00Z"},
		"prepaidBalance":{"val":5},"spendLimitReached":false}}`
	account := `{"hasGrokCodeAccess":true,"subscriptionTier":"test","userId":"u1","principalId":"seat1","teamId":"team2",
		"userBlockedReason":null,"teamBlockedReasons":["TEAM_SPEND_LIMIT_REACHED"]}`
	h := fixtureHTTP(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/v1/billing":
			return response(http.StatusOK, billing)
		case "/v1/user":
			return response(http.StatusOK, account)
		default:
			t.Errorf("unexpected endpoint %s", req.URL)
			return response(http.StatusNotFound, "{}")
		}
	})
	out := query(t.Context(), Request{Sources: []Source{source}}, h)
	result := out.Results[0]
	assert.Equal(t, statusMismatch, result.Status)
	assert.Equal(t, ptr(true), result.Blocked)
	assert.Nil(t, result.ClearsAt)
	assert.Equal(t, ptr(false), result.Pools[0].Blocked)
	assert.NotEqual(t, subscription(agentGrok, "team1/seat1", "", true).ID, result.Subscription.ID)
}

func TestQuotaKeepsUsageWhenAccountIdentityIsUnavailable(t *testing.T) {
	source := subscriptionsFixture(t)[1]
	usage := readFixture(t, agentClaude)
	h := fixtureHTTP(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() == claudeOAuth+"usage" {
			return response(http.StatusOK, usage)
		}
		return response(http.StatusTooManyRequests, "fixture provider error body")
	})
	out := query(t.Context(), Request{Sources: []Source{source}}, h)
	assert.Equal(t, ResponseComplete, out.Status)
	assert.Contains(t, out.Results[0].Missing, "account_identity")
	assert.True(t, out.Results[0].hasUsage())
	source.Expected = "account"
	out = query(t.Context(), Request{Sources: []Source{source}}, h)
	assert.Equal(t, ResponseNoReadings, out.Status)
	assert.Equal(t, statusMismatch, out.Results[0].Status)
	assert.Equal(t, "unverified", out.Results[0].AccountCheck)
	assert.True(t, out.Results[0].hasUsage())
}

func TestQuotaCodexWorkspaceSeatsHaveDistinctIdentity(t *testing.T) {
	data := codexUsageResponse{Tier: "business", AccountID: "same-workspace", UserID: "seat-1"}
	first := codexSubscription(data, nil)
	data.UserID = "seat-2"
	second := codexSubscription(data, nil)
	assert.True(t, first.Verified)
	assert.True(t, second.Verified)
	assert.NotEqual(t, first.ID, second.ID)
	result := Result{Status: statusOK, Subscription: second}
	result.checkAccount("same-workspace/seat-1", fixedTime())
	assert.Equal(t, statusMismatch, result.Status)
	data.UserID = ""
	assert.False(t, codexSubscription(data, nil).Verified)

	data.Tier, data.UserID = "enterprise_cbp_usage_based", "seat-1"
	assert.Equal(t, first.ID, codexSubscription(data, nil).ID)
	data.UserID = "seat-2"
	assert.Equal(t, second.ID, codexSubscription(data, nil).ID)
	data.Tier = "unrecognized_plan"
	assert.False(t, codexSubscription(data, nil).Verified)
}
