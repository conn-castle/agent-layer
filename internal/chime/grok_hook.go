package chime

import (
	"bytes"
	"encoding/json"
	"fmt"
)

const (
	// GrokHookCommand is the Stop hook command Agent Layer projects for Grok.
	GrokHookCommand = `al hook chime grok || { echo 'agent-layer chime handler unavailable' >&2; true; } # agent-layer-chime`
	// HookTimeoutSeconds bounds every projected chime hook command.
	HookTimeoutSeconds = 5
)

// GrokHookDocument returns the managed .grok/hooks/agent-layer-chime.json contents.
func GrokHookDocument() []byte {
	return fmt.Appendf(nil, `{
  "hooks": {
    "Stop": [
      {
        "hooks": [
          {
            "type": "command",
            "command": %q,
            "timeout": %d
          }
        ]
      }
    ]
  }
}
`, GrokHookCommand, HookTimeoutSeconds)
}

// IsGrokHookDocument reports whether data is the complete managed hook
// document, ignoring only JSON whitespace. Compacting preserves key order and
// duplicate keys, so lookalike user hooks cannot decode into the managed shape.
func IsGrokHookDocument(data []byte) bool {
	var existing, managed bytes.Buffer
	if json.Compact(&existing, data) != nil || json.Compact(&managed, GrokHookDocument()) != nil {
		return false
	}
	return bytes.Equal(existing.Bytes(), managed.Bytes())
}
