package projection

import (
	"fmt"

	"github.com/conn-castle/agent-layer/internal/config"
)

// MCPServerResolveError wraps a server-specific resolve error.
type MCPServerResolveError struct {
	ServerID string
	Err      error
}

func (e *MCPServerResolveError) Error() string {
	return fmt.Sprintf("mcp server %s: %v", e.ServerID, e.Err)
}

func (e *MCPServerResolveError) Unwrap() error {
	return e.Err
}

// ClientPlaceholderResolver returns a resolver that preserves placeholders in client-specific syntax.
// clientSyntax is a format string like "${%s}" or "${env:%s}" where %s is the env var name.
// Built-in placeholders (like AL_REPO_ROOT) are resolved to their actual values.
func ClientPlaceholderResolver(clientSyntax string) EnvVarResolver {
	return func(name, value string) string {
		if config.IsBuiltInEnvVar(name) {
			return value
		}
		return fmt.Sprintf(clientSyntax, name)
	}
}

// FullValueResolver returns a resolver that keeps resolved env values.
// Missing values still error during substitution before the resolver runs.
func FullValueResolver(_ map[string]string) EnvVarResolver {
	return func(_ string, value string) string {
		return value
	}
}

// ResolveEnabledMCPServers resolves all enabled MCP servers without client filtering.
// This is useful for operations like doctor checks that need to connect to all servers.
func ResolveEnabledMCPServers(servers []config.MCPServer, env map[string]string) ([]ResolvedMCPServer, error) {
	resolver := FullValueResolver(env)

	var resolved []ResolvedMCPServer
	for _, server := range servers {
		if server.Enabled == nil || !*server.Enabled {
			continue
		}

		entry, err := resolveSingleServer(server, env, resolver)
		if err != nil {
			return nil, &MCPServerResolveError{ServerID: server.ID, Err: err}
		}
		resolved = append(resolved, entry)
	}

	return resolved, nil
}

// resolveSingleServer resolves a single MCP server configuration.
func resolveSingleServer(server config.MCPServer, env map[string]string, resolver EnvVarResolver) (ResolvedMCPServer, error) {
	entry := ResolvedMCPServer{
		ID:        server.ID,
		Transport: server.Transport,
	}
	repoRoot := env[config.BuiltinRepoRootEnvVar]

	switch server.Transport {
	case config.TransportHTTP:
		httpTransport := server.HTTPTransport
		if httpTransport == "" {
			httpTransport = "sse"
		}
		entry.HTTPTransport = httpTransport

		url, err := config.SubstituteEnvVarsWith(server.URL, env, resolver)
		if err != nil {
			return entry, fmt.Errorf("url: %w", err)
		}
		entry.URL = url

		if len(server.Headers) > 0 {
			headers := make(map[string]string, len(server.Headers))
			for key, value := range server.Headers {
				resolvedValue, err := config.SubstituteEnvVarsWith(value, env, resolver)
				if err != nil {
					return entry, fmt.Errorf("header %s: %w", key, err)
				}
				headers[key] = resolvedValue
			}
			entry.Headers = headers
		}
	case config.TransportStdio:
		command, err := config.SubstituteEnvVarsWith(server.Command, env, resolver)
		if err != nil {
			return entry, fmt.Errorf("command: %w", err)
		}
		command, err = config.ExpandPathIfNeeded(server.Command, command, repoRoot)
		if err != nil {
			return entry, fmt.Errorf("command: %w", err)
		}
		entry.Command = command

		if len(server.Args) > 0 {
			args := make([]string, 0, len(server.Args))
			for _, arg := range server.Args {
				resolvedArg, err := config.SubstituteEnvVarsWith(arg, env, resolver)
				if err != nil {
					return entry, fmt.Errorf("arg %s: %w", arg, err)
				}
				resolvedArg, err = config.ExpandPathIfNeeded(arg, resolvedArg, repoRoot)
				if err != nil {
					return entry, fmt.Errorf("arg %s: %w", arg, err)
				}
				args = append(args, resolvedArg)
			}
			entry.Args = args
		}

		if len(server.Env) > 0 {
			envMap := make(map[string]string, len(server.Env))
			for key, value := range server.Env {
				resolvedValue, err := config.SubstituteEnvVarsWith(value, env, resolver)
				if err != nil {
					return entry, fmt.Errorf("env %s: %w", key, err)
				}
				envMap[key] = resolvedValue
			}
			entry.Env = envMap
		}
	}

	return entry, nil
}
