package agentdispatch

import (
	"bufio"
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
		{"literal multi-byte UTF-8", `{"type":"agent_message","message":"café ✓"}`, "café ✓"},
		{"unicode escapes", `{"type":"agent_message","message":"caf\u00e9 \u2713"}`, "café ✓"},
		{"uppercase unicode escape digits", `{"type":"agent_message","message":"caf\u00E9"}`, "café"},
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

func parseStructuredEvent(t *testing.T, input string) (structuredRecord, error) {
	t.Helper()
	parser := newSelectiveJSONReader()
	parser.reset(strings.NewReader(input))
	return parser.next()
}

func TestStructuredEventParserRejectsMalformedProviderOutput(t *testing.T) {
	for _, test := range []struct {
		name   string
		input  string
		wanted string
	}{
		{"not an object", `["agent_message"]`, "must be a JSON object"},
		{"unquoted key", `{type:"agent_message"}`, "object key must be a string"},
		{"key without a value", `{"type" "agent_message"}`, "must be followed by ':'"},
		{"members not separated", `{"type":"a" "id":"b"}`, "values must be separated by ','"},
		{"array elements not separated", `{"usage":[1 2]}`, "array values must be separated by ','"},
		{"truncated mid-string", `{"type":"agent_mess`, "unexpected EOF"},
		{"truncated mid-object", `{"type":"agent_message"`, "unexpected EOF"},
		{"raw control character", "{\"type\":\"agent\tmessage\"}", "unescaped control character"},
		{"unknown escape", `{"type":"agent\qmessage"}`, "invalid escape"},
		{"malformed unicode escape", `{"type":"agent\u00zzmessage"}`, "invalid unicode escape"},
		{"truncated literal", `{"is_error":tru}`, "invalid JSON literal"},
		{"unknown value", `{"type":?}`, "invalid JSON value"},
		{"malformed number", `{"id":1.2.3}`, "invalid JSON number"},
		{"unbounded number", `{"id":` + strings.Repeat("9", 128) + `}`, "invalid JSON number"},
	} {
		t.Run(test.name, func(t *testing.T) {
			// A provider line that is not valid JSON has to be reported as such.
			// Returning a partially filled record would let a reducer act on
			// fields that were never actually emitted.
			_, err := parseStructuredEvent(t, test.input)
			if err == nil || !strings.Contains(err.Error(), test.wanted) {
				t.Fatalf("%s error = %v", test.name, err)
			}
		})
	}
}

func TestStructuredEventParserRejectsUnboundedNesting(t *testing.T) {
	depth := structuredJSONMaxDepth + 8
	input := `{"message":` + strings.Repeat(`{"message":`, depth) + `"deep"` + strings.Repeat(`}`, depth+1)

	// Provider output is untrusted input. Following arbitrary nesting would
	// recurse until the process dies rather than skipping one bad event.
	_, err := parseStructuredEvent(t, input)
	if err == nil || !strings.Contains(err.Error(), "maximum JSON nesting depth") {
		t.Fatalf("deeply nested event error = %v", err)
	}
}

func TestStructuredEventParserRetainsOnlyAllowlistedFields(t *testing.T) {
	unselected := strings.Repeat("x", 4*structuredJSONBufferBytes)
	record, err := parseStructuredEvent(t, `{`+
		`"transcript":"`+unselected+`",`+
		`"type":"agent_message",`+
		`"usage":{"input_tokens":1024,"cache":null},`+
		`"tools":["read","write"],`+
		`"is_error":false,`+
		`"result":"done"`+
		`}`)
	if err != nil {
		t.Fatal(err)
	}
	// The parser validates everything but keeps only the fields the reducers
	// read, so a provider transcript far larger than the read buffer cannot
	// decide how much memory a dispatch holds.
	if _, retained := record.Fields["transcript"]; retained {
		t.Fatalf("unselected provider content was retained: %#v", record.Fields)
	}
	if _, retained := record.Fields["usage"]; retained {
		t.Fatalf("unselected nested object was retained: %#v", record.Fields)
	}
	if record.Fields["type"] != "agent_message" || record.Fields["result"] != "done" ||
		record.Fields["is_error"] != false {
		t.Fatalf("retained fields = %#v", record.Fields)
	}
}

func TestStructuredEventParserDecodesJSONEscapesInRetainedValues(t *testing.T) {
	record, err := parseStructuredEvent(t, `{"result":"line\nb\/cA\t\"quoted\""}`)
	if err != nil {
		t.Fatal(err)
	}
	// These values are surfaced to the caller as the agent's answer, so an
	// escape decoded incorrectly would change the text the user reads.
	if record.Fields["result"] != "line\nb/cA\t\"quoted\"" {
		t.Fatalf("decoded result = %q", record.Fields["result"])
	}
}

func TestStructuredEventMetadataFailsRatherThanTruncatingSilently(t *testing.T) {
	oversized := strings.Repeat("s", structuredJSONKeyBytes+1)

	// A session identifier is used to resume the conversation. A truncated one
	// would look valid and then silently address the wrong session, so the
	// event has to fail instead.
	_, err := parseStructuredEvent(t, `{"session_id":"`+oversized+`"}`)
	if err == nil || !strings.Contains(err.Error(), "metadata exceeded") {
		t.Fatalf("oversized metadata error = %v", err)
	}

	// The final answer is the one field that is allowed to be shortened,
	// because losing the whole event would lose the agent's work entirely.
	parser := newSelectiveJSONReader()
	parser.retainedStringBytes = 4
	parser.reset(strings.NewReader(`{"result":"abcdefghij"}`))
	record, err := parser.next()
	if err != nil {
		t.Fatal(err)
	}
	answer, ok := record.Fields[jsonResultKey].(string)
	if !ok || answer != "abcd"+truncatedAnswerNotice {
		t.Fatalf("truncated answer = %#v", record.Fields[jsonResultKey])
	}
}

func TestStructuredEventParserConsumesEachRecordCompletely(t *testing.T) {
	parser := newSelectiveJSONReader()
	parser.reset(strings.NewReader(
		`{"type":"first","transcript":"` + strings.Repeat("y", 1024) + `"}` +
			`{"type":"second","usage":[1,2,{"nested":true}]}`,
	))

	first, err := parser.next()
	if err != nil || first.Fields["type"] != "first" {
		t.Fatalf("first record = %#v, %v", first.Fields, err)
	}
	// Each record must leave the reader positioned exactly at the next one, or
	// a long event would swallow the events that follow it.
	second, err := parser.next()
	if err != nil || second.Fields["type"] != "second" {
		t.Fatalf("second record = %#v, %v", second.Fields, err)
	}
	if _, err := parser.next(); err != io.EOF {
		t.Fatalf("reader did not end after the last record: %v", err)
	}
}

func TestStructuredEventParserAcceptsEmptyContainers(t *testing.T) {
	record, err := parseStructuredEvent(t, `{"type":"agent_message","usage":{},"tools":[],"session_id":""}`)
	if err != nil {
		t.Fatal(err)
	}
	if record.Fields["type"] != "agent_message" || record.Fields["session_id"] != "" {
		t.Fatalf("record with empty containers = %#v", record.Fields)
	}

	empty, err := parseStructuredEvent(t, `{}`)
	if err != nil || len(empty.Fields) != 0 {
		t.Fatalf("empty record = %#v, %v", empty.Fields, err)
	}
}

func TestStructuredJSONLineReaderIsolatesEachLine(t *testing.T) {
	source := bufio.NewReaderSize(strings.NewReader(
		`{"type":"first"}`+"\n"+"not json\n"+`{"type":"second"}`+"\n",
	), structuredJSONBufferBytes)
	parser := newSelectiveJSONReader()

	first := &structuredJSONLineReader{source: source}
	parser.reset(first)
	record, err := parser.next()
	if err != nil || record.Fields["type"] != "first" {
		t.Fatalf("first line record = %#v, %v", record.Fields, err)
	}
	if err := parser.discard(); err != nil {
		t.Fatal(err)
	}

	malformed := &structuredJSONLineReader{source: source}
	parser.reset(malformed)
	if _, err := parser.next(); err == nil {
		t.Fatal("malformed line parsed as a structured event")
	}
	if err := parser.discard(); err != nil {
		t.Fatal(err)
	}

	// One unparseable line must cost exactly one event. Letting the parser read
	// past the newline would consume the next provider event as well.
	last := &structuredJSONLineReader{source: source}
	parser.reset(last)
	record, err = parser.next()
	if err != nil || record.Fields["type"] != "second" {
		t.Fatalf("record after malformed line = %#v, %v", record.Fields, err)
	}
}
