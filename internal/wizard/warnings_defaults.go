package wizard

import (
	"fmt"

	"github.com/nicholasjconn/agent-layer/internal/config"
)

// WarningDefaults holds wizard defaults sourced from the template config.
type WarningDefaults struct {
	InstructionTokenThreshold      int
	MCPServerThreshold             int
	MCPToolsTotalThreshold         int
	MCPServerToolsThreshold        int
	MCPSchemaTokensTotalThreshold  int
	MCPSchemaTokensServerThreshold int
}

// loadWarningDefaults returns warning defaults from the template config.
func loadWarningDefaults() (WarningDefaults, error) {
	cfg, err := config.LoadTemplateConfig()
	if err != nil {
		return WarningDefaults{}, err
	}

	w := cfg.Warnings
	if w.InstructionTokenThreshold == nil ||
		w.MCPServerThreshold == nil ||
		w.MCPToolsTotalThreshold == nil ||
		w.MCPServerToolsThreshold == nil ||
		w.MCPSchemaTokensTotalThreshold == nil ||
		w.MCPSchemaTokensServerThreshold == nil {
		return WarningDefaults{}, fmt.Errorf("template config warnings defaults are incomplete")
	}

	return WarningDefaults{
		InstructionTokenThreshold:      *w.InstructionTokenThreshold,
		MCPServerThreshold:             *w.MCPServerThreshold,
		MCPToolsTotalThreshold:         *w.MCPToolsTotalThreshold,
		MCPServerToolsThreshold:        *w.MCPServerToolsThreshold,
		MCPSchemaTokensTotalThreshold:  *w.MCPSchemaTokensTotalThreshold,
		MCPSchemaTokensServerThreshold: *w.MCPSchemaTokensServerThreshold,
	}, nil
}
