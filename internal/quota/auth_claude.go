package quota

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os/user"
	"path/filepath"
	"runtime"
	"slices"

	"github.com/conn-castle/agent-layer/internal/clients"
)

func claudeCredential(ctx context.Context, s credentialSource) (credential, failure) {
	var read keyringReader
	if runtime.GOOS == platformDarwin {
		read = readKeyring
	}
	return claudeCredentialFrom(ctx, s, read)
}

func claudeCredentialFrom(ctx context.Context, s credentialSource, read keyringReader) (credential, failure) {
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
	if s.enabledFlag("CLAUDE_CODE_USE_BEDROCK", "CLAUDE_CODE_USE_VERTEX", "CLAUDE_CODE_USE_FOUNDRY") ||
		s.has("CLAUDE_CODE_CUSTOM_OAUTH_URL") ||
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
	if read != nil {
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
			// Keychain service names hash the raw selector string. A relative
			// selector maps to the same service from different working
			// directories, so only an existing absolute selector uses its own
			// lock directory; relative or missing selectors share a common
			// fallback directory keyed by the OS user.
			lockDir := claudeKeychainLockDir(selected, s.home("", ""))
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
	if fail := settingsFile(filepath.Join(home, "settings.json"), &settings); fail.status != "" {
		return nil, nil, fail
	}
	// Dispatch regenerates <root>/.claude/settings.json from project config,
	// so resolve that layer from the current config rather than a stale file.
	settings = mergeSettings(settings, s.config)
	project := filepath.Join(s.root, ".claude")
	workspace := filepath.Join(textFrom(s.workDir, s.root), ".claude")
	paths := []string{filepath.Join(project, "settings.local.json")}
	if workspace != project {
		paths = append(paths, filepath.Join(workspace, "settings.json"), filepath.Join(workspace, "settings.local.json"))
	}
	for _, path := range paths {
		var layer map[string]any
		if fail := settingsFile(path, &layer); fail.status != "" {
			return nil, nil, fail
		}
		settings = mergeSettings(settings, layer)
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

func claudeKeychainLockDir(selected, fallback string) string {
	common := osUserLockDir(fallback)
	if filepath.IsAbs(selected) {
		return existingDirOrFallback(selected, common)
	}
	return common
}

func osUserLockDir(fallback string) string {
	if current, err := user.Current(); err == nil && current.HomeDir != "" {
		if dir := existingDirOrFallback(current.HomeDir, ""); dir != "" {
			return dir
		}
	}
	return fallback
}

func claudePrincipal(org, account string) string {
	if org == "" || account == "" {
		return ""
	}
	return org + "/" + account
}
