package sync

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/conn-castle/agent-layer/internal/messages"
)

// appendCodexAgentSpecific encodes agent-specific TOML and appends it to the codex config output.
func appendCodexAgentSpecific(builder *strings.Builder, agentSpecific map[string]any) error {
	if len(agentSpecific) == 0 {
		return nil
	}
	encoded, err := encodeAgentSpecificTOML(agentSpecific)
	if err != nil {
		return err
	}
	if encoded == "" {
		return nil
	}
	builder.WriteString("\n")
	builder.WriteString(encoded)
	return nil
}

// encodeAgentSpecificTOML encodes an agent-specific TOML map for inclusion in the codex config.
func encodeAgentSpecificTOML(agentSpecific map[string]any) (string, error) {
	var buf bytes.Buffer
	encoder := toml.NewEncoder(&buf)
	if err := encoder.Encode(agentSpecific); err != nil {
		return "", fmt.Errorf(messages.SyncMarshalCodexAgentSpecificFailedFmt, err)
	}
	return buf.String(), nil
}
