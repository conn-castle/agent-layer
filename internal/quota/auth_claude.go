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

// claudeCredentialFrom loads Claude credentials for s. A nil read indicates
// platform Keychain storage is unavailable (non-Darwin).
func claudeCredentialFrom(ctx context.Context, s credentialSource, read keyringReader) (credential, failure) {
	if s.file != "" {
		return claudeFileCredential(s.file)
	}
	home := s.home(claudeConfigEnv, ".claude")
	if home == "" {
		return credential{}, unsupportedAuth(reasonCredentialPath)
	}
	env, settings, failed := claudeLaunchSettings(s, home, false)
	if failed.status != "" {
		return credential{}, failed
	}
	nativeConfigDir := s.value(claudeConfigEnv)
	token, failed := claudeValidateSettings(s, home, nativeConfigDir, env, settings)
	if failed.status != "" {
		return credential{}, failed
	}
	if token != "" {
		return credential{token: token, expires: jwtExpiry(token), renewalUnknown: true}, failure{}
	}
	canRenew := claudeDiskSettingsAllowRenewal(s, home, nativeConfigDir, env)
	s.env = env
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
			if canRenew {
				// Keychain service names hash only four SHA-256 bytes of the raw
				// selector string, so serialize all macOS Keychain renewals on one
				// stable per-OS-user lock directory independent of request HOME.
				auth.native = s.native(osUserLockDir(), "", "")
			}
			return auth, fail
		}
		keychainFailure = fail
	}
	path := filepath.Join(home, claudeCredentials)
	auth, fail := claudeFileCredential(path)
	if fail.reason == reasonMissingCredential && keychainFailure.status != "" {
		return credential{}, keychainFailure
	}
	if canRenew && (keychainFailure.status == "" || keychainFailure.reason == reasonMissingCredential) {
		auth.native = s.native(home, "", "")
	}
	return auth, fail
}

func claudeValidateSettings(s credentialSource, home, nativeConfigDir string, env []string, settings map[string]any) (string, failure) {
	s.env = env
	if s.enabledFlag("CLAUDE_CODE_USE_BEDROCK", "CLAUDE_CODE_USE_VERTEX", "CLAUDE_CODE_USE_FOUNDRY") ||
		s.has("CLAUDE_CODE_CUSTOM_OAUTH_URL") ||
		customEndpoint(s.value("ANTHROPIC_BASE_URL"), "https://api.anthropic.com") {
		return "", unsupportedAuth(reasonCustomEndpoint)
	}
	if s.has("ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN") {
		return "", nonSubscription("api_key_or_gateway_token")
	}
	if s.has("CLAUDE_CODE_API_KEY_FILE_DESCRIPTOR") || setting(settings, "apiKeyHelper") != "" {
		return "", unsupportedAuth(reasonCustomAuth)
	}
	if token := s.value("CLAUDE_CODE_OAUTH_TOKEN"); token != "" {
		return token, failure{}
	}
	if s.has("CLAUDE_CODE_OAUTH_TOKEN_FILE_DESCRIPTOR", "ANTHROPIC_PROFILE", "ANTHROPIC_FEDERATION_RULE_ID") ||
		setting(settings, "forceLoginGatewayUrl") != "" || setting(settings, "forceLoginMethod") == "gateway" {
		return "", unsupportedAuth(reasonCustomAuth)
	}
	// Claude discovers its configuration home before loading its settings.
	// A settings-level home override makes a file-backed login ambiguous.
	if nativeConfigDir != s.value(claudeConfigEnv) || !clients.SamePath(home, s.home(claudeConfigEnv, ".claude")) {
		return "", unsupportedAuth(reasonCustomAuth)
	}
	return "", failure{}
}

func claudeDiskSettingsAllowRenewal(s credentialSource, home, nativeConfigDir string, projectedEnv []string) bool {
	diskEnv, diskSettings, failed := claudeLaunchSettings(s, home, true)
	if failed.status != "" {
		return false
	}
	diskToken, failed := claudeValidateSettings(s, home, nativeConfigDir, diskEnv, diskSettings)
	if failed.status != "" || diskToken != "" {
		return false
	}
	projSecure, projHas := clients.GetEnv(projectedEnv, "CLAUDE_SECURESTORAGE_CONFIG_DIR")
	diskSecure, diskHas := clients.GetEnv(diskEnv, "CLAUDE_SECURESTORAGE_CONFIG_DIR")
	return projHas == diskHas && projSecure == diskSecure
}

func claudeLaunchSettings(s credentialSource, home string, useDiskProject bool) ([]string, map[string]any, failure) {
	var settings map[string]any
	if fail := settingsFile(filepath.Join(home, "settings.json"), &settings); fail.status != "" {
		return nil, nil, fail
	}
	project := filepath.Join(s.root, ".claude")
	workspace := filepath.Join(textFrom(s.workDir, s.root), ".claude")
	if useDiskProject {
		var layer map[string]any
		if fail := settingsFile(filepath.Join(project, "settings.json"), &layer); fail.status != "" {
			return nil, nil, fail
		}
		settings = mergeSettings(settings, layer)
	} else {
		// Dispatch regenerates <root>/.claude/settings.json from project config,
		// so resolve that layer from the current config rather than a stale file.
		settings = mergeSettings(settings, s.config)
	}
	var paths []string
	if workspace != project {
		paths = append(paths, filepath.Join(workspace, "settings.json"))
	}
	paths = append(paths, filepath.Join(project, "settings.local.json"))
	if workspace != project {
		paths = append(paths, filepath.Join(workspace, "settings.local.json"))
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
		token: oauth.Access, account: oauth.Account, expires: millis(oauth.Expires), refreshable: oauth.Refresh != "",
		refreshExpires: millis(oauth.RefreshExpires),
		identity:       subscription(agentClaude, claudePrincipal(oauth.Organization, oauth.Account), oauth.Tier, false),
	}, failure{}
}

var osUserHome = func() string {
	if current, err := user.Current(); err == nil {
		return current.HomeDir
	}
	return ""
}

func osUserLockDir() string {
	return existingDirOrFallback(osUserHome(), "")
}

func claudePrincipal(org, account string) string {
	if org == "" || account == "" {
		return ""
	}
	return org + "/" + account
}
