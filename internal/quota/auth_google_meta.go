package quota

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"runtime"
)

const (
	agyProfileDirectory = "antigravity-cli"
	museBase            = "https://api.meta.ai"
	museKeychainService = "ai.meta.dev.credentials"
)

func antigravityCredential(ctx context.Context, s credentialSource) (credential, failure) {
	var useCredits *bool
	path := s.file
	if path == "" {
		if s.has("CLOUD_CODE_PA_ENDPOINT", "CLOUD_CODE_PA_BASE_URL") {
			return credential{}, unsupportedAuth(reasonCustomEndpoint)
		}
		profile := filepath.Join(s.root, ".agy", agyProfileDirectory)
		var rawSettings map[string]any
		if fail := settingsFile(filepath.Join(profile, "settings.json"), &rawSettings); fail.status != "" {
			return credential{}, fail
		}
		rawSettings = mergeSettings(rawSettings, s.config)
		var settings struct {
			UseCredits    *bool  `json:"useG1Credits"`
			ModelProvider string `json:"modelProvider"`
			Auth          struct {
				Type string `json:"type"`
			} `json:"auth"`
			Security struct {
				Auth struct {
					Selected string `json:"selectedType"`
				} `json:"auth"`
			} `json:"security"`
		}
		if len(rawSettings) > 0 {
			encoded, err := json.Marshal(rawSettings)
			if err != nil || json.Unmarshal(encoded, &settings) != nil {
				return credential{}, unsupportedAuth("provider_settings_unreadable")
			}
		}
		if settings.ModelProvider == "gemini" {
			return credential{}, nonSubscription(reasonAPIKey)
		}
		switch textFrom(settings.Auth.Type, settings.Security.Auth.Selected) {
		case "gemini-api-key":
			return credential{}, nonSubscription(reasonAPIKey)
		case "", "oauth-personal", "consumer", "google-account":
		default:
			return credential{}, unsupportedAuth(reasonCustomAuth)
		}
		path = filepath.Join(profile, "antigravity-oauth-token")
		useCredits = settings.UseCredits
	}
	raw, fail := credentialFile(path)
	lockDir := filepath.Dir(path)
	if s.file == "" && fail.reason == reasonMissingCredential {
		raw, fail = readKeyring(ctx, "gemini", agentAntigravity)
		lockDir = s.home("", "")
	}
	if fail.status != "" {
		return credential{}, fail
	}
	var data struct {
		Method string `json:"auth_method"`
		Email  string `json:"id_token_email"`
		Token  struct {
			Access  string          `json:"access_token"`
			Refresh string          `json:"refresh_token"`
			Expires json.RawMessage `json:"expiry"`
		} `json:"token"`
	}
	if json.Unmarshal(raw, &data) != nil {
		return credential{}, invalidCredentials()
	}
	if data.Method != "consumer" {
		return credential{}, unsupportedAuth(reasonCustomAuth)
	}
	if data.Token.Access == "" {
		return credential{}, invalidCredentials()
	}
	return credential{
		token: data.Token.Access, expires: optionalTime(data.Token.Expires), refreshable: data.Token.Refresh != "",
		identity: subscription(agentAntigravity, data.Email, "", false), useCredits: useCredits,
		native: s.native(lockDir, "", ""),
	}, failure{}
}

func museCredential(ctx context.Context, s credentialSource) (credential, failure) {
	return museCredentialFrom(ctx, s, runtime.GOOS, readKeyring)
}

