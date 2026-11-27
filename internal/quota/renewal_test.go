package quota

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/config"
)

const renewalHarness = `#!/bin/sh
set -eu
case "${0##*/}" in
  grok) [ "$*" = '--no-auto-update models' ]; store="$GROK_AUTH_PATH" ;;
  agy) [ "$2" = models ]; store="${1#--gemini_dir=}/antigravity-cli/antigravity-oauth-token" ;;
  muse) [ "$*" = 'usage --json' ]; store="$XDG_CONFIG_HOME/muse/auth.json" ;;
  claude)
    case "$*" in *--no-session-persistence*--strict-mcp-config*--tools*) ;; *) exit 1 ;; esac
    IFS= read -r frame
    case "$frame" in *'"type":"control_request"'*'"subtype":"initialize"'*|*'"subtype":"initialize"'*'"type":"control_request"'*) ;; *) exit 1 ;; esac
    store="$CLAUDE_CONFIG_DIR/.credentials.json" ;;
  codex)
    [ "$*" = app-server ]
    IFS= read -r frame
    case "$frame" in *'"method":"initialize"'*) ;; *) exit 1 ;; esac
    printf '%s\n' '{"id":1,"result":{}}'
    IFS= read -r frame
    [ "$frame" = '{"method":"initialized"}' ]
    IFS= read -r frame
    [ "$frame" = '{"id":2,"method":"account/read","params":{"refreshToken":true}}' ]
    store="$CODEX_HOME/auth.json" ;;
  *) exit 1 ;;
esac
[ "$store" = "$AL_TEST_QUOTA_STORE" ]
printf x >> "$AL_TEST_QUOTA_ATTEMPTS"
if [ -n "${AL_TEST_QUOTA_ERROR:-}" ]; then
  printf '%s\n' "$AL_TEST_QUOTA_ERROR"
  exit 0
fi
if [ "${AL_TEST_QUOTA_REMOVE:-}" = yes ]; then
  rm "$store"
  exit 0
fi
cp "$AL_TEST_QUOTA_REPLACEMENT" "$store.next"
mv "$store.next" "$store"
case "${0##*/}" in
  claude) printf '%s\n' '{"type":"control_response","response":{"request_id":"models","subtype":"success","response":{"models":[{"value":"fixture-model"}]}}}' ;;
  codex) printf '%s\n' '{"id":2,"result":{"account":{"type":"chatgpt"}}}' ;;
esac
`

func renewalTestRequest(t *testing.T, agent, binary, relative string, old, fresh string) (Request, string) {
	t.Helper()
	root := t.TempDir()
	bin := filepath.Join(root, binary)
	require.NoError(t, os.WriteFile(bin, []byte(renewalHarness), 0o700)) // #nosec G306 -- executable test harness in a private temp directory.
	t.Setenv("PATH", root+string(os.PathListSeparator)+os.Getenv("PATH"))
	store, attempts, replacement := filepath.Join(root, relative), filepath.Join(root, "attempts"), filepath.Join(root, "replacement")
	writeFixture(t, store, old)
	writeFixture(t, replacement, fresh)
	project := &config.ProjectConfig{Root: root, Env: map[string]string{
		"AL_TEST_QUOTA_STORE": store, "AL_TEST_QUOTA_REPLACEMENT": replacement, "AL_TEST_QUOTA_ATTEMPTS": attempts,
		claudeConfigEnv: filepath.Join(root, ".claude-config"), codexHomeEnv: filepath.Join(root, ".codex"),
		"XDG_CONFIG_HOME": filepath.Join(root, ".config"),
	}}
	project.Config.Agents.Claude.LocalConfigDir = ptr(true)
	project.Config.Agents.Codex.LocalConfigDir = ptr(true)
	env := []string{"HOME=" + root, "PATH=" + os.Getenv("PATH")}
	return Request{Project: project, WorkDir: root, Env: env, Sources: []Source{{Selector: agent, Agent: agent}}}, attempts
}

func renewalFixtureHTTP(t *testing.T, wantToken string) *httpReader {
	t.Helper()
	routes := allFixtureRoutes(t)
	return fixtureHTTP(func(req *http.Request) (*http.Response, error) {
		if req.Header.Get("Authorization") != "Bearer "+wantToken {
			t.Error("quota request did not use the renewed fixture credential")
		}
		assert.Contains(t, routes, req.URL.String())
		return response(http.StatusOK, routes[req.URL.String()])
	})
}

