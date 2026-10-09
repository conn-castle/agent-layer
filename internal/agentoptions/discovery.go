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
const antigravityBinary = "agy"
const modelsCommand = "models"
const initializeMethod = "initialize"
const methodKey = "method"
const paramsKey = "params"
const jsonRPCKey = "jsonrpc"
const initializedMethod = "initialized"
const clientNameKey = "name"

var errGrokUnauthenticated = errors.New("harness is not authenticated; sign in using al grok")

// HasModelDiscovery reports whether the installed harness can supply a catalog.
func HasModelDiscovery(agent string) bool {
	return slices.Contains([]string{agentAntigravity, agentClaude, agentCodex, agentCopilotCLI, agentGrok, agentMuse}, agent)
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
	if agent == agentClaude && req.Cleanup != nil {
		models, err = discoverClaudeModelsInBackground(req)
	} else {
		models, err = discoverCommandModels(agent, req, nil)
	}
	if errors.Is(err, errGrokUnauthenticated) {
		// Grok prints its authentication status before it silently refreshes an
		// expired session, so a second run reports the refresh the first persisted.
		models, err = discoverCommandModels(agent, req, nil)
	}
	// Claude captures the lookup deadline at its reply, then preserves native
	// shutdown errors even if graceful cleanup outlasts that deadline.
	if ctx.Err() != nil && agent != agentClaude {
		return nil, fmt.Errorf("%s model discovery: %w", agent, ctx.Err())
	}
	if err != nil {
		return nil, fmt.Errorf("%s model discovery: %w", agent, err)
	}
	if agent == agentClaude {
		return models, nil
	}
	return normalizeModels(agent, models)
}

func discoverClaudeModelsInBackground(req DiscoveryRequest) ([]string, error) {
	finish, err := req.Cleanup.begin()
	if err != nil {
		return nil, err
	}
	type reply struct {
		models []string
		err    error
	}
	ready := make(chan reply)
	go func() {
		delivered := false
		var shutdownErr error
		models, err := discoverCommandModels(agentClaude, req, &claudeProbeObserver{
			ready: func(models []string) {
				select {
				case ready <- reply{models: models}:
					delivered = true
				case <-req.Context.Done():
				}
			},
			shutdown: func(err error) { shutdownErr = err },
		})
		if !delivered {
			// A failed lookup is reported once. If the caller already timed
			// out, retain native shutdown failures for the cleanup owner.
			select {
			case ready <- reply{models, err}:
				finish(nil)
				return
			case <-req.Context.Done():
			}
		}
		if shutdownErr != nil {
			shutdownErr = fmt.Errorf("claude model discovery shutdown: %w", shutdownErr)
		}
		finish(shutdownErr)
	}()
	select {
	case response := <-ready:
		return response.models, response.err
	case <-req.Context.Done():
		return nil, req.Context.Err()
	}
}

type claudeProbeObserver struct {
	ready    func([]string)
	shutdown func(error)
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
		models, err = readAntigravityModels(output)
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
		if agent == agentMuse {
			return nil, errors.New("muse model/list returned an empty catalog; this Muse build does not expose model suggestions through MSP")
		}
		return nil, fmt.Errorf("%s model discovery returned no models", agent)
	}
	return result, nil
}

