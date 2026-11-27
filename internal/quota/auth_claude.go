package quota

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"path/filepath"
	"runtime"
	"slices"

	"github.com/conn-castle/agent-layer/internal/clients"
)

func claudeCredential(ctx context.Context, s credentialSource) (credential, failure) {
	return claudeCredentialFrom(ctx, s, runtime.GOOS, readKeyring)
}

func claudeCredentialFrom(ctx context.Context, s credentialSource, platform string, read keyringReader) (credential, failure) {
	if s.file != "" {
		return claudeFileCredential(s.file)
	}
	home := s.home(claudeConfigEnv, ".claude")
	if home == "" {
		return credential{}, unsupportedAuth(reasonCredentialPath)
	}
	env, settings, failed := claudeLaunchSettings(s, home)
	if failed.status != "" {
		return credential{}, failed
	}
	nativeConfigDir := s.value(claudeConfigEnv)
	s.env = env
	if s.has("CLAUDE_CODE_USE_BEDROCK", "CLAUDE_CODE_USE_VERTEX", "CLAUDE_CODE_USE_FOUNDRY", "CLAUDE_CODE_CUSTOM_OAUTH_URL") ||
		customEndpoint(s.value("ANTHROPIC_BASE_URL"), "https://api.anthropic.com") {
		return credential{}, unsupportedAuth(reasonCustomEndpoint)
	}
	if s.has("ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN") {
		return credential{}, nonSubscription("api_key_or_gateway_token")
	}
	if s.has("CLAUDE_CODE_API_KEY_FILE_DESCRIPTOR") || setting(settings, "apiKeyHelper") != "" {
		return credential{}, unsupportedAuth(reasonCustomAuth)
	}
	if token := s.value("CLAUDE_CODE_OAUTH_TOKEN"); token != "" {
		return credential{token: token, expires: jwtExpiry(token), renewalUnknown: true}, failure{}
	}
	if s.has("CLAUDE_CODE_OAUTH_TOKEN_FILE_DESCRIPTOR", "ANTHROPIC_PROFILE", "ANTHROPIC_FEDERATION_RULE_ID") ||
		setting(settings, "forceLoginGatewayUrl") != "" || setting(settings, "forceLoginMethod") == "gateway" {
		return credential{}, unsupportedAuth(reasonCustomAuth)
	}
	// Claude discovers its configuration home before loading its settings.
	// A settings-level home override makes a file-backed login ambiguous.
	if nativeConfigDir != s.value(claudeConfigEnv) || !clients.SamePath(home, s.home(claudeConfigEnv, ".claude")) {
		return credential{}, unsupportedAuth(reasonCustomAuth)
	}
	var keychainFailure failure
	if platform == platformDarwin {
		service := "Claude Code-credentials"
		selected, override := clients.GetEnv(s.env, "CLAUDE_SECURESTORAGE_CONFIG_DIR")
		if !override {
			selected = s.value(claudeConfigEnv)
		}
		if selected != "" {
			sum := sha256.Sum256([]byte(selected))
			service += fmt.Sprintf("-%x", sum[:4])
		}
		raw, fail := read(ctx, service, "")
		if fail.status == "" {
			auth, fail := parseClaudeCredential(raw)
			lockDir := home
			if override && selected != nativeConfigDir {
				lockDir = s.home("", "")
			}
			auth.native = s.native(lockDir, "", "")
			return auth, fail
		}
		keychainFailure = fail
	}
	path := filepath.Join(home, claudeCredentials)
	auth, fail := claudeFileCredential(path)
	if fail.reason == reasonMissingCredential && keychainFailure.status != "" {
		return credential{}, keychainFailure
	}
	if keychainFailure.status == "" || keychainFailure.reason == reasonMissingCredential {
		auth.native = s.native(home, "", "")
	}
	return auth, fail
}

func claudeLaunchSettings(s credentialSource, home string) ([]string, map[string]any, failure) {
	var settings map[string]any
	project := filepath.Join(s.root, ".claude")
	workspace := filepath.Join(textFrom(s.workDir, s.root), ".claude")
	projectSettings := filepath.Join(project, "settings.json")
	paths := []string{filepath.Join(home, "settings.json"), projectSettings}
	if workspace != project {
		paths = append(paths, filepath.Join(workspace, "settings.json"), filepath.Join(workspace, "settings.local.json"))
	}
	paths = append(paths, filepath.Join(project, "settings.local.json"))
	for i, path := range paths {
		var layer map[string]any
		if fail := settingsFile(path, &layer); fail.status != "" {
			return nil, nil, fail
		}
		settings = mergeSettings(settings, layer)
		if i == 1 {
			settings = mergeSettings(settings, s.config)
		}
	}
	var managedPath string
	switch runtime.GOOS {
	case platformDarwin:
		managedPath = "/Library/Application Support/ClaudeCode/managed-settings.json"
	case "linux":
		managedPath = "/etc/claude-code/managed-settings.json"
	}
	if managedPath != "" {
		var managed map[string]any
		if fail := settingsFile(managedPath, &managed); fail.status != "" {
			return nil, nil, fail
		}
		settings = mergeSettings(settings, managed)
	}
	env := s.env
	for key, raw := range table(settings, "env") {
		if value, ok := raw.(string); ok {
			env = clients.SetUniqueEnv(env, key, value)
		}
	}
	return env, settings, failure{}
}

func claudeFileCredential(path string) (credential, failure) {
	raw, fail := credentialFile(path)
	if fail.status != "" {
		return credential{}, fail
	}
	return parseClaudeCredential(raw)
}

func parseClaudeCredential(raw []byte) (credential, failure) {
	var data struct {
		OAuth struct {
			Access         string   `json:"accessToken"`
			Refresh        string   `json:"refreshToken"`
			Expires        int64    `json:"expiresAt"`
			RefreshExpires int64    `json:"refreshTokenExpiresAt"`
			Scopes         []string `json:"scopes"`
			Tier           string   `json:"subscriptionType"`
			Account        string   `json:"accountUuid"`
			Organization   string   `json:"organizationUuid"`
		} `json:"claudeAiOauth"`
	}
	if json.Unmarshal(raw, &data) != nil {
		return credential{}, invalidCredentials()
	}
	oauth := data.OAuth
	if oauth.Access == "" {
		return credential{}, missingCredentials()
	}
	if len(oauth.Scopes) > 0 && !slices.Contains(oauth.Scopes, "user:profile") {
		return credential{}, unsupportedAuth("oauth_scope_unavailable")
	}
	return credential{
		token: oauth.Access, expires: millis(oauth.Expires), refreshable: oauth.Refresh != "",
		refreshExpires: millis(oauth.RefreshExpires),
		identity:       subscription(agentClaude, claudePrincipal(oauth.Organization, oauth.Account), oauth.Tier, false),
	}, failure{}
}

func claudePrincipal(org, account string) string {
	if org == "" {
		return account
	}
	if account == "" {
		return ""
	}
	return org + "/" + account
}
