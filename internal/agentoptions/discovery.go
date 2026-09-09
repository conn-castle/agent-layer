package agentoptions

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/conn-castle/agent-layer/internal/clients"
	"github.com/conn-castle/agent-layer/internal/clients/antigravity"
	"github.com/conn-castle/agent-layer/internal/clients/claude"
	"github.com/conn-castle/agent-layer/internal/clients/codex"
	"github.com/conn-castle/agent-layer/internal/clients/grok"
	"github.com/conn-castle/agent-layer/internal/versiondispatch"
)

const discoveryTimeout = 10 * time.Second
const maxDiscoveryBytes = 2 * 1024 * 1024
const agentAntigravity = "antigravity"
const modelsCommand = "models"
const initializeMethod = "initialize"
const methodKey = "method"
const paramsKey = "params"

// HasModelDiscovery reports whether the installed harness can supply a catalog.
func HasModelDiscovery(agent string) bool {
	return slices.Contains([]string{agentAntigravity, agentClaude, agentCodex, agentCopilotCLI, agentGrok}, agent)
}

// DiscoverModels queries the harness without sync or inference. It uses the same
// project environment and provider configuration helpers as launch and dispatch.
// A returned catalog describes selectable models, not guaranteed account access.
func DiscoverModels(agent string, req DiscoveryRequest) ([]string, error) {
	if !HasModelDiscovery(agent) {
		return nil, fmt.Errorf("model discovery is not supported for %s", agent)
	}
	if req.Env == nil {
		req.Env = os.Environ()
	}
	req.Env = slices.Clone(req.Env)
	effectiveEnv := req.Env
	if req.Project != nil {
		effectiveEnv = clients.BuildEnv(effectiveEnv, req.Project.Env, nil)
	}
	if offline, _ := clients.GetEnv(effectiveEnv, versiondispatch.EnvNoNetwork); strings.TrimSpace(offline) != "" {
		return nil, fmt.Errorf("model discovery disabled by %s", versiondispatch.EnvNoNetwork)
	}
	if req.LookPath == nil {
		req.LookPath = exec.LookPath
	}
	if req.Context == nil {
		req.Context = context.Background()
	}
	if req.Timeout <= 0 {
		req.Timeout = discoveryTimeout
	}
	ctx, cancel := context.WithTimeout(req.Context, req.Timeout)
	defer cancel()
	req.Context = ctx
	var models []string
	var err error
	if agent == agentAntigravity {
		models, err = antigravity.DiscoverModels(antigravity.ModelOptionsRequest{
			Context: ctx, Project: req.Project, Env: req.Env, LookPath: req.LookPath, Timeout: req.Timeout,
		})
	} else {
		models, err = discoverCommandModels(agent, req)
	}
	if ctx.Err() != nil {
		return nil, fmt.Errorf("%s model discovery: %w", agent, ctx.Err())
	}
	if err != nil {
		return nil, fmt.Errorf("%s model discovery: %w", agent, err)
	}
	return normalizeModels(agent, models)
}

// ParseModelCommandOutput validates native non-protocol model output. Remote
// executors can transport the bytes without maintaining a second model parser.
func ParseModelCommandOutput(agent string, output []byte) ([]string, error) {
	if len(output) > maxDiscoveryBytes {
		return nil, errors.New("model discovery output exceeded size limit")
	}
	var models []string
	var err error
	switch agent {
	case agentAntigravity:
		models, err = antigravity.ParseModelOutput(output)
	case agentGrok:
		models, err = readGrokModels(bytes.NewReader(output))
	default:
		return nil, fmt.Errorf("%s requires protocol model discovery", agent)
	}
	if err != nil {
		return nil, fmt.Errorf("%s model discovery: %w", agent, err)
	}
	return normalizeModels(agent, models)
}

func normalizeModels(agent string, models []string) ([]string, error) {
	result := make([]string, 0, len(models))
	for _, model := range models {
		model = strings.TrimSpace(model)
		if model == "" || strings.ContainsAny(model, "\r\n\t") {
			return nil, fmt.Errorf("%s model discovery returned an invalid model", agent)
		}
		if !slices.Contains(result, model) {
			result = append(result, model)
		}
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("%s model discovery returned no models", agent)
	}
	return result, nil
}

func discoveryCommand(agent string, req DiscoveryRequest) (*exec.Cmd, error) {
	binary := agent
	if agent == agentCopilotCLI {
		binary = "copilot"
	}
	path, err := req.LookPath(binary)
	if err != nil {
		return nil, err
	}
	env := slices.Clone(req.Env)
	if project := req.Project; project != nil {
		env = clients.BuildEnv(env, project.Env, nil)
		switch agent {
		case agentClaude:
			env = claude.ConfigureEnvironment(project.Root, env, project.Config.Agents.Claude, nil)
		case agentCodex:
			env = codex.ConfigureEnvironment(project.Root, env, project.Config.Agents.Codex, nil)
		case agentGrok:
			if err := grok.EnsureHome(project.Root); err != nil {
				return nil, err
			}
			env = grok.ConfigureEnvironment(project.Root, env, project.Config.Agents.Grok, nil)
		}
	}
	args := []string{modelsCommand}
	switch agent {
	case agentClaude:
		args = []string{"-p", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose",
			"--no-session-persistence", "--strict-mcp-config", "--mcp-config", `{"mcpServers":{}}`,
			"--settings", `{"disableAllHooks":true}`, "--tools", ""}
	case agentCodex:
		args = []string{"app-server"}
	case agentCopilotCLI:
		args = []string{"--headless", "--stdio", "--no-auto-update"}
	}
	// #nosec G204 -- PATH-resolved harness; fixed discovery arguments, no prompt.
	cmd := exec.CommandContext(req.Context, path, args...)
	cmd.Env = env
	cmd.WaitDelay = time.Second
	if req.Project != nil {
		cmd.Dir = req.Project.Root
	}
	return cmd, nil
}