func discoveryCommand(agent string, req DiscoveryRequest) (*exec.Cmd, error) {
	binary := agent
	switch agent {
	case agentAntigravity:
		binary = antigravityBinary
	case agentCopilotCLI:
		binary = "copilot"
	}
	// Resolve the binary before provider setup so a missing harness creates no
	// project state such as the Antigravity home.
	path, err := req.LookPath(binary)
	if err != nil {
		return nil, err
	}
	env := slices.Clone(req.Env)
	args := []string{modelsCommand}
	if project := req.Project; project != nil {
		env = clients.BuildEnv(env, project.Env, nil)
		switch agent {
		case agentAntigravity:
			if args, err = antigravity.BaseArgs(project.Root, project.Config); err != nil {
				return nil, err
			}
			args = append(args, modelsCommand)
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
	switch agent {
	case agentAntigravity:
		env = antigravity.ConfigureEnvironment(env)
	case agentClaude:
		args = []string{"-p", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose",
			"--no-session-persistence", "--strict-mcp-config", "--mcp-config", `{"mcpServers":{}}`,
			"--settings", `{"disableAllHooks":true}`, "--tools", ""}
	case agentCodex:
		args = []string{"app-server"}
	case agentCopilotCLI:
		args = []string{"--headless", "--stdio", "--no-auto-update"}
	case agentMuse:
		args = []string{"serve", "--no-session-log"}
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

func discoverCommandModels(agent string, req DiscoveryRequest, observer *claudeProbeObserver) ([]string, error) {
	// Long-lived protocol servers are cancelled after their reply. Claude uses
	// EOF instead and waits for its credential-safe shutdown.
	ctx, cancel := context.WithCancel(req.Context)
	defer cancel()
	req.Context = ctx
	cmd, err := discoveryCommand(agent, req)
	if err != nil {
		return nil, err
	}
	var stdout io.ReadCloser
	var stdoutWriter *os.File
	if agent == agentClaude {
		// Own this pipe: Wait must observe native exit independently of any
		// descendant that inherited stdout and keeps its write end open.
		stdout, stdoutWriter, err = os.Pipe()
		cmd.Stdout = stdoutWriter
	} else {
		stdout, err = cmd.StdoutPipe()
	}
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = stdout.Close()
		if stdoutWriter != nil {
			_ = stdoutWriter.Close()
		}
	}()
	// agy models takes no input, so it keeps the null-device stdin of a nil cmd.Stdin.
	var stdin io.WriteCloser
	if agent != agentAntigravity {
		if stdin, err = cmd.StdinPipe(); err != nil {
			return nil, err
		}
	}
	if agent == agentClaude {
		// EOF enters Claude's graceful shutdown, which waits for a held OAuth
		// refresh before exiting. SIGKILL can spend the refresh token before its
		// replacement is saved. Use EOF on cancellation too, without a later kill.
		cmd.Cancel = func() error {
			if err := stdin.Close(); errors.Is(err, os.ErrClosed) {
				return os.ErrProcessDone
			} else {
				return err
			}
		}
		cmd.WaitDelay = 0
	}
	closeStdin := func() {
		if stdin != nil {
			_ = stdin.Close()
		}
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	if stdoutWriter != nil {
		_ = stdoutWriter.Close()
	}
	waitProcess := cmd.Wait
	if agent == agentClaude {
		waitProcess = claudeProcessWait(ctx, cmd, stdout)
	}
	stopClose := context.AfterFunc(ctx, func() {
		closeStdin()
		if agent != agentClaude {
			_ = stdout.Close()
		}
	})
	defer stopClose()
	waited := false
	defer func() {
		closeStdin()
		cancel()
		if !waited {
			_ = waitProcess()
		}
	}()
	reader := &io.LimitedReader{R: stdout, N: maxDiscoveryBytes + 1}
	if agent == agentAntigravity {
		output, err := io.ReadAll(reader)
		if err != nil {
			return nil, err
		}
		if reader.N == 0 {
			return nil, errors.New("agy models output exceeded size limit")
		}
		err = waitProcess()
		waited = true
		if err != nil {
			return nil, err
		}
		models, err := readAntigravityModels(output)
		if err != nil {
			return nil, err
		}
		if len(models) == 0 {
			return nil, errors.New("agy models returned no model options")
		}
		return models, nil
	}
	if agent == agentGrok {
		// An unauthenticated status still waits for exit: killing Grok early would
		// abort the session refresh that the retry depends on. That status also
		// outranks the exit status, which a failed refresh may make nonzero.
		models, err := readGrokModels(reader)
		if err != nil && !errors.Is(err, errGrokUnauthenticated) {
			return nil, err
		}
		if reader.N == 0 {
			return nil, errors.New("model discovery output exceeded size limit")
		}
		waited = true
		if waitErr := waitProcess(); waitErr != nil && err == nil {
			return nil, waitErr
		}
		return models, err
	}
	if agent == agentCopilotCLI {
		return readCopilotModels(reader, stdin)
	}
	decoder, encoder := json.NewDecoder(reader), json.NewEncoder(stdin)
	if agent == agentMuse {
		return readMuseModels(decoder, encoder)
	}
	if agent == agentClaude {
		models, err := readClaudeModels(decoder, encoder)
		if err == nil {
			models, err = normalizeModels(agentClaude, models)
		}
		// The lookup deadline ends at the reply, not at native shutdown.
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		closeStdin()
		if observer != nil && err == nil {
			observer.ready(models)
		}
		// Drain after success or a protocol error so shutdown cannot block on
		// a full stdout pipe while credential persistence is still running.
		drainErr, waitErr := waitForClaudeExit(waitProcess, stdout)
		waited = true
		if ctx.Err() != nil && errors.Is(waitErr, ctx.Err()) {
			waitErr = nil
		}
		if observer != nil {
			observer.shutdown(errors.Join(drainErr, waitErr))
		}
		if err != nil {
			return nil, errors.Join(err, drainErr, waitErr)
		}
		if shutdownErr := errors.Join(drainErr, waitErr); shutdownErr != nil {
			return nil, shutdownErr
		}
		return models, nil
	}
	return readCodexModels(decoder, encoder)
}

func claudeProcessWait(ctx context.Context, cmd *exec.Cmd, stdout io.ReadCloser) func() error {
	exited := make(chan struct{})
	var exitErr error
	go func() {
		exitErr = cmd.Wait()
		close(exited)
	}()
	readClosed := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		defer close(readClosed)
		// Release a parser waiting on inherited stdout only after native exit.
		// Buffered replies remain readable until the lookup is cancelled.
		<-exited
		_ = stdout.Close()
	})
	return func() error {
		<-exited
		if !stop() {
			<-readClosed
		}
		return exitErr
	}
}

func waitForClaudeExit(wait func() error, stdout io.ReadCloser) (drainErr, waitErr error) {
	drained := make(chan error, 1)
	go func() {
		_, err := io.Copy(io.Discard, stdout)
		drained <- err
	}()
	waitErr = wait()
	// Claude is now gone, so closing the reader cannot interrupt its
	// credential writes. Descendants need not keep cleanup blocked.
	_ = stdout.Close()
	drainErr = <-drained
	if errors.Is(drainErr, os.ErrClosed) {
		drainErr = nil
	}
	return drainErr, waitErr
}

func readMuseModels(decoder *json.Decoder, encoder *json.Encoder) ([]string, error) {
	if err := encoder.Encode(map[string]any{jsonRPCKey: copilotJSONRPCVersion, "id": 1, methodKey: initializeMethod, paramsKey: map[string]any{"clientInfo": map[string]string{clientNameKey: "agent_layer", "version": "0.0.0"}}}); err != nil {
		return nil, err
	}
	// JSON-RPC permits notifications and unrelated messages before the
	// matching response, so read until the expected id arrives instead of
	// assuming the next message answers this request.
	var init map[string]any
	for {
		init = nil
		if err := decoder.Decode(&init); err != nil {
			return nil, err
		}
		if id, ok := init["id"]; !ok || id == nil || fmt.Sprint(id) != "1" {
			continue
		}
		break
	}
	if init["error"] != nil {
		return nil, fmt.Errorf("muse initialize failed: %v", init["error"])
	}
	if err := encoder.Encode(map[string]any{jsonRPCKey: copilotJSONRPCVersion, methodKey: initializedMethod}); err != nil {
		return nil, err
	}
	if err := encoder.Encode(map[string]any{jsonRPCKey: copilotJSONRPCVersion, "id": 2, methodKey: "model/list", paramsKey: map[string]any{}}); err != nil {
		return nil, err
	}
	var reply struct {
		ID     any `json:"id"`
		Result struct {
			Models *[]struct {
				ModelID string `json:"modelId"`
			} `json:"models"`
		} `json:"result"`
		Error any `json:"error"`
	}
	for {
		reply = struct {
			ID     any `json:"id"`
			Result struct {
				Models *[]struct {
					ModelID string `json:"modelId"`
				} `json:"models"`
			} `json:"result"`
			Error any `json:"error"`
		}{}
		if err := decoder.Decode(&reply); err != nil {
			return nil, err
		}
		if reply.ID == nil || fmt.Sprint(reply.ID) != "2" {
			continue
		}
		break
	}
	if reply.Error != nil {
		return nil, fmt.Errorf("muse model/list failed: %v", reply.Error)
	}
	if reply.Result.Models == nil {
		return nil, errors.New("muse model/list response omitted result.models")
	}
	models := make([]string, 0, len(*reply.Result.Models))
	for _, row := range *reply.Result.Models {
		models = append(models, row.ModelID)
	}
	return models, nil
}

func readGrokModels(reader io.Reader) ([]string, error) {
	scanner := bufio.NewScanner(reader)
	var models []string
	inModels := false
	unauthenticated := false
	var parseErr error
	// Read to EOF so Grok can finish a silent refresh after its status line.
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.Contains(strings.ToLower(line), "not authenticated") {
			unauthenticated = true
			continue
		}
		if line == "Available models:" {
			inModels = true
			continue
		}
		if !inModels || line == "" {
			continue
		}
		if !strings.HasPrefix(line, "* ") && !strings.HasPrefix(line, "- ") {
			parseErr = errors.New("unrecognized grok models output")
			continue
		}
		value := strings.TrimSpace(strings.TrimSuffix(line[2:], " (default)"))
		models = append(models, value)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if unauthenticated {
		return nil, errGrokUnauthenticated
	}
	if parseErr != nil {
		return nil, parseErr
	}
	return models, nil
}

// readAntigravityModels requires the native slug<TAB>display format so arbitrary
// stdout, such as an authentication message, never becomes a model suggestion.
func readAntigravityModels(output []byte) ([]string, error) {
	scanner := bufio.NewScanner(bytes.NewReader(output))
	models := make([]string, 0)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		slug, label, ok := strings.Cut(line, "\t")
		if !ok || strings.TrimSpace(slug) == "" || strings.TrimSpace(label) == "" || strings.Contains(label, "\t") {
			return nil, errors.New("agy models returned an invalid model row; expected slug<TAB>display name")
		}
		models = append(models, strings.TrimSpace(slug))
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return models, nil
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
	if err := codexRequest(decoder, encoder, 1, initializeMethod, map[string]any{"clientInfo": map[string]string{clientNameKey: "agent_layer", "version": "1"}}, &initialized); err != nil {
		return nil, err
	}
	if err := encoder.Encode(map[string]any{methodKey: initializedMethod, paramsKey: map[string]any{}}); err != nil {
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
