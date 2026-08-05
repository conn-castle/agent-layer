package agentdispatch

import (
	"io"
	"strings"
	"testing"
)

// readTestStructuredLines drives the production structured-event reader over a
// raw JSONL stream and returns every event it emitted.
func readTestStructuredLines(t *testing.T, stream string) []providerEvent {
	t.Helper()
	var events []providerEvent
	if err := readStructuredEventsWithLineage(strings.NewReader(stream), io.Discard, AgentCodex, "", false, func(event providerEvent) error {
		events = append(events, event)
		return nil
	}, nil); err != nil {
		t.Fatalf("reading structured events failed: %v", err)
	}
	return events
}

// TestMalformedStructuredEventsAreRejectedAndSkipped covers the contract that
// makes a dispatch survive a misbehaving provider: a record the parser cannot
// prove well-formed is never reduced into an event, its rejection is reported
// with a diagnostic reason, and the next record on the stream is still
// delivered. A provider that emits one bad line must not cost the run its
// remaining output.
func TestMalformedStructuredEventsAreRejectedAndSkipped(t *testing.T) {
	deepObject := strings.Repeat(`{"a":`, structuredJSONMaxDepth+2) + "1" + strings.Repeat("}", structuredJSONMaxDepth+2)
	deepArray := `{"a":` + strings.Repeat("[", structuredJSONMaxDepth+2) + strings.Repeat("]", structuredJSONMaxDepth+2) + "}"
	oversizedMetadata := `{"thread_id":"` + strings.Repeat("t", structuredJSONKeyBytes+1) + `"}`

	tests := []struct {
		name       string
		record     string
		wantReason string
	}{
		{"top-level array", `["type"]`, "must be a JSON object"},
		{"unquoted key", `{type:"turn.failed"}`, "object key must be a string"},
		{"missing colon", `{"type" "turn.failed"}`, `must be followed by ':'`},
		{"missing object separator", `{"type":"a" "id":"b"}`, "object values must be separated"},
		{"missing array separator", `{"a":["x" "y"]}`, "array values must be separated"},
		{"invalid value token", `{"type":@}`, "invalid JSON value"},
		{"invalid literal", `{"type":tru}`, "invalid JSON literal"},
		{"unescaped control character", "{\"type\":\"a\tb\"}", "unescaped control character"},
		{"invalid string escape", `{"type":"a\qb"}`, "invalid escape"},
		{"invalid unicode escape", `{"type":"a\u00zz"}`, "invalid unicode escape"},
		{"malformed number", `{"type":1.2.3}`, "invalid JSON number"},
		{"oversized number", `{"type":-` + strings.Repeat("9", 80) + `}`, "invalid JSON number"},
		{"object nested past depth limit", deepObject, "maximum JSON nesting depth"},
		{"array nested past depth limit", deepArray, "maximum JSON nesting depth"},
		{"unterminated string", `{"type":"turn.failed`, "unexpected EOF"},
		{"truncated object", `{"type":"turn.failed"`, "unexpected EOF"},
		{"oversized retained metadata", oversizedMetadata, "metadata exceeded"},
		{"multiple values on one line", `{"type":"turn.completed"}{"type":"turn.completed"}`, "multiple values"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			events := readTestStructuredLines(t, test.record+"\n"+`{"type":"turn.completed"}`+"\n")
			if len(events) != 2 {
				t.Fatalf("events = %#v, want one rejection followed by one recovered event", events)
			}
			if events[0].Kind != eventProgress || events[0].Activity != invalidStructuredEvent {
				t.Fatalf("rejection event = %#v", events[0])
			}
			if !strings.Contains(events[0].Reason, test.wantReason) {
				t.Fatalf("rejection reason = %q, want it to explain %q", events[0].Reason, test.wantReason)
			}
			if events[1].Kind != eventComplete {
				t.Fatalf("recovered event = %#v, want the following record to still be delivered", events[1])
			}
		})
	}
}

// TestStructuredEventsAcceptFullJSONValueGrammar covers provider records whose
// well-formed JSON uses the parts of the grammar the reducers never read.
// The selective parser validates the entire record, so a number, literal, or
// escape it mishandles would reject an event the provider considers valid.
func TestStructuredEventsAcceptFullJSONValueGrammar(t *testing.T) {
	tests := []struct {
		name   string
		record string
		want   string
	}{
		{"numeric fields", `{"type":"agent_message","usage":{"cost":-1.5e-3,"tokens":42},"message":"numbers parsed"}`, "numbers parsed"},
		{"literal fields", `{"type":"agent_message","done":true,"cached":false,"parent":null,"message":"literals parsed"}`, "literals parsed"},
		{"unicode escapes", `{"type":"agent_message","message":"café ✓"}`, "café ✓"},
		{"control escapes", `{"type":"agent_message","message":"line\nbreak\ttab\\slash\"quote\""}`, "line\nbreak\ttab\\slash\"quote\""},
		{"insignificant whitespace", "{ \"type\" : \"agent_message\" ,\t\"message\" :\r \"whitespace parsed\" }", "whitespace parsed"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			events := readTestStructuredLines(t, test.record+"\n")
			if len(events) != 1 {
				t.Fatalf("events = %#v, want exactly one reduced event", events)
			}
			if events[0].Kind != eventAnswer || events[0].Answer != test.want {
				t.Fatalf("event = %#v, want answer %q", events[0], test.want)
			}
		})
	}
}