func quotaFixtureJWT(at time.Time) string {
	claims := fmt.Sprintf(`{"exp":%d}`, at.Unix())
	return "fixture." + base64.RawURLEncoding.EncodeToString([]byte(claims)) + ".fixture"
}

func TestQuotaRenewalUsesSelectedNativeStore(t *testing.T) {
	old, fresh := fixedTime().Add(-time.Hour), fixedTime().Add(time.Hour)
	grokLogin := func(token string, expires time.Time) string {
		return fmt.Sprintf(`{"https://auth.x.ai::public-client":{"key":%q,"auth_mode":"oidc","refresh_token":"fixture-refresh","expires_at":%q,"principal_id":"seat1","team_id":"team1"}}`, token, expires.Format(time.RFC3339))
	}
	claudeLogin := func(token string, expires time.Time) string {
		return fmt.Sprintf(`{"claudeAiOauth":{"accessToken":%q,"refreshToken":"fixture-refresh","expiresAt":%d}}`, token, expires.UnixMilli())
	}
	codexLogin := func(at time.Time) string {
		return fmt.Sprintf(`{"auth_mode":"chatgpt","tokens":{"access_token":%q,"refresh_token":"fixture-refresh","account_id":"workspace-fixture"}}`, quotaFixtureJWT(at))
	}
	agyLogin := func(token string, expires time.Time) string {
		return fmt.Sprintf(`{"auth_method":"consumer","token":{"access_token":%q,"refresh_token":"fixture-refresh","expiry":%q}}`, token, expires.Format(time.RFC3339))
	}
	museLogin := func(token string, expires time.Time) string {
		return fmt.Sprintf(`{"providers":{"meta":{"mechanism":"oauth","access_token":%q,"refresh_token":"fixture-refresh","expires_at":%q}}}`, token, expires.Format(time.RFC3339))
	}
	for _, tc := range []struct {
		agent, binary, relative, stale, ready, token string
	}{
		{agentGrok, agentGrok, ".grok-config/auth.json", grokLogin("stale", old), grokLogin(fakeToken, fresh), fakeToken},
		{agentClaude, agentClaude, ".claude-config/.credentials.json", claudeLogin("stale", old), claudeLogin(fakeToken, fresh), fakeToken},
		{agentCodex, agentCodex, ".codex/auth.json", codexLogin(old), codexLogin(fresh), quotaFixtureJWT(fresh)},
		{agentAntigravity, "agy", ".agy/antigravity-cli/antigravity-oauth-token", agyLogin("stale", old), agyLogin(fakeToken, fresh), fakeToken},
		{agentMuse, agentMuse, ".config/muse/auth.json", museLogin("stale", old), museLogin(fakeToken, fresh), fakeToken},
	} {
		t.Run(tc.agent, func(t *testing.T) {
			req, attempts := renewalTestRequest(t, tc.agent, tc.binary, tc.relative, tc.stale, tc.ready)
			second := req.Sources[0]
			second.Selector = "second"
			req.Sources = append(req.Sources, second)
			h := renewalFixtureHTTP(t, tc.token)
			offline := req
			offline.Offline = true
			assert.Equal(t, ResponseNoReadings, query(t.Context(), offline, h).Status)
			detached := Request{Sources: []Source{{Agent: tc.agent, File: filepath.Join(req.WorkDir, tc.relative)}}}
			out := query(t.Context(), detached, h)
			assert.Equal(t, "access_token_expired", out.Results[0].Reason)
			assert.NoFileExists(t, attempts)

			short, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			out = query(short, req, h)
			cancel()
			assert.Equal(t, reasonAccessExpired, out.Results[0].Reason)
			assert.True(t, out.Results[0].Retryable)
			assert.NoFileExists(t, attempts)

			out = query(t.Context(), req, h)
			require.Equal(t, ResponseComplete, out.Status, out.Results)
			for _, result := range out.Results {
				assert.Equal(t, ptr(false), result.Reauth)
				assert.Zero(t, result.Retries)
			}
			assert.Equal(t, ResponseComplete, query(t.Context(), req, h).Status)
			count, err := os.ReadFile(attempts) // #nosec G304 -- fixture path in t.TempDir.
			require.NoError(t, err)
			assert.Equal(t, "x", string(count))
		})
	}
}

