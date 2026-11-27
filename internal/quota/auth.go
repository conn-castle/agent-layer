package quota

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"

	"github.com/conn-castle/agent-layer/internal/clients"
	"github.com/conn-castle/agent-layer/internal/clients/claude"
	"github.com/conn-castle/agent-layer/internal/clients/codex"
	"github.com/conn-castle/agent-layer/internal/clients/grok"
	"github.com/conn-castle/agent-layer/internal/fsutil"
	"github.com/conn-castle/agent-layer/internal/root"
)

const (
	jsonExt                  = ".json"
	authFilename             = "auth.json"
	nativeConfig             = "config.toml"
	claudeCredentials        = ".credentials.json"
	claudeConfigEnv          = "CLAUDE_CONFIG_DIR"
	codexHomeEnv             = "CODEX_HOME"
	codexChatGPT             = "chatgpt"
	grokIssuer               = "https://auth.x.ai"
	keyringUnavailable       = "credential_store_unavailable"
	credentialStoreFile      = "file"
	credentialStoreAuto      = "auto"
	keyringServiceAttr       = "service"
	keyringUsernameAttr      = "username"
	flagYes                  = "yes"
	reasonExpiredCredentials = "expired_credentials"       // #nosec G101 -- public result reason.
	reasonAccessExpired      = "access_token_expired"      // #nosec G101 -- public result reason.
	reasonScopeMismatch      = "credential_scope_mismatch" // #nosec G101 -- public result reason.
)

type credential struct {
	token          string
	account        string
	workspaceClaim string
	expires        *time.Time
	refreshExpires *time.Time
	refreshable    bool
	renewalUnknown bool
	identity       *Subscription
	modelAuth      map[string]string
	useCredits     *bool
	native         *nativeCredential
}

func (c credential) expired(now time.Time) bool {
	return c.expires != nil && !now.Before(*c.expires)
}

func (c credential) authenticationFailure(reason string, now time.Time) failure {
	hasRenewal := c.canRenew(now)
	if reason == reasonExpiredCredentials {
		if !hasRenewal && !c.renewalUnknown {
			return failure{status: statusLoginExpired, reason: reason, reauth: ptr(true)}
		}
		fail := failure{status: statusCredentials, reason: reasonAccessExpired}
		if hasRenewal && c.refreshExpires != nil {
			fail.reauth = ptr(false)
		}
		return fail
	}
	if hasRenewal || c.renewalUnknown {
		// Rejection alone does not establish whether the refresh grant survives.
		return failure{status: statusCredentials, reason: reason}
	}
	if reason == credentialRevoked {
		return failure{status: statusLoginExpired, reason: reason, reauth: ptr(true)}
	}
	return failure{status: statusCredentials, reason: reason, reauth: ptr(true)}
}

func (c credential) canRenew(now time.Time) bool {
	return c.refreshable && (c.refreshExpires == nil || now.Before(*c.refreshExpires))
}

func missingCredentials() failure {
	return failure{status: statusCredentials, reason: reasonMissingCredential, reauth: ptr(true)}
}

func invalidCredentials() failure {
	return failure{status: statusCredentials, reason: "invalid_credentials", reauth: ptr(true)}
}

func nonSubscription(reason string) failure {
	return failure{status: statusNonSubscription, reason: reason, reauth: ptr(false)}
}

func unsupportedAuth(reason string) failure {
	return failure{status: statusUnsupported, reason: reason}
}

type credentialSource struct {
	root, workDir, file, model string
	env                        []string
	config                     map[string]any
}

func loadCredential(ctx context.Context, req Request, source Source) (credential, failure) {
	if ctx.Err() != nil {
		return credential{}, contextFailure(ctx)
	}
	s := credentialSource{file: source.File, workDir: req.WorkDir}
	if source.File == "" {
		s.root = req.Project.Root
		s.model = source.ConfiguredModel
		s.env = clients.BuildEnv(req.Env, req.Project.Env, nil)
		switch source.Agent {
		case agentCodex:
			s.env = codex.ConfigureEnvironment(s.root, s.env, req.Project.Config.Agents.Codex, nil)
			s.config = req.Project.Config.Agents.Codex.AgentSpecific
		case agentClaude:
			s.env = claude.ConfigureEnvironment(s.root, s.env, req.Project.Config.Agents.Claude, nil)
			s.config = req.Project.Config.Agents.Claude.AgentSpecific
		case agentGrok:
			s.env = grok.ConfigureEnvironment(s.root, s.env, req.Project.Config.Agents.Grok, nil)
		case agentAntigravity:
			s.config = req.Project.Config.Agents.Antigravity.AgentSpecific
		}
	}
	var auth credential
	var fail failure
	switch source.Agent {
	case agentCodex:
		auth, fail = codexCredential(ctx, s)
	case agentClaude:
		auth, fail = claudeCredential(ctx, s)
	case agentGrok:
		auth, fail = grokCredential(s)
	case agentAntigravity:
		auth, fail = antigravityCredential(ctx, s)
	case agentMuse:
		auth, fail = museCredential(ctx, s)
	}
	if ctx.Err() != nil {
		return credential{}, contextFailure(ctx)
	}
	return auth, fail
}

