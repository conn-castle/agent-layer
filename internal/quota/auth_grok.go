package quota

import (
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/conn-castle/agent-layer/internal/clients/grok"
)

const grokBase = "https://cli-chat-proxy.grok.com/v1"

type grokSession struct {
	Key       string          `json:"key"`
	Mode      string          `json:"auth_mode"`
	User      string          `json:"user_id"`
	Principal string          `json:"principal_id"`
	Team      string          `json:"team_id"`
	Refresh   string          `json:"refresh_token"`
	Expires   json.RawMessage `json:"expires_at"`
	Issuer    string          `json:"oidc_issuer"`
	Client    string          `json:"oidc_client_id"`
}

func grokCredential(s credentialSource) (credential, failure) {
	if s.file != "" {
		return grokFileCredential(s.file, "")
	}
	home := s.value("GROK_HOME")
	settings, fail := grokSettings(home)
	if fail.status != "" {
		return credential{}, fail
	}
	options := table(settings, "grok_com_config")
	endpoints := table(settings, "endpoints")
	authConfig := table(settings, "auth")
	if s.has("GROK_DEPLOYMENT_KEY") || setting(endpoints, "deployment_key") != "" ||
		customEndpoint(textFrom(s.value("GROK_CLI_CHAT_PROXY_BASE_URL"), setting(endpoints, "cli_chat_proxy_base_url")), grokBase) {
		return credential{}, unsupportedAuth(reasonCustomEndpoint)
	}
	if textFrom(s.value("GROK_MODELS_BASE_URL"), setting(endpoints, "models_base_url")) != "" ||
		customEndpoint(textFrom(s.value("GROK_XAI_API_BASE_URL"), setting(endpoints, "xai_api_base_url")), "https://api.x.ai/v1") {
		return credential{}, nonSubscription("custom_model_endpoint")
	}
	if textFrom(s.value("GROK_AUTH_PROVIDER_COMMAND"), setting(authConfig, "auth_provider_command"), setting(options, "auth_provider_command")) != "" {
		return credential{}, unsupportedAuth(reasonCustomAuth)
	}
	oidc := table(options, "oidc")
	issuer := textFrom(s.value("GROK_OIDC_ISSUER"), setting(oidc, "issuer"), grokIssuer)
	if strings.TrimRight(issuer, "/") != grokIssuer {
		return credential{}, unsupportedAuth("enterprise_oidc")
	}
	modelAuth := grokModelAuth(settings)
	model := textFrom(s.model, setting(table(settings, "models"), "default"))
	if mode := modelAuth[model]; mode != "" {
		if mode == statusNonSubscription {
			return credential{}, nonSubscription("model_api_key")
		}
		return credential{}, unsupportedAuth("model_custom_auth")
	}
	client := textFrom(s.value("GROK_OIDC_CLIENT_ID"), setting(oidc, "client_id"))
	var auth credential
	if inline := s.value("GROK_AUTH"); inline != "" {
		var data grokSession
		if json.Unmarshal([]byte(inline), &data) != nil {
			return credential{}, unsupportedAuth(reasonCustomAuth)
		}
		if client != "" && data.Client != client {
			return credential{}, unsupportedAuth(reasonCustomAuth)
		}
		auth, fail = parseGrokSession(data)
	} else {
		path := filepath.Join(home, authFilename)
		if override := s.value("GROK_AUTH_PATH"); override != "" {
			path = s.absolute(override)
		}
		auth, fail = grokFileCredential(path, client)
		auth.native = s.native(filepath.Dir(path), grok.EnvAuthPath, path)
	}
	// The installed CLI uses an active session ahead of an API-key fallback.
	if fail.reason == reasonMissingCredential && s.has("XAI_API_KEY", "GROK_CODE_XAI_API_KEY") {
		return credential{}, nonSubscription(reasonAPIKey)
	}
	auth.modelAuth = modelAuth
	return auth, fail
}

func grokSettings(home string) (map[string]any, failure) {
	var result map[string]any
	for _, name := range []string{"managed_config.toml", nativeConfig, "requirements.toml"} {
		var layer map[string]any
		if fail := settingsFile(filepath.Join(home, name), &layer); fail.status != "" {
			return nil, fail
		}
		result = mergeSettings(result, layer)
	}
	return result, failure{}
}

func grokModelAuth(settings map[string]any) map[string]string {
	models := table(settings, "model")
	modes := make(map[string]string)
	for id := range models {
		model := table(models, id)
		switch {
		case setting(model, "auth_provider") != "" || setting(model, "model_provider") != "":
			modes[id] = statusUnsupported
		case setting(model, reasonAPIKey) != "" || model["env_key"] != nil ||
			setting(model, "base_url") != "" || setting(model, "api_base_url") != "" || model["extra_headers"] != nil || model["env_http_headers"] != nil:
			modes[id] = statusNonSubscription
		}
	}
	return modes
}

func grokFileCredential(path, client string) (credential, failure) {
	raw, fail := credentialFile(path)
	if fail.status != "" {
		return credential{}, fail
	}
	var entries map[string]grokSession
	if json.Unmarshal(raw, &entries) != nil {
		return credential{}, invalidCredentials()
	}
	if client != "" {
		if chosen, ok := entries[grokIssuer+"::"+client]; ok {
			if chosen.Client != "" && chosen.Client != client || customEndpoint(chosen.Issuer, grokIssuer) {
				return credential{}, invalidCredentials()
			}
			return parseGrokSession(chosen)
		}
		return credential{}, missingCredentials()
	}
	var chosen *grokSession
	for slot, entry := range entries {
		issuer, id, ok := strings.Cut(slot, "::")
		if !ok || issuer != grokIssuer || id == "" || entry.Client != "" && entry.Client != id || customEndpoint(entry.Issuer, grokIssuer) {
			continue
		}
		if chosen != nil {
			return credential{}, unsupportedAuth("ambiguous_credential_slot")
		}
		chosen = ptr(entry)
	}
	if chosen == nil {
		if len(entries) > 0 {
			return credential{}, unsupportedAuth(reasonCustomAuth)
		}
		return credential{}, missingCredentials()
	}
	return parseGrokSession(*chosen)
}

func parseGrokSession(data grokSession) (credential, failure) {
	switch data.Mode {
	case reasonAPIKey, "apikey":
		return credential{}, nonSubscription(reasonAPIKey)
	case "oidc":
		if customEndpoint(data.Issuer, grokIssuer) {
			return credential{}, unsupportedAuth("enterprise_oidc")
		}
	default:
		return credential{}, unsupportedAuth(reasonCustomAuth)
	}
	if data.Key == "" {
		return credential{}, invalidCredentials()
	}
	return credential{
		token: data.Key, expires: optionalTime(data.Expires), refreshable: data.Refresh != "",
		identity: subscription(agentGrok, grokPrincipal(data.User, data.Principal, data.Team), "", false),
	}, failure{}
}

func grokPrincipal(user, principal, team string) string {
	id := textFrom(principal, user)
	if id == "" {
		return ""
	}
	if team != "" {
		return team + "/" + id
	}
	return id
}
