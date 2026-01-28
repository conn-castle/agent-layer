package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRequiredEnvVarsForMCPServer(t *testing.T) {
	server := MCPServer{
		URL:     "https://example.com?token=${URL_TOKEN}",
		Command: "run-${CMD_TOKEN}",
		Args:    []string{"--key", "${ARG_TOKEN}"},
		Headers: map[string]string{"Authorization": "Bearer ${HDR_TOKEN}"},
		Env:     map[string]string{"API_KEY": "${ENV_TOKEN}"},
	}

	want := []string{"ARG_TOKEN", "CMD_TOKEN", "ENV_TOKEN", "HDR_TOKEN", "URL_TOKEN"}
	assert.Equal(t, want, RequiredEnvVarsForMCPServer(server))
}

func TestRequiredEnvVarsForMCPServers(t *testing.T) {
	servers := []MCPServer{
		{URL: "https://example.com?token=${TOKEN}"},
		{Headers: map[string]string{"X": "${TOKEN}"}, Env: map[string]string{"API_KEY": "${API_KEY}"}},
	}

	want := []string{"API_KEY", "TOKEN"}
	assert.Equal(t, want, RequiredEnvVarsForMCPServers(servers))
}

func TestRequiredEnvVarsForMCPServerEmpty(t *testing.T) {
	server := MCPServer{
		URL:     "https://example.com",
		Command: "run",
		Args:    []string{"--key", "value"},
	}

	// No env vars referenced
	result := RequiredEnvVarsForMCPServer(server)
	assert.Nil(t, result)
}

func TestRequiredEnvVarsForMCPServersEmpty(t *testing.T) {
	servers := []MCPServer{
		{URL: "https://example.com"},
		{Command: "run"},
	}

	// No env vars referenced
	result := RequiredEnvVarsForMCPServers(servers)
	assert.Nil(t, result)
}

func TestRequiredEnvVarsForMCPServerIgnoresBuiltIns(t *testing.T) {
	server := MCPServer{
		Args: []string{"${" + BuiltinRepoRootEnvVar + "}", "${CUSTOM_PATH}"},
	}

	want := []string{"CUSTOM_PATH"}
	assert.Equal(t, want, RequiredEnvVarsForMCPServer(server))
}