func discoverCommandModels(agent string, req DiscoveryRequest) ([]string, error) {
	// Give protocol completion its own cancellation scope to reap the harness
	// immediately after the reply, without waiting for the discovery deadline.
	ctx, cancel := context.WithCancel(req.Context)
	defer cancel()
	req.Context = ctx
	cmd, err := discoveryCommand(agent, req)
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	stopClose := context.AfterFunc(ctx, func() {
		_ = stdout.Close()
		_ = stdin.Close()
	})
	defer stopClose()
	waited := false
	defer func() {
		_ = stdin.Close()
		cancel()
		if !waited {
			_ = cmd.Wait()
		}
	}()
	reader := &io.LimitedReader{R: stdout, N: maxDiscoveryBytes + 1}
	if agent == agentGrok {
		models, err := readGrokModels(reader)
		if err != nil {
			return nil, err
		}
		if reader.N == 0 {
			return nil, errors.New("model discovery output exceeded size limit")
		}
		err = cmd.Wait()
		waited = true
		if err != nil {
			return nil, err
		}
		return models, nil
	}
	if agent == agentCopilotCLI {
		return readCopilotModels(reader, stdin)
	}
	decoder, encoder := json.NewDecoder(reader), json.NewEncoder(stdin)
	if agent == agentClaude {
		return readClaudeModels(decoder, encoder)
	}
	return readCodexModels(decoder, encoder)
}

func readGrokModels(reader io.Reader) ([]string, error) {
	scanner := bufio.NewScanner(reader)
	var models []string
	inModels := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.Contains(strings.ToLower(line), "not authenticated") {
			return nil, errors.New("harness is not authenticated; sign in using al grok")
		}
		if line == "Available models:" {
			inModels = true
			continue
		}
		if !inModels || line == "" {
			continue
		}
		if !strings.HasPrefix(line, "* ") && !strings.HasPrefix(line, "- ") {
			return nil, errors.New("unrecognized grok models output")
		}
		value := strings.TrimSpace(strings.TrimSuffix(line[2:], " (default)"))
		models = append(models, value)
	}
	return models, scanner.Err()
}

func readClaudeModels(decoder *json.Decoder, encoder *json.Encoder) ([]string, error) {
	if err := encoder.Encode(map[string]any{"type": "control_request", "request_id": modelsCommand, "request": map[string]any{"subtype": initializeMethod}}); err != nil {
		return nil, err
	}
	for {
		var msg struct {
			Type     string `json:"type"`
			Response struct {
				ID       string `json:"request_id"`
				Subtype  string `json:"subtype"`
				Response struct {
					Models []struct {
						Value string `json:"value"`
					} `json:"models"`
				} `json:"response"`
			} `json:"response"`
		}
		if err := decoder.Decode(&msg); err != nil {
			return nil, fmt.Errorf("read initialization response: %w", err)
		}
		if msg.Type != "control_response" || msg.Response.ID != modelsCommand {
			continue
		}
		if msg.Response.Subtype != "success" {
			return nil, errors.New("harness rejected initialization; check Claude authentication and configuration")
		}
		models := make([]string, 0, len(msg.Response.Response.Models))
		for _, model := range msg.Response.Response.Models {
			models = append(models, model.Value)
		}
		return models, nil
	}
}

func codexRequest(decoder *json.Decoder, encoder *json.Encoder, id int, method string, params any, result any) error {
	if err := encoder.Encode(map[string]any{"id": id, methodKey: method, paramsKey: params}); err != nil {
		return err
	}
	for {
		var msg struct {
			ID     *int            `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  json.RawMessage `json:"error"`
		}
		if err := decoder.Decode(&msg); err != nil {
			return fmt.Errorf("read %s response: %w", method, err)
		}
		if msg.ID == nil || *msg.ID != id {
			continue
		}
		if len(msg.Error) > 0 && string(msg.Error) != "null" {
			return fmt.Errorf("harness rejected %s; check Codex authentication and configuration", method)
		}
		return json.Unmarshal(msg.Result, result)
	}
}

func readCodexModels(decoder *json.Decoder, encoder *json.Encoder) ([]string, error) {
	var initialized map[string]any
	if err := codexRequest(decoder, encoder, 1, initializeMethod, map[string]any{"clientInfo": map[string]string{"name": "agent_layer", "version": "1"}}, &initialized); err != nil {
		return nil, err
	}
	if err := encoder.Encode(map[string]any{methodKey: "initialized", paramsKey: map[string]any{}}); err != nil {
		return nil, err
	}
	var models []string
	cursor := ""
	seen := map[string]bool{}
	for id := 2; ; id++ {
		params := map[string]any{"includeHidden": false}
		if cursor != "" {
			params["cursor"] = cursor
		}
		var page struct {
			Data []struct {
				Model string `json:"model"`
			} `json:"data"`
			NextCursor *string `json:"nextCursor"`
		}
		if err := codexRequest(decoder, encoder, id, "model/list", params, &page); err != nil {
			return nil, err
		}
		for _, model := range page.Data {
			models = append(models, model.Model)
		}
		if page.NextCursor == nil || *page.NextCursor == "" {
			return models, nil
		}
		cursor = *page.NextCursor
		if seen[cursor] {
			return nil, errors.New("model/list repeated a pagination cursor")
		}
		seen[cursor] = true
	}
}