func TestQuotaCodexFailedRenewalRequiresLogin(t *testing.T) {
	stale := fmt.Sprintf(`{"auth_mode":"chatgpt","tokens":{"access_token":%q,"refresh_token":"fixture-refresh"}}`, quotaFixtureJWT(fixedTime().Add(-time.Hour)))
	for _, tc := range []struct {
		message, reason string
	}{
		{"Your refresh token was revoked.", credentialRevoked},
		{"Your refresh token has expired.", reasonExpiredCredentials},
		{"Your refresh token was already used.", reasonExpiredCredentials},
		{"You have since logged out or signed in to another account.", reasonExpiredCredentials},
	} {
		t.Run(tc.message, func(t *testing.T) {
			req, attempts := renewalTestRequest(t, agentCodex, agentCodex, ".codex/auth.json", stale, stale)
			raw, err := json.Marshal(tc.message)
			require.NoError(t, err)
			req.Project.Env["AL_TEST_QUOTA_ERROR"] = `{"id":2,"error":{"message":` + string(raw) + `}}`
			assertFailedRenewal(t, req, attempts, tc.reason)
		})
	}
}

func TestQuotaGrokRemovedLoginRequiresLogin(t *testing.T) {
	stale := fmt.Sprintf(`{"https://auth.x.ai::public-client":{"key":"stale","auth_mode":"oidc","refresh_token":"fixture-refresh","expires_at":%q}}`,
		fixedTime().Add(-time.Hour).Format(time.RFC3339))
	req, attempts := renewalTestRequest(t, agentGrok, agentGrok, ".grok-config/auth.json", stale, stale)
	req.Project.Env["AL_TEST_QUOTA_REMOVE"] = "yes"
	assertFailedRenewal(t, req, attempts, reasonExpiredCredentials)
}

func assertFailedRenewal(t *testing.T, req Request, attempts, reason string) {
	t.Helper()
	h := fixtureHTTP(func(*http.Request) (*http.Response, error) {
		t.Error("quota request sent after failed renewal")
		return response(http.StatusUnauthorized, "{}")
	})
	out := query(t.Context(), req, h)
	require.Equal(t, ResponseNoReadings, out.Status)
	assert.FileExists(t, attempts)
	assert.Equal(t, statusLoginExpired, out.Results[0].Status)
	assert.Equal(t, reason, out.Results[0].Reason)
	assert.Equal(t, ptr(true), out.Results[0].Reauth)
}

func TestQuotaNativeOneShotFinishesDuringGrace(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	root := t.TempDir()
	ready := filepath.Join(root, "ready")
	release := filepath.Join(root, "release")
	done := filepath.Join(root, "saved")
	native := nativeCredential{dir: root, env: []string{"PATH=" + os.Getenv("PATH")}}
	wait := make(chan error, 1)
	go func() {
		wait <- native.run(ctx, "/bin/sh", []string{"-c", `printf ready > ready; while [ ! -f release ]; do sleep 0.01; done; printf saved > saved`})
	}()
	require.Eventually(t, func() bool {
		_, err := os.Stat(ready)
		return err == nil
	}, 2*time.Second, 10*time.Millisecond)
	cancel()
	require.NoError(t, os.WriteFile(release, []byte("go"), 0o600))
	require.NoError(t, <-wait)
	assert.FileExists(t, done)
}

