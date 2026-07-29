package agentdispatch

import (
	_ "embed"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	toml "github.com/pelletier/go-toml/v2"
)

const (
	waitMinutesPlaceholder = "{{wait_minutes}}"
	modelParameter         = "model"
	handleParameter        = "handle"
)

//go:embed mcp_tool_descriptions.toml
var mcpToolDescriptionsTOML []byte

type mcpToolDescriptionCatalog struct {
	Tools map[string]mcpToolDescription `toml:"tools"`
}

type mcpToolDescription struct {
	Description string            `toml:"description"`
	Parameters  map[string]string `toml:"parameters"`
}

func loadMCPToolDescriptions() (mcpToolDescriptionCatalog, error) {
	var catalog mcpToolDescriptionCatalog
	if err := toml.Unmarshal(mcpToolDescriptionsTOML, &catalog); err != nil {
		return mcpToolDescriptionCatalog{}, fmt.Errorf("parse MCP tool descriptions: %w", err)
	}
	expected := map[string][]string{
		ToolOptions:  {},
		ToolStart:    {"agent", modelParameter, "reasoning_effort", "skill", "prompt", "prompt_file"},
		ToolWait:     {handleParameter},
		ToolContinue: {handleParameter, "prompt", "prompt_file"},
		ToolCancel:   {handleParameter},
	}
	if len(catalog.Tools) != len(expected) {
		return mcpToolDescriptionCatalog{}, fmt.Errorf(
			"MCP tool descriptions define %d tools; expected %d", len(catalog.Tools), len(expected))
	}
	for name, parameters := range expected {
		tool, ok := catalog.Tools[name]
		if !ok {
			return mcpToolDescriptionCatalog{}, fmt.Errorf("MCP tool description %q is missing", name)
		}
		if strings.TrimSpace(tool.Description) == "" {
			return mcpToolDescriptionCatalog{}, fmt.Errorf("MCP tool description %q is empty", name)
		}
		if name == ToolWait {
			if strings.Count(tool.Description, waitMinutesPlaceholder) != 1 {
				return mcpToolDescriptionCatalog{}, fmt.Errorf(
					"MCP tool %q description must contain %q exactly once", name, waitMinutesPlaceholder)
			}
		} else if strings.Contains(tool.Description, waitMinutesPlaceholder) {
			return mcpToolDescriptionCatalog{}, fmt.Errorf(
				"MCP tool %q description cannot contain %q", name, waitMinutesPlaceholder)
		}
		if len(tool.Parameters) != len(parameters) {
			return mcpToolDescriptionCatalog{}, fmt.Errorf(
				"MCP tool %q defines %d parameters; expected %d", name, len(tool.Parameters), len(parameters))
		}
		for _, parameter := range parameters {
			if strings.TrimSpace(tool.Parameters[parameter]) == "" {
				return mcpToolDescriptionCatalog{}, fmt.Errorf(
					"MCP tool %q parameter description %q is missing", name, parameter)
			}
		}
	}
	return catalog, nil
}

func mcpInputSchema[In any](toolName string, description mcpToolDescription) (*jsonschema.Schema, error) {
	schema, err := jsonschema.ForType(reflect.TypeFor[In](), &jsonschema.ForOptions{})
	if err != nil {
		return nil, fmt.Errorf("build MCP tool %q input schema: %w", toolName, err)
	}
	for name, property := range schema.Properties {
		text, ok := description.Parameters[name]
		if !ok {
			return nil, fmt.Errorf("MCP tool %q has no description for inferred parameter %q", toolName, name)
		}
		property.Description = text
	}
	for name := range description.Parameters {
		if _, ok := schema.Properties[name]; !ok {
			return nil, fmt.Errorf("MCP tool %q describes unknown parameter %q", toolName, name)
		}
	}
	return schema, nil
}

func renderMCPToolDescription(description string, waitTimeoutMinutes int) string {
	return strings.ReplaceAll(description, waitMinutesPlaceholder, strconv.Itoa(waitTimeoutMinutes))
}