func museCredentialFrom(ctx context.Context, s credentialSource, platform string, read keyringReader) (credential, failure) {
	path := s.file
	allowKeychain := false
	if path == "" {
		if s.has("META_API_KEY") {
			return credential{}, nonSubscription(reasonAPIKey)
		}
		if customEndpoint(s.value("TBH_MINT_BASE_URL"), museBase) ||
			s.has("META_API_BASE_URL", "MUSE_API_BASE_URL", "TBH_API_BASE_URL") {
			return credential{}, unsupportedAuth(reasonCustomEndpoint)
		}
		switch backend := s.value("TBH_CREDENTIAL_BACKEND"); backend {
		case "", credentialStoreFile:
			allowKeychain = platform == platformDarwin && backend == ""
		case "keychain":
			if platform != platformDarwin {
				return credential{}, unsupportedAuth(keyringUnavailable)
			}
			allowKeychain = true
		default:
			return credential{}, unsupportedAuth(reasonCustomAuth)
		}
		home := s.home("XDG_CONFIG_HOME", ".config")
		if home == "" {
			return credential{}, unsupportedAuth(reasonCredentialPath)
		}
		path = filepath.Join(home, agentMuse, authFilename)
	}
	if ctx.Err() != nil {
		return credential{}, contextFailure(ctx)
	}
	raw, fail := credentialFile(path)
	if fail.status != "" {
		return credential{}, fail
	}
	var data struct {
		Providers struct {
			Meta struct {
				museSecrets
				Base      string          `json:"api_base_url"`
				Mechanism string          `json:"mechanism"`
				Storage   string          `json:"storage"`
				Email     string          `json:"user_email"`
				Expires   json.RawMessage `json:"expires_at"`
			} `json:"meta"`
		} `json:"providers"`
	}
	if json.Unmarshal(raw, &data) != nil {
		return credential{}, invalidCredentials()
	}
	meta := data.Providers.Meta
	lockDir := filepath.Dir(path)
	if meta.Storage == "keychain" {
		if !allowKeychain {
			return credential{}, unsupportedAuth(keyringUnavailable)
		}
		secrets, fail := readMuseKeychain(ctx, read)
		if fail.status != "" {
			return credential{}, fail
		}
		meta.museSecrets = secrets
		lockDir = s.home("", "")
	}
	if meta.Mechanism != "oauth" {
		if meta.Key != "" {
			return credential{}, nonSubscription(reasonAPIKey)
		}
		return credential{}, missingCredentials()
	}
	if meta.Access == "" {
		return credential{}, invalidCredentials()
	}
	if meta.Base != "" && customEndpoint(meta.Base, museBase) && customEndpoint(meta.Base, museBase+"/v1") {
		return credential{}, unsupportedAuth(reasonCustomEndpoint)
	}
	expires := optionalTime(meta.Expires)
	if expires == nil {
		expires = jwtExpiry(meta.Access)
	}
	return credential{
		token: meta.Access, expires: expires, refreshable: meta.Refresh != "",
		identity: subscription(agentMuse, meta.Email, "", false),
		native:   s.native(lockDir, "XDG_CONFIG_HOME", filepath.Dir(filepath.Dir(path))),
	}, failure{}
}

type museSecrets struct {
	Access  string `json:"access_token"`
	Key     string `json:"api_key"`
	Refresh string `json:"refresh_token"`
}

func readMuseKeychain(ctx context.Context, read keyringReader) (museSecrets, failure) {
	// Muse's auth.json selects the Meta slot and keeps its account metadata.
	// The platform store holds the secret fields for that slot.
	raw, fail := read(ctx, museKeychainService, "meta")
	if fail.reason == reasonMissingCredential {
		return museSecrets{}, unsupportedAuth("muse_keychain_entry_unavailable")
	}
	if fail.status != "" {
		return museSecrets{}, fail
	}
	// Apple's security tool encodes passwords containing control bytes as hex.
	if len(raw) > 0 && raw[0] != '{' {
		if decoded, err := hex.DecodeString(string(raw)); err == nil {
			raw = decoded
		}
	}
	var data struct {
		museSecrets
		Version int `json:"secret_schema_version"`
	}
	if len(raw) > 16<<10 || json.Unmarshal(raw, &data) != nil {
		return museSecrets{}, invalidCredentials()
	}
	if data.Version != 1 {
		return museSecrets{}, unsupportedAuth("muse_keychain_version_unsupported")
	}
	return data.museSecrets, failure{}
}