func TestQuotaClaudeKeychainFailureIsNotMissingLogin(t *testing.T) {
	home := t.TempDir()
	s := credentialSource{root: home, env: []string{"HOME=" + home}}
	read := func(context.Context, string, string) ([]byte, failure) {
		return nil, unsupportedAuth(keyringUnavailable)
	}
	_, fail := claudeCredentialFrom(t.Context(), s, read)
	assert.Equal(t, keyringUnavailable, fail.reason)
	writeFixture(t, filepath.Join(home, ".claude", claudeCredentials), `{"claudeAiOauth":{"accessToken":"fixture-token"}}`)
	auth, fail := claudeCredentialFrom(t.Context(), s, read)
	assert.Empty(t, fail.status)
	assert.Nil(t, auth.native)
	fileOnly, fail := claudeCredentialFrom(t.Context(), s, nil)
	assert.Empty(t, fail.status)
	require.NotNil(t, fileOnly.native)
	assert.Equal(t, filepath.Join(home, ".claude"), fileOnly.native.lockDir)

	// Keychain login when ~/.claude does not exist yet must fall back to the
	// common OS-user lock directory so locking succeeds without ENOENT.
	missingHome := t.TempDir()
	keychainSource := credentialSource{root: missingHome, env: []string{"HOME=" + missingHome}}
	keychainRead := func(context.Context, string, string) ([]byte, failure) {
		return []byte(`{"claudeAiOauth":{"accessToken":"fixture-token","refreshToken":"fixture-refresh"}}`), failure{}
	}
	auth, fail = claudeCredentialFrom(t.Context(), keychainSource, keychainRead)
	require.Empty(t, fail.status)
	require.NotNil(t, auth.native)
	commonLockDir := osUserLockDir(missingHome)
	assert.Equal(t, commonLockDir, auth.native.lockDir)
	unlock, err := auth.native.lock(t.Context())
	require.NoError(t, err)
	unlock()

	// Processes sharing the same Keychain service via an existing absolute
	// CLAUDE_CONFIG_DIR vs CLAUDE_SECURESTORAGE_CONFIG_DIR lock that directory.
	alt := t.TempDir()
	viaConfigDir, failA := claudeCredentialFrom(t.Context(), credentialSource{
		root: missingHome, env: []string{"HOME=" + missingHome, claudeConfigEnv + "=" + alt},
	}, keychainRead)
	require.Empty(t, failA.status)
	viaSecureStorage, failB := claudeCredentialFrom(t.Context(), credentialSource{
		root: missingHome, env: []string{"HOME=" + missingHome, "CLAUDE_SECURESTORAGE_CONFIG_DIR=" + alt},
	}, keychainRead)
	require.Empty(t, failB.status)
	assert.Equal(t, alt, viaConfigDir.native.lockDir)
	assert.Equal(t, viaConfigDir.native.lockDir, viaSecureStorage.native.lockDir)

	// Relative CLAUDE_SECURESTORAGE_CONFIG_DIR hashes the raw value, so two
	// different working directories and HOME overrides must share one OS-user lock.
	dirA, dirB := t.TempDir(), t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dirA, ".claude-config"), 0o700))
	require.NoError(t, os.Mkdir(filepath.Join(dirB, ".claude-config"), 0o700))
	relA, failRelA := claudeCredentialFrom(t.Context(), credentialSource{
		root: dirA, workDir: dirA, env: []string{"HOME=" + dirA, "CLAUDE_SECURESTORAGE_CONFIG_DIR=.claude-config"},
	}, keychainRead)
	require.Empty(t, failRelA.status)
	relB, failRelB := claudeCredentialFrom(t.Context(), credentialSource{
		root: dirB, workDir: dirB, env: []string{"HOME=" + dirB, "CLAUDE_SECURESTORAGE_CONFIG_DIR=.claude-config"},
	}, keychainRead)
	require.Empty(t, failRelB.status)
	assert.Equal(t, commonLockDir, relA.native.lockDir)
	assert.Equal(t, relA.native.lockDir, relB.native.lockDir)
}

func TestQuotaGrokExternalAuthRenewalEnsuresPrivateHome(t *testing.T) {
	old, fresh := fixedTime().Add(-time.Hour), fixedTime().Add(time.Hour)
	grokLogin := func(token string, expires time.Time) string {
		return fmt.Sprintf(`{"https://auth.x.ai::public-client":{"key":%q,"auth_mode":"oidc","refresh_token":"fixture-refresh","expires_at":%q,"principal_id":"seat1","team_id":"team1"}}`, token, expires.Format(time.RFC3339))
	}
	external := filepath.Join(t.TempDir(), "external-auth.json")
	req, attempts := renewalTestRequest(t, agentGrok, agentGrok, "unused-store.json", grokLogin("stale", old), grokLogin(fakeToken, fresh))
	writeFixture(t, external, grokLogin("stale", old))
	req.Project.Env["AL_TEST_QUOTA_STORE"] = external
	req.Project.Env["GROK_AUTH_PATH"] = external
	grokHome := filepath.Join(req.Project.Root, ".grok-config")
	_, err := os.Stat(grokHome)
	require.ErrorIs(t, err, os.ErrNotExist)

	out := query(t.Context(), req, renewalFixtureHTTP(t, fakeToken))
	require.Equal(t, ResponseComplete, out.Status, out.Results)
	assert.FileExists(t, attempts)
	info, err := os.Stat(grokHome)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), info.Mode().Perm())
}