func (s credentialSource) value(key string) string {
	value, _ := clients.GetEnv(s.env, key)
	return value
}

func (s credentialSource) has(names ...string) bool {
	for _, name := range names {
		if s.value(name) != "" {
			return true
		}
	}
	return false
}

func (s credentialSource) enabledFlag(names ...string) bool {
	for _, name := range names {
		switch strings.ToLower(strings.TrimSpace(s.value(name))) {
		case "1", "true", flagYes, "on":
			return true
		}
	}
	return false
}

func existingDirOrFallback(dir, fallback string) string {
	if dir != "" {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return dir
		}
	}
	return fallback
}

func (s credentialSource) home(override, defaultDirectory string) string {
	if value := s.value(override); value != "" {
		return s.absolute(value)
	}
	if value := s.value("HOME"); value != "" {
		return filepath.Join(s.absolute(value), defaultDirectory)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, defaultDirectory)
}

func (s credentialSource) absolute(path string) string {
	if filepath.IsAbs(path) || s.workDir == "" {
		return clients.ResolveHome(path)
	}
	return clients.ResolvePath(s.workDir + string(os.PathSeparator) + path)
}

func readBoundedFile(path string) ([]byte, error) {
	file, err := fsutil.OpenRegularFile(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	raw, err := io.ReadAll(io.LimitReader(file, maxPayloadSize+1))
	if len(raw) > maxPayloadSize {
		return nil, errors.New("file exceeds read limit")
	}
	return raw, err
}

func credentialFile(path string) ([]byte, failure) {
	if path == "" {
		return nil, unsupportedAuth(reasonCredentialPath)
	}
	raw, err := readBoundedFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return nil, missingCredentials()
	case err != nil:
		return nil, failure{status: statusCredentials, reason: "credential_file_unreadable"}
	default:
		return raw, failure{}
	}
}

func settingsFile(path string, output any) failure {
	raw, err := readBoundedFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return failure{}
	}
	if err == nil {
		if filepath.Ext(path) == jsonExt {
			err = json.Unmarshal(raw, output)
		} else {
			err = toml.Unmarshal(raw, output)
		}
	}
	if err != nil {
		return unsupportedAuth("provider_settings_unreadable")
	}
	return failure{}
}

func table(values map[string]any, key string) map[string]any {
	out, _ := values[key].(map[string]any)
	return out
}

func setting(values map[string]any, key string) string {
	out, _ := values[key].(string)
	return out
}

