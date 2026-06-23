package doctor

import (
	"reflect"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
)

// TestFindUnknownConfigKeys_NamedSliceFieldRecursion exercises the real schema's
// named-slice-field branch (findUnknownConfigKeys: `child.arrayChild != nil`),
// which fires for struct fields like `mcp.servers []MCPServer`. The existing
// array test only reaches the map-valued `[]any` branch via an allowAny parent,
// so a regression in the named-slice recursion would let a typo'd field inside
// `[[mcp.servers]]` (e.g. `comand` for `command`) go unreported by `al doctor`,
// silently accepting a broken MCP server definition. The indexed path
// `mcp.servers[0].<key>` must be surfaced.
func TestFindUnknownConfigKeys_NamedSliceFieldRecursion(t *testing.T) {
	t.Parallel()
	schema := buildSchema(reflect.TypeOf(config.Config{}))
	raw := map[string]any{
		"mcp": map[string]any{
			"servers": []any{
				map[string]any{
					"id":     "srv",
					"comand": "oops", // typo of "command"
				},
			},
		},
	}

	var details []configUnknownKeyDetail
	findUnknownConfigKeys(raw, schema, "", &details)

	found := false
	for _, d := range details {
		if d.Path == "mcp.servers[0].comand" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected unknown key mcp.servers[0].comand to be reported, got %#v", details)
	}
}

func TestFindUnknownConfigKeys_TraversesMapValuesWithSchema(t *testing.T) {
	type dynamicEntry struct {
		Known string `toml:"known"`
	}
	type dynamicRoot struct {
		Dynamic map[string]dynamicEntry `toml:"dynamic"`
	}

	schema := buildSchema(reflect.TypeOf(dynamicRoot{}))
	raw := map[string]any{
		"dynamic": map[string]any{
			"entry": map[string]any{
				"known": "ok",
				"extra": "unexpected",
			},
		},
	}

	var details []configUnknownKeyDetail
	findUnknownConfigKeys(raw, schema, "", &details)

	if len(details) != 1 {
		t.Fatalf("expected 1 unknown key detail, got %#v", details)
	}
	if details[0].Path != "dynamic.entry.extra" {
		t.Fatalf("path = %q, want %q", details[0].Path, "dynamic.entry.extra")
	}
	if !reflect.DeepEqual(details[0].Allowed, []string{"known"}) {
		t.Fatalf("allowed = %#v, want %#v", details[0].Allowed, []string{"known"})
	}
}

func TestFindUnknownConfigKeys_TraversesArrayOfMapValuesWithSchema(t *testing.T) {
	type nestedEntry struct {
		Known string `toml:"known"`
	}
	type nestedRoot struct {
		Dynamic map[string][]map[string]nestedEntry `toml:"dynamic"`
	}

	schema := buildSchema(reflect.TypeOf(nestedRoot{}))
	raw := map[string]any{
		"dynamic": map[string]any{
			"bucket": []any{
				map[string]any{
					"entry": map[string]any{
						"known": "ok",
						"extra": "unexpected",
					},
				},
			},
		},
	}

	var details []configUnknownKeyDetail
	findUnknownConfigKeys(raw, schema, "", &details)

	if len(details) != 1 {
		t.Fatalf("expected 1 unknown key detail, got %#v", details)
	}
	if details[0].Path != "dynamic.bucket[0].entry.extra" {
		t.Fatalf("path = %q, want %q", details[0].Path, "dynamic.bucket[0].entry.extra")
	}
	if !reflect.DeepEqual(details[0].Allowed, []string{"known"}) {
		t.Fatalf("allowed = %#v, want %#v", details[0].Allowed, []string{"known"})
	}
}

func TestJoinConfigPath_QuotesSpecialCharacters(t *testing.T) {
	if got := joinConfigPath("agents.vscode", "model"); got != "agents.vscode.model" {
		t.Fatalf("joinConfigPath normal key = %q, want %q", got, "agents.vscode.model")
	}
	if got := joinConfigPath("agents.vscode", "foo.bar"); got != `agents.vscode["foo.bar"]` {
		t.Fatalf("joinConfigPath dotted key = %q, want %q", got, `agents.vscode["foo.bar"]`)
	}
	if got := joinConfigPath("", "foo.bar"); got != `["foo.bar"]` {
		t.Fatalf("joinConfigPath root dotted key = %q, want %q", got, `["foo.bar"]`)
	}
	if got := joinConfigIndexedPath("mcp.servers", "foo.bar", 2); got != `mcp.servers["foo.bar"][2]` {
		t.Fatalf("joinConfigIndexedPath dotted key = %q, want %q", got, `mcp.servers["foo.bar"][2]`)
	}
}
