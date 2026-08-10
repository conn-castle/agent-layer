package benchmark

import (
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
	"sort"
	"strings"
	"unicode"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/envref"
)

const studyURLNameSegment = "name"

// studyMCPPreflight is deliberately derived from config.Config. It is a
// transport contract for the task container, not a benchmark-specific config
// model. Its values remain placeholder-bearing and therefore secret-free.
type studyMCPPreflight struct {
	Servers []studyMCPServer `json:"servers"`
}

type studyMCPServer struct {
	ID        string            `json:"id"`
	Transport string            `json:"transport"`
	URL       string            `json:"url,omitempty"`
	Headers   map[string]string `json:"headers,omitempty"`
	Command   string            `json:"command,omitempty"`
	Args      []string          `json:"args,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
}

func studyMCPContract(configBytes []byte, source string) (studyMCPPreflight, []string, error) {
	cfg, err := config.ParseConfig(configBytes, source)
	if err != nil {
		return studyMCPPreflight{}, nil, err
	}
	// A study uploads the complete effective Agent Layer configuration, not a
	// reduced MCP-only view. Validate provider-native passthrough values before
	// staging so a literal API key cannot reach the task image through an
	// agent_specific table that normal sync will project later.
	if err := rejectLiteralStudyConfigSecrets(cfg); err != nil {
		return studyMCPPreflight{}, nil, err
	}
	credentials := map[string]bool{}
	contract := studyMCPPreflight{}
	for _, server := range cfg.MCP.Servers {
		if server.Enabled == nil || !*server.Enabled {
			continue
		}
		if err := rejectLiteralMCPSecrets(server); err != nil {
			return studyMCPPreflight{}, nil, fmt.Errorf("mcp server %q: %w", server.ID, err)
		}
		values := append([]string{server.URL, server.Command}, server.Args...)
		values = append(values, mapValues(server.Headers)...)
		values = append(values, mapValues(server.Env)...)
		for _, value := range values {
			for _, name := range config.ExtractEnvVarNames(value) {
				if !config.IsBuiltInEnvVar(name) {
					credentials[name] = true
				}
			}
		}
		contract.Servers = append(contract.Servers, studyMCPServer{ID: server.ID, Transport: server.Transport, URL: server.URL, Headers: server.Headers, Command: server.Command, Args: server.Args, Env: server.Env})
	}
	for _, agent := range []struct {
		name    string
		enabled bool
		values  config.ProviderPassthrough
	}{
		{"antigravity", config.IsAgentEnabled(cfg.Agents.Antigravity.Enabled), cfg.Agents.Antigravity.AgentSpecific},
		{"claude", config.IsAgentEnabled(cfg.Agents.Claude.Enabled), cfg.Agents.Claude.AgentSpecific},
		{"codex", config.IsAgentEnabled(cfg.Agents.Codex.Enabled), cfg.Agents.Codex.AgentSpecific},
	} {
		if !agent.enabled {
			continue
		}
		for _, name := range placeholderNames(reflect.ValueOf(agent.values)) {
			if !config.IsBuiltInEnvVar(name) {
				credentials[name] = true
			}
		}
	}
	names := make([]string, 0, len(credentials))
	for name := range credentials {
		names = append(names, name)
	}
	sort.Strings(names)
	return contract, names, nil
}

// rejectLiteralStudyConfigSecrets walks the parsed effective configuration so
// it includes arbitrary provider-native maps under agents.*.agent_specific.
// It intentionally ties rejection to credential-bearing field names and URL
// query values: normal provider settings remain valid, while a value such as
// api_key = "literal" cannot be copied into a reproducible study bundle.
func rejectLiteralStudyConfigSecrets(cfg *config.Config) error {
	if cfg == nil {
		return fmt.Errorf("effective config is missing")
	}
	return walkStudyConfigValue(reflect.ValueOf(*cfg), "", false)
}

// walkStudyConfigValue retains credential context while descending raw
// provider-native values. A common representation is
// credentials = { value = "..." }; checking only the immediate key would
// incorrectly treat value as an ordinary setting and stage a literal secret.
func walkStudyConfigValue(value reflect.Value, path string, credentialContext bool) error {
	if !value.IsValid() {
		return nil
	}
	for value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return nil
		}
		value = value.Elem()
	}
	switch value.Kind() {
	case reflect.Struct:
		for index := range value.NumField() {
			field := value.Type().Field(index)
			if field.PkgPath != "" {
				continue
			}
			name := strings.Split(field.Tag.Get("toml"), ",")[0]
			if name == "" || name == "-" {
				name = field.Name
			}
			if err := walkStudyConfigValue(value.Field(index), joinStudyConfigPath(path, name), credentialContext || secretBearingName(name)); err != nil {
				return err
			}
		}
	case reflect.Map:
		iter := value.MapRange()
		for iter.Next() {
			key := fmt.Sprint(iter.Key().Interface())
			if err := walkStudyConfigValue(iter.Value(), joinStudyConfigPath(path, key), credentialContext || secretBearingName(key)); err != nil {
				return err
			}
		}
	case reflect.Slice, reflect.Array:
		for index := range value.Len() {
			valueContext := credentialContext
			if index > 0 {
				if previous, ok := studyConfigString(value.Index(index - 1)); ok {
					valueContext = valueContext || secretBearingFlag(previous)
				}
			}
			if err := walkStudyConfigValue(value.Index(index), fmt.Sprintf("%s[%d]", path, index), valueContext); err != nil {
				return err
			}
		}
	case reflect.String:
		return rejectLiteralStudyConfigString(path, value.String(), credentialContext)
	}
	return nil
}

func studyConfigString(value reflect.Value) (string, bool) {
	for value.IsValid() && (value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer) {
		if value.IsNil() {
			return "", false
		}
		value = value.Elem()
	}
	if !value.IsValid() || value.Kind() != reflect.String {
		return "", false
	}
	return value.String(), true
}

func joinStudyConfigPath(path, name string) string {
	if path == "" {
		return name
	}
	return path + "." + name
}

func rejectLiteralStudyConfigString(path, value string, credentialContext bool) error {
	if flag, inlineValue, hasInlineValue := strings.Cut(strings.TrimSpace(value), "="); hasInlineValue && secretBearingFlag(flag) {
		return rejectLiteralStudyConfigString(path, inlineValue, true)
	}
	field := path[strings.LastIndex(path, ".")+1:]
	urlBearing := studyURLName(field)
	if urlBearing {
		if key, found := envref.LiteralSecretQueryKey(value); found {
			return fmt.Errorf("%s contains literal credential-bearing URL query %q; use ${NAME}", path, key)
		}
		if parsed, err := url.Parse(strings.TrimSpace(value)); err == nil && parsed.User != nil {
			if username := parsed.User.Username(); username != "" && !envref.IsEntirelyPlaceholders(username) {
				return fmt.Errorf("%s contains a literal URL username; use ${NAME}", path)
			}
			if password, found := parsed.User.Password(); found && !envref.IsEntirelyPlaceholders(password) {
				return fmt.Errorf("%s contains a literal URL password; use ${NAME}", path)
			}
		}
		// OAuth token_endpoint, authorization_endpoint, and ordinary service
		// URLs are names, not secret values. Their query/userinfo checks above
		// are the only credential rule that applies to URL-bearing fields.
		return nil
	}
	if !credentialContext && !secretBearingName(field) {
		return nil
	}
	residual := strings.TrimSpace(envref.Pattern.ReplaceAllString(value, ""))
	if residual == "" || (strings.EqualFold(field, "authorization") && strings.EqualFold(residual, "Bearer")) {
		return nil
	}
	return fmt.Errorf("%s contains a literal credential-bearing value; use ${NAME}", path)
}

func placeholderNames(value reflect.Value) []string {
	seen := map[string]bool{}
	var walk func(reflect.Value)
	walk = func(item reflect.Value) {
		if !item.IsValid() {
			return
		}
		for item.Kind() == reflect.Interface || item.Kind() == reflect.Pointer {
			if item.IsNil() {
				return
			}
			item = item.Elem()
		}
		switch item.Kind() {
		case reflect.String:
			for _, name := range config.ExtractEnvVarNames(item.String()) {
				seen[name] = true
			}
		case reflect.Map:
			iter := item.MapRange()
			for iter.Next() {
				walk(iter.Value())
			}
		case reflect.Slice, reflect.Array:
			for index := range item.Len() {
				walk(item.Index(index))
			}
		}
	}
	walk(value)
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func mapValues(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		result = append(result, values[key])
	}
	return result
}

func rejectLiteralMCPSecrets(server config.MCPServer) error {
	if key, found := envref.LiteralSecretQueryKey(server.URL); found {
		return fmt.Errorf("literal credential-bearing URL query %q is forbidden; use ${NAME}", key)
	}
	for key, value := range server.Headers {
		residual := strings.TrimSpace(envref.Pattern.ReplaceAllString(value, ""))
		if secretBearingName(key) && residual != "" && !strings.EqualFold(residual, "Bearer") {
			return fmt.Errorf("literal credential-bearing header %q is forbidden; use ${NAME}", key)
		}
	}
	for key, value := range server.Env {
		if secretBearingName(key) && !envref.IsEntirelyPlaceholders(strings.TrimSpace(value)) {
			return fmt.Errorf("literal credential-bearing environment %q is forbidden; use ${NAME}", key)
		}
	}
	return nil
}

func secretBearingName(name string) bool {
	segments := credentialNameSegments(name)
	if len(segments) == 0 {
		return false
	}
	for _, segment := range segments {
		switch segment {
		case "authorization", "token", "secret", "password", "passwd", "cookie", "credential", "credentials", "apikey", "accesskey", "privatekey":
			return true
		}
	}
	if len(segments) == 1 && segments[0] == "auth" {
		return true
	}
	for index := range len(segments) - 1 {
		if ((segments[index] == "api" || segments[index] == "access") &&
			(segments[index+1] == "key" || segments[index+1] == "token")) ||
			(segments[index] == "private" && segments[index+1] == "key" &&
				(index+2 >= len(segments) || (segments[index+2] != "id" && segments[index+2] != "identifier" && segments[index+2] != studyURLNameSegment))) {
			return true
		}
	}
	return false
}

func studyURLName(name string) bool {
	for _, segment := range credentialNameSegments(name) {
		switch segment {
		case "url", "uri", "endpoint":
			return true
		}
	}
	return false
}

// credentialNameSegments recognizes separators and camel-case boundaries. It
// deliberately does not use substring matching: tokenizer is a normal setting,
// while token and apiKey are credential-bearing identifiers.
func credentialNameSegments(name string) []string {
	var segments []string
	var word []rune
	runes := []rune(name)
	flush := func() {
		if len(word) != 0 {
			segments = append(segments, strings.ToLower(string(word)))
			word = nil
		}
	}
	for index, character := range runes {
		if !unicode.IsLetter(character) && !unicode.IsDigit(character) {
			flush()
			continue
		}
		if index > 0 && len(word) > 0 && unicode.IsUpper(character) {
			previous := runes[index-1]
			nextIsLower := index+1 < len(runes) && unicode.IsLower(runes[index+1])
			if unicode.IsLower(previous) || (unicode.IsUpper(previous) && nextIsLower) {
				flush()
			}
		}
		word = append(word, character)
	}
	flush()
	return segments
}

func secretBearingFlag(value string) bool {
	flag, _, _ := strings.Cut(strings.TrimSpace(value), "=")
	return strings.HasPrefix(flag, "--") && secretBearingName(strings.TrimLeft(flag, "-"))
}

func writeStudyMCPPreflight(path string, contract studyMCPPreflight) error {
	data, err := json.Marshal(contract)
	if err != nil {
		return err
	}
	return writeJSONBytes(path, data)
}