func textFrom(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func decodeJWTClaims(token string, claims any) bool {
	_, rest, ok := strings.Cut(token, ".")
	payload, _, end := strings.Cut(rest, ".")
	if !ok || !end {
		return false
	}
	raw, err := base64.RawURLEncoding.DecodeString(payload)
	return err == nil && json.Unmarshal(raw, claims) == nil
}

func jwtExpiry(token string) *time.Time {
	var data struct{ Exp int64 }
	if !decodeJWTClaims(token, &data) {
		return nil
	}
	// Unverified claims check local eligibility. The provider verifies identity.
	return unixSeconds(data.Exp)
}

func readKeyring(ctx context.Context, service, account string) ([]byte, failure) {
	var path string
	var args []string
	lookupTimeout := 2 * time.Second
	switch runtime.GOOS {
	case platformDarwin:
		path = "/usr/bin/security"
		// Allow time to answer the system's one-time access prompt.
		lookupTimeout = 10 * time.Second
		args = []string{"find-generic-password", "-s", service, "-w"}
		if account != "" {
			args = append(args, "-a", account)
		}
	case "linux":
		path = "/usr/bin/secret-tool"
		args = []string{"lookup", keyringServiceAttr, service, keyringUsernameAttr, account}
	default:
		return nil, unsupportedAuth(keyringUnavailable)
	}
	// Only the OS-installed executable and fixed read operations are used.
	callCtx, cancel := context.WithTimeout(ctx, lookupTimeout)
	defer cancel()
	cmd := exec.CommandContext(callCtx, path, args...) // #nosec G204 -- absolute OS utility paths and fixed lookup arguments.
	cmd.WaitDelay = 100 * time.Millisecond
	var output cappedWriter
	cmd.Stdout = &output
	if err := cmd.Run(); err != nil || output.Len() == 0 {
		if ctx.Err() != nil {
			return nil, contextFailure(ctx)
		}
		if callCtx.Err() != nil {
			return nil, unsupportedAuth("credential_store_timeout")
		}
		var status *exec.ExitError
		// The security tool returns errSecItemNotFound (-25300) as its
		// eight-bit process exit status. Other errors do not prove absence.
		if runtime.GOOS == platformDarwin && errors.As(err, &status) && status.ExitCode() == 44 {
			return nil, missingCredentials()
		}
		return nil, unsupportedAuth(keyringUnavailable)
	}
	raw := bytes.TrimSpace(output.Bytes())
	const prefix = "go-keyring-base64:"
	if bytes.HasPrefix(raw, []byte(prefix)) {
		decoded, err := base64.StdEncoding.DecodeString(string(raw[len(prefix):]))
		if err != nil {
			return nil, invalidCredentials()
		}
		raw = decoded
	}
	return raw, failure{}
}

type cappedWriter struct{ buf bytes.Buffer }

func (w *cappedWriter) Write(data []byte) (int, error) {
	if len(data) > maxPayloadSize-w.buf.Len() {
		return 0, errors.New("keyring output exceeds limit")
	}
	return w.buf.Write(data)
}

func (w *cappedWriter) Len() int      { return w.buf.Len() }
func (w *cappedWriter) Bytes() []byte { return w.buf.Bytes() }

func codexCredential(ctx context.Context, s credentialSource) (credential, failure) {
	if s.file != "" {
		var raw []byte
		var fail failure
		if filepath.Base(s.file) == codexVaultFilename && filepath.Base(filepath.Dir(s.file)) == codexSecretsDirectory {
			raw, fail = readCodexVault(ctx, s.file, filepath.Dir(filepath.Dir(s.file)), readCodexKeyring)
		} else {
			raw, fail = credentialFile(s.file)
		}
		if fail.status != "" {
			return credential{}, fail
		}
		return parseCodexCredential(raw)
	}
	settings, fail := codexLaunchSettings(s)
	if fail.status != "" {
		return credential{}, fail
	}
	raw, fail := codexStoredCredential(ctx, settings, readCodexKeyring)
	if fail.status != "" {
		return credential{}, fail
	}
	auth, fail := parseCodexCredential(raw)
	if fail.status != "" {
		if settings.forceChatGPT && fail.status == statusNonSubscription {
			return credential{}, failure{status: statusCredentials, reason: "codex_chatgpt_login_required", reauth: ptr(true)}
		}
		return credential{}, fail
	}
	if settings.workspaces != nil && (!slices.Contains(settings.workspaces, auth.workspaceClaim) ||
		auth.account != "" && auth.account != auth.workspaceClaim) {
		return credential{}, failure{status: statusCredentials, reason: "credential_workspace_mismatch", reauth: ptr(true)}
	}
	lockDir := settings.home
	if settings.store == "keyring" || settings.store == credentialStoreAuto {
		lockDir = existingDirOrFallback(lockDir, osUserLockDir())
	}
	auth.native = s.native(lockDir, codexHomeEnv, settings.home)
	return auth, failure{}
}

func parseCodexCredential(raw []byte) (credential, failure) {
	var data struct {
		Mode   string `json:"auth_mode"`
		APIKey string `json:"OPENAI_API_KEY"`
		Tokens struct {
			Access  string `json:"access_token"`
			IDToken string `json:"id_token"`
			Refresh string `json:"refresh_token"`
			Account string `json:"account_id"`
		} `json:"tokens"`
	}
	if json.Unmarshal(raw, &data) != nil {
		return credential{}, invalidCredentials()
	}
	switch data.Mode {
	case "apikey", reasonAPIKey:
		return credential{}, nonSubscription(reasonAPIKey)
	case "":
		if data.APIKey != "" {
			return credential{}, nonSubscription(reasonAPIKey)
		}
	case codexChatGPT, "chatgptAuthTokens", "chatgpt_auth_tokens":
	default:
		return credential{}, unsupportedAuth(reasonCustomAuth)
	}
	if data.Tokens.Access == "" {
		return credential{}, invalidCredentials()
	}
	var claims struct {
		Auth struct {
			Account string `json:"chatgpt_account_id"`
		} `json:"https://api.openai.com/auth"`
	}
	_ = decodeJWTClaims(data.Tokens.IDToken, &claims)
	return credential{
		token: data.Tokens.Access, account: data.Tokens.Account, workspaceClaim: claims.Auth.Account,
		expires: jwtExpiry(data.Tokens.Access), refreshable: data.Tokens.Refresh != "" &&
			(data.Mode == "" || data.Mode == codexChatGPT),
		identity: subscription(agentCodex, data.Tokens.Account, "", false),
	}, failure{}
}

type codexSettings struct {
	home, store  string
	workspaces   []string
	forceChatGPT bool
	vault        bool
}

func codexLaunchSettings(s credentialSource) (codexSettings, failure) {
	home := s.home(codexHomeEnv, codex.HomeDirName)
	if home == "" {
		return codexSettings{}, unsupportedAuth(reasonCredentialPath)
	}
	var system, native map[string]any
	if runtime.GOOS != "windows" {
		if fail := settingsFile("/etc/codex/config.toml", &system); fail.status != "" {
			return codexSettings{}, fail
		}
	}
	if fail := settingsFile(filepath.Join(home, nativeConfig), &native); fail.status != "" {
		return codexSettings{}, fail
	}
	// The managed project file serves as user config only when it is also
	// CODEX_HOME. Otherwise Codex loads it as a project layer if trusted.
	if clients.SamePath(home, filepath.Join(s.root, codex.HomeDirName)) {
		native = mergeSettings(native, s.config)
	}
	native = mergeSettings(system, native)
	profile, ok := codexProfile(native)
	if !ok {
		return codexSettings{}, unsupportedAuth(reasonCustomAuth)
	}
	native = mergeSettings(native, profile)
	native, fail := codexProjectSettings(s, home, native)
	if fail.status != "" {
		return codexSettings{}, fail
	}
	return parseCodexSettings(s, home, native)
}

func codexProfile(native map[string]any) (map[string]any, bool) {
	raw, exists := native["profile"]
	if !exists {
		return nil, true
	}
	id, ok := raw.(string)
	profile := table(table(native, "profiles"), id)
	return profile, ok && profile != nil
}

func parseCodexSettings(s credentialSource, home string, native map[string]any) (codexSettings, failure) {
	for _, key := range []string{"forced_login_method", "chatgpt_base_url", "model_provider", "openai_base_url", "cli_auth_credentials_store"} {
		if value, exists := native[key]; exists {
			if _, ok := value.(string); !ok {
				return codexSettings{}, unsupportedAuth(reasonCustomAuth)
			}
		}
	}
	method := setting(native, "forced_login_method")
	switch method {
	case "api":
		return codexSettings{}, nonSubscription(reasonAPIKey)
	case "", codexChatGPT:
	default:
		return codexSettings{}, unsupportedAuth(reasonCustomAuth)
	}
	if customEndpoint(setting(native, "chatgpt_base_url"), "https://chatgpt.com/backend-api") {
		return codexSettings{}, unsupportedAuth(reasonCustomEndpoint)
	}
	if selected := setting(native, "model_provider"); selected != "" && selected != "openai" {
		return codexSettings{}, unsupportedAuth("custom_model_provider")
	}
	if s.has("OPENAI_FEDERATION_RULE_ID", "OPENAI_IDENTITY_TOKEN_FILE", "CODEX_AUTH_TOKEN", "CODEX_PROFILE") {
		return codexSettings{}, unsupportedAuth(reasonCustomAuth)
	}
	if strings.TrimSpace(s.value("CODEX_API_KEY")) != "" {
		if method == codexChatGPT {
			// The supported Codex release selects the environment key, then
			// rejects it under forced ChatGPT. Avoid its logout side effect.
			return codexSettings{}, failure{status: statusCredentials, reason: "codex_login_method_conflict", reauth: ptr(false)}
		}
		return codexSettings{}, nonSubscription(reasonAPIKey)
	}
	if strings.TrimSpace(s.value("CODEX_ACCESS_TOKEN")) != "" {
		return codexSettings{}, unsupportedAuth(reasonCustomAuth)
	}
	if customEndpoint(textFrom(s.value("OPENAI_BASE_URL"), setting(native, "openai_base_url")), "https://api.openai.com/v1") {
		return codexSettings{}, unsupportedAuth(reasonCustomEndpoint)
	}
	workspaces, ok := codexWorkspaceIDs(native["forced_chatgpt_workspace_id"])
	if !ok {
		return codexSettings{}, unsupportedAuth(reasonCustomAuth)
	}
	vault := runtime.GOOS == "windows"
	if raw, exists := table(native, "features")["secret_auth_storage"]; exists {
		var ok bool
		if vault, ok = raw.(bool); !ok {
			return codexSettings{}, unsupportedAuth(reasonCustomAuth)
		}
	}
	return codexSettings{
		home: home, store: setting(native, "cli_auth_credentials_store"), workspaces: workspaces,
		forceChatGPT: method == codexChatGPT, vault: vault,
	}, failure{}
}

func codexWorkspaceIDs(raw any) ([]string, bool) {
	switch values := raw.(type) {
	case nil:
		return nil, true
	case string:
		return []string{values}, values != ""
	case []any:
		result := make([]string, 0, len(values))
		for _, value := range values {
			id, ok := value.(string)
			if !ok || id == "" {
				return nil, false
			}
			result = append(result, id)
		}
		return result, len(result) > 0
	default:
		return nil, false
	}
}

func codexProjectSettings(s credentialSource, home string, native map[string]any) (map[string]any, failure) {
	cwd := clients.ResolvePath(textFrom(s.workDir, s.root))
	projectRoot, found, err := root.FindGitRoot(cwd)
	if err != nil {
		return nil, unsupportedAuth("provider_settings_unreadable")
	}
	if !found {
		projectRoot = clients.ResolvePath(s.root)
	}
	dirs := []string{}
	for dir := cwd; ; dir = filepath.Dir(dir) {
		dirs = append(dirs, dir)
		if dir == projectRoot || filepath.Dir(dir) == dir {
			break
		}
	}
	slices.Reverse(dirs)
	projects := table(native, "projects")
	trust := setting(table(projects, projectRoot), "trust_level")
	for _, dir := range dirs {
		if explicit := setting(table(projects, dir), "trust_level"); explicit != "" {
			trust = explicit
		}
		folder := filepath.Join(dir, codex.HomeDirName)
		if trust != "trusted" || clients.SamePath(home, folder) {
			continue
		}
		var layer map[string]any
		if fail := settingsFile(filepath.Join(folder, nativeConfig), &layer); fail.status != "" {
			return nil, fail
		}
		if dir == clients.ResolvePath(s.root) {
			layer = mergeSettings(layer, s.config)
		}
		// These auth-routing fields are user/system-only in the native loader.
		for _, key := range []string{"openai_base_url", "chatgpt_base_url", "model_provider", "model_providers", "profile", "profiles"} {
			delete(layer, key)
		}
		native = mergeSettings(native, layer)
	}
	return native, failure{}
}

func codexStoredCredential(ctx context.Context, settings codexSettings, read keyringReader) ([]byte, failure) {
	home := settings.home
	switch settings.store {
	case "", credentialStoreFile:
		return credentialFile(filepath.Join(home, authFilename))
	case "keyring", credentialStoreAuto:
		var raw []byte
		var fail failure
		if settings.vault {
			raw, fail = readCodexVault(ctx, filepath.Join(home, codexSecretsDirectory, codexVaultFilename), home, read)
		} else {
			raw, fail = read(ctx, "Codex Auth", codexKeyringAccount(home, "cli"))
		}
		if fail.status == "" {
			return raw, failure{}
		}
		if settings.store == credentialStoreAuto && fail.reason == reasonMissingCredential {
			return credentialFile(filepath.Join(home, authFilename))
		}
		// Our helper cannot establish that Codex itself could not open an
		// inaccessible store. Falling back here could report an older login.
		return nil, fail
	default:
		return nil, unsupportedAuth(keyringUnavailable)
	}
}

func codexKeyringAccount(home, prefix string) string {
	home = clients.ResolvePath(home)
	sum := sha256.Sum256([]byte(strings.ToValidUTF8(home, "\uFFFD")))
	return fmt.Sprintf("%s|%x", prefix, sum[:8])
}

func customEndpoint(selected, official string) bool {
	return selected != "" && strings.TrimRight(selected, "/") != official
}

func mergeSettings(base, overlay map[string]any) map[string]any {
	if len(overlay) == 0 {
		return base
	}
	if base == nil {
		base = make(map[string]any, len(overlay))
	}
	for key, value := range overlay {
		if nested, ok := value.(map[string]any); ok {
			base[key] = mergeSettings(table(base, key), nested)
		} else {
			base[key] = value
		}
	}
	return base
}
