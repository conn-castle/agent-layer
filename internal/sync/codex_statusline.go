package sync

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/templates"
)

const codexStatuslineSourceName = "codex-statusline.toml"

func codexAgentSpecificForOutput(sys System, root string, codex config.CodexConfig) (map[string]any, bool, error) {
	var agentSpecific map[string]any
	if len(codex.AgentSpecific) > 0 {
		agentSpecific = cloneAgentSpecificValue(codex.AgentSpecific).(map[string]any)
	}
	if !config.CodexStatuslineEnabled(codex) {
		return agentSpecific, false, nil
	}
	if codexAgentSpecificDefinesStatusLine(codex.AgentSpecific) {
		return agentSpecific, false, nil
	}

	statusLine, err := readCodexStatuslineSource(sys, root)
	if err != nil {
		return nil, false, err
	}
	if agentSpecific == nil {
		agentSpecific = make(map[string]any)
	}
	if err := injectCodexStatusLine(agentSpecific, statusLine); err != nil {
		return nil, false, err
	}
	return agentSpecific, true, nil
}

func codexAgentSpecificDefinesStatusLine(agentSpecific map[string]any) bool {
	tui, ok := agentSpecific["tui"].(map[string]any)
	if !ok {
		return false
	}
	_, ok = tui["status_line"]
	return ok
}

func injectCodexStatusLine(agentSpecific map[string]any, statusLine []string) error {
	tui := make(map[string]any)
	if existing, ok := agentSpecific["tui"]; ok {
		existingTUI, ok := existing.(map[string]any)
		if !ok {
			return fmt.Errorf(messages.SyncCodexStatuslineTUITableConflict)
		}
		tui = existingTUI
	}
	tui["status_line"] = statusLine
	agentSpecific["tui"] = tui
	return nil
}

func readCodexStatuslineSource(sys System, root string) ([]string, error) {
	src := filepath.Join(root, ".agent-layer", codexStatuslineSourceName)
	data, err := sys.ReadFile(src)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf(messages.SyncReadFailedFmt, src, err)
		}
		data, err = templates.Read(codexStatuslineSourceName)
		if err != nil {
			return nil, fmt.Errorf(messages.SyncReadTemplateFailedFmt, codexStatuslineSourceName, err)
		}
		if err := sys.MkdirAll(filepath.Dir(src), 0o755); err != nil {
			return nil, fmt.Errorf(messages.SyncCreateDirFailedFmt, filepath.Dir(src), err)
		}
		if err := sys.WriteFileAtomic(src, data, 0o644); err != nil {
			return nil, fmt.Errorf(messages.SyncWriteFileFailedFmt, src, err)
		}
	}
	return parseCodexStatuslineSource(data, src)
}

func parseCodexStatuslineSource(data []byte, source string) ([]string, error) {
	var raw map[string]any
	if err := toml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf(messages.SyncCodexStatuslineInvalidTOMLFmt, source, err)
	}
	tuiValue, ok := raw["tui"]
	if !ok {
		return nil, fmt.Errorf(messages.SyncCodexStatuslineStatusLineMissingFmt, source)
	}
	if len(raw) != 1 {
		return nil, fmt.Errorf(messages.SyncCodexStatuslineOnlyStatusLineFmt, source)
	}

	tui, ok := tuiValue.(map[string]any)
	if !ok {
		return nil, fmt.Errorf(messages.SyncCodexStatuslineOnlyStatusLineFmt, source)
	}
	statusLineValue, ok := tui["status_line"]
	if !ok {
		return nil, fmt.Errorf(messages.SyncCodexStatuslineStatusLineMissingFmt, source)
	}
	if len(tui) != 1 {
		return nil, fmt.Errorf(messages.SyncCodexStatuslineOnlyStatusLineFmt, source)
	}

	switch statusLine := statusLineValue.(type) {
	case []any:
		out := make([]string, 0, len(statusLine))
		for _, item := range statusLine {
			itemString, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf(messages.SyncCodexStatuslineStatusLineTypeFmt, source)
			}
			out = append(out, itemString)
		}
		return out, nil
	default:
		return nil, fmt.Errorf(messages.SyncCodexStatuslineStatusLineTypeFmt, source)
	}
}
