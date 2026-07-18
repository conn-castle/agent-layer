package agentdispatch

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"unicode/utf8"
)

const (
	structuredJSONBufferBytes = 64 * 1024
	structuredJSONMaxDepth    = 256
	structuredJSONKeyBytes    = 256
	jsonErrorKey              = "error"
	jsonFalseLiteral          = "false"
	jsonMessageKey            = "message"
	jsonReasonKey             = "reason"
	jsonResultKey             = "result"
	jsonTextKey               = "text"
	jsonTypeKey               = "type"
	claudeLineageContentLimit = 256
)

type structuredScalar struct {
	Present bool
	Valid   bool
	Value   string
}

type claudeToolUseBlock struct {
	Type structuredScalar
	ID   structuredScalar
	Name structuredScalar
}

type claudeStructuredProjection struct {
	ParentToolUseID structuredScalar
	TaskID          structuredScalar
	ToolUseID       structuredScalar
	Status          structuredScalar
	TaskType        structuredScalar
	Content         []claudeToolUseBlock
	InvalidReason   string
}

type structuredRecord struct {
	Fields map[string]any
	Claude claudeStructuredProjection
}

// selectiveJSONReader validates one JSON value while retaining only the small
// set of fields used by Agent Dispatch. Unselected strings are scanned without
// buffering their contents, so command output does not determine memory use.
type selectiveJSONReader struct {
	reader              *bufio.Reader
	retainedStringBytes int
	record              *structuredRecord
	block               *claudeToolUseBlock
}

// structuredJSONLineReader exposes one JSONL record at a time without
// retaining the record. It consumes the newline but reports EOF to the JSON
// parser, allowing a malformed record to be skipped without losing the next
// provider event.
type structuredJSONLineReader struct {
	source    *bufio.Reader
	ended     bool
	sourceEOF bool
	sourceErr error
}

func (r *structuredJSONLineReader) Read(data []byte) (int, error) {
	if r.ended {
		return 0, io.EOF
	}
	for index := range data {
		value, err := r.source.ReadByte()
		if err != nil {
			r.ended = true
			if err == io.EOF {
				r.sourceEOF = true
			} else {
				r.sourceErr = err
			}
			if index > 0 {
				return index, nil
			}
			return 0, err
		}
		if value == '\n' {
			r.ended = true
			if index > 0 {
				return index, nil
			}
			return 0, io.EOF
		}
		data[index] = value
	}
	return len(data), nil
}

func newSelectiveJSONReader() *selectiveJSONReader {
	return &selectiveJSONReader{
		reader:              bufio.NewReaderSize(bytes.NewReader(nil), structuredJSONBufferBytes),
		retainedStringBytes: maxRetainedAnswerBytes,
	}
}

func (p *selectiveJSONReader) reset(reader io.Reader) {
	p.reader.Reset(reader)
}

func (p *selectiveJSONReader) discard() error {
	_, err := io.Copy(io.Discard, p.reader)
	return err
}

func (p *selectiveJSONReader) next() (structuredRecord, error) {
	first, err := p.readNonSpace()
	if err != nil {
		return structuredRecord{}, err
	}
	if first != '{' {
		return structuredRecord{}, fmt.Errorf("structured event must be a JSON object")
	}
	p.record = &structuredRecord{Fields: make(map[string]any)}
	if err := p.readObject(p.record.Fields, nil, 1); err != nil {
		return structuredRecord{}, err
	}
	result := *p.record
	p.record = nil
	return result, nil
}

func (p *selectiveJSONReader) readObject(result map[string]any, path []string, depth int) error {
	if depth > structuredJSONMaxDepth {
		return fmt.Errorf("structured event exceeded maximum JSON nesting depth %d", structuredJSONMaxDepth)
	}
	next, err := p.readNonSpace()
	if err != nil {
		return unexpectedJSONEnd(err)
	}
	if next == '}' {
		return nil
	}
	if err := p.reader.UnreadByte(); err != nil {
		return err
	}
	seen := make(map[string]struct{})
	for {
		quote, err := p.readNonSpace()
		if err != nil {
			return unexpectedJSONEnd(err)
		}
		if quote != '"' {
			return fmt.Errorf("structured event object key must be a string")
		}
		key, complete, err := p.readString(structuredJSONKeyBytes)
		if err != nil {
			return err
		}
		colon, err := p.readNonSpace()
		if err != nil {
			return unexpectedJSONEnd(err)
		}
		if colon != ':' {
			return fmt.Errorf("structured event object key must be followed by ':'")
		}
		if _, duplicate := seen[key]; duplicate && p.retainsClaudeScalar(appendPath(path, key)) {
			p.invalidateClaude(lineageReasonStructureInvalid)
		}
		seen[key] = struct{}{}
		var childPath []string
		if complete {
			childPath = appendPath(path, key)
		} else {
			childPath = []string{"\x00"}
		}
		if err := p.readValue(result, childPath, depth+1); err != nil {
			return err
		}
		separator, err := p.readNonSpace()
		if err != nil {
			return unexpectedJSONEnd(err)
		}
		switch separator {
		case '}':
			return nil
		case ',':
			continue
		default:
			return fmt.Errorf("structured event object values must be separated by ','")
		}
	}
}

func (p *selectiveJSONReader) readArray(result map[string]any, path []string, depth int) error {
	if depth > structuredJSONMaxDepth {
		return fmt.Errorf("structured event exceeded maximum JSON nesting depth %d", structuredJSONMaxDepth)
	}
	next, err := p.readNonSpace()
	if err != nil {
		return unexpectedJSONEnd(err)
	}
	if next == ']' {
		return nil
	}
	if err := p.reader.UnreadByte(); err != nil {
		return err
	}
	content := len(path) == 2 && path[0] == jsonMessageKey && path[1] == "content"
	for {
		if content {
			block := claudeToolUseBlock{}
			p.block = &block
			if err := p.readValue(result, path, depth+1); err != nil {
				p.block = nil
				return err
			}
			p.block = nil
			if len(p.record.Claude.Content) < claudeLineageContentLimit {
				p.record.Claude.Content = append(p.record.Claude.Content, block)
			} else {
				p.invalidateClaude(lineageReasonLimitExceeded)
			}
		} else if err := p.readValue(result, path, depth+1); err != nil {
			return err
		}
		separator, err := p.readNonSpace()
		if err != nil {
			return unexpectedJSONEnd(err)
		}
		switch separator {
		case ']':
			return nil
		case ',':
			continue
		default:
			return fmt.Errorf("structured event array values must be separated by ','")
		}
	}
}

func (p *selectiveJSONReader) readValue(result map[string]any, path []string, depth int) error {
	first, err := p.readNonSpace()
	if err != nil {
		return unexpectedJSONEnd(err)
	}
	switch first {
	case '{':
		p.setClaudeScalar(path, "", false)
		return p.readObject(result, path, depth)
	case '[':
		p.setClaudeScalar(path, "", false)
		return p.readArray(result, path, depth)
	case '"':
		keep := retainedStructuredPath(path)
		keepClaude := p.retainsClaudeScalar(path)
		limit := 0
		truncatable := false
		if keep {
			limit, truncatable = p.retainedStructuredStringLimit(path)
		} else if keepClaude {
			limit = structuredJSONKeyBytes
		}
		value, complete, err := p.readString(limit)
		if err != nil {
			return err
		}
		if keep {
			if !complete {
				if !truncatable {
					return fmt.Errorf("structured event metadata exceeded %d bytes", limit)
				}
				value += truncatedAnswerNotice
			}
			setStructuredPath(result, path, value)
		}
		p.setClaudeScalar(path, value, complete)
		return nil
	case 't':
		if err := p.readLiteral("rue"); err != nil {
			return err
		}
		if retainedStructuredPath(path) {
			setStructuredPath(result, path, true)
		}
		p.setClaudeScalar(path, "", false)
		return nil
	case 'f':
		if err := p.readLiteral(jsonFalseLiteral[1:]); err != nil {
			return err
		}
		if retainedStructuredPath(path) {
			setStructuredPath(result, path, false)
		}
		p.setClaudeScalar(path, "", false)
		return nil
	case 'n':
		return p.readLiteral("ull")
	default:
		if first == '-' || first >= '0' && first <= '9' {
			p.setClaudeScalar(path, "", false)
			return p.readNumber(first)
		}
		return fmt.Errorf("structured event contains invalid JSON value")
	}
}

func (p *selectiveJSONReader) retainsClaudeScalar(path []string) bool {
	if len(path) == 1 {
		switch path[0] {
		case "parent_tool_use_id", "task_id", "tool_use_id", "status", "task_type":
			return true
		}
	}
	return p.block != nil && len(path) == 3 && path[0] == jsonMessageKey && path[1] == "content" &&
		(path[2] == jsonTypeKey || path[2] == "id" || path[2] == "name")
}

func (p *selectiveJSONReader) setClaudeScalar(path []string, value string, valid bool) {
	if p.record == nil || !p.retainsClaudeScalar(path) {
		return
	}
	slot := p.claudeScalar(path)
	if slot.Present {
		p.invalidateClaude(lineageReasonStructureInvalid)
	}
	slot.Present = true
	slot.Valid = valid
	slot.Value = value
	if !valid {
		p.invalidateClaude(lineageReasonStructureInvalid)
	}
}

func (p *selectiveJSONReader) claudeScalar(path []string) *structuredScalar {
	if p.block != nil && len(path) == 3 {
		switch path[2] {
		case jsonTypeKey:
			return &p.block.Type
		case "id":
			return &p.block.ID
		default:
			return &p.block.Name
		}
	}
	switch path[0] {
	case "parent_tool_use_id":
		return &p.record.Claude.ParentToolUseID
	case "task_id":
		return &p.record.Claude.TaskID
	case "tool_use_id":
		return &p.record.Claude.ToolUseID
	case "status":
		return &p.record.Claude.Status
	default:
		return &p.record.Claude.TaskType
	}
}

func (p *selectiveJSONReader) invalidateClaude(reason string) {
	if p.record != nil && p.record.Claude.InvalidReason == "" {
		p.record.Claude.InvalidReason = reason
	}
}

// retainedStructuredStringLimit returns the byte ceiling and whether the field
// may be returned with the explicit final-answer truncation notice.
func (p *selectiveJSONReader) retainedStructuredStringLimit(path []string) (int, bool) {
	key := path[len(path)-1]
	switch key {
	case jsonResultKey, jsonMessageKey, jsonTextKey, jsonReasonKey, jsonErrorKey:
		return p.retainedStringBytes, true
	default:
		return structuredJSONKeyBytes, false
	}
}

func (p *selectiveJSONReader) readString(limit int) (string, bool, error) {
	var encoded bytes.Buffer
	complete := limit != 0
	if complete {
		encoded.WriteByte('"')
	}
	safeLength := encoded.Len()
	escaped := false
	for {
		value, err := p.reader.ReadByte()
		if err != nil {
			return "", false, unexpectedJSONEnd(err)
		}
		if !escaped && value < 0x20 {
			return "", false, fmt.Errorf("structured event string contains an unescaped control character")
		}
		if !escaped && value == '"' {
			if limit == 0 {
				return "", false, nil
			}
			if !complete {
				safeLength = 1 + validUTF8PrefixLength(encoded.Bytes()[1:safeLength])
				encoded.Truncate(safeLength)
			}
			encoded.WriteByte('"')
			decoded, decodeErr := strconv.Unquote(encoded.String())
			if decodeErr != nil {
				return "", false, fmt.Errorf("decode structured event string: %w", decodeErr)
			}
			return decoded, complete, nil
		}
		if complete {
			if encoded.Len()-1 < limit {
				encoded.WriteByte(value)
			} else {
				complete = false
			}
		}
		if escaped {
			switch {
			case value == 'u':
				for range 4 {
					hex, readErr := p.reader.ReadByte()
					if readErr != nil {
						return "", false, unexpectedJSONEnd(readErr)
					}
					if !isHex(hex) {
						return "", false, fmt.Errorf("structured event string contains an invalid unicode escape")
					}
					if complete {
						if encoded.Len()-1 < limit {
							encoded.WriteByte(hex)
						} else {
							complete = false
						}
					}
				}
			case value == '/' && complete:
				encoded.Truncate(encoded.Len() - 2)
				encoded.WriteByte('/')
			case !bytes.ContainsRune([]byte(`"\\/bfnrt`), rune(value)):
				return "", false, fmt.Errorf("structured event string contains an invalid escape")
			}
			if complete {
				safeLength = encoded.Len()
			}
			escaped = false
			continue
		}
		if value == '\\' {
			escaped = true
			continue
		}
		if complete {
			safeLength = encoded.Len()
		}
	}
}

// validUTF8PrefixLength removes at most one incomplete trailing UTF-8 rune
// while preserving earlier bytes, which remain subject to normal JSON parsing.
func validUTF8PrefixLength(data []byte) int {
	if utf8.Valid(data) {
		return len(data)
	}
	for removed := 1; removed < utf8.UTFMax && removed <= len(data); removed++ {
		if utf8.Valid(data[:len(data)-removed]) {
			return len(data) - removed
		}
	}
	return len(data)
}

func (p *selectiveJSONReader) readLiteral(remainder string) error {
	for i := 0; i < len(remainder); i++ {
		value, err := p.reader.ReadByte()
		if err != nil {
			return unexpectedJSONEnd(err)
		}
		if value != remainder[i] {
			return fmt.Errorf("structured event contains invalid JSON literal")
		}
	}
	return nil
}

func (p *selectiveJSONReader) readNumber(first byte) error {
	var encoded [64]byte
	encoded[0] = first
	length := 1
	for {
		value, err := p.reader.ReadByte()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if value == ',' || value == '}' || value == ']' || isJSONSpace(value) {
			if err := p.reader.UnreadByte(); err != nil {
				return err
			}
			break
		}
		if length == len(encoded) {
			return fmt.Errorf("structured event contains an invalid JSON number")
		}
		encoded[length] = value
		length++
	}
	var number json.Number
	if err := json.Unmarshal(encoded[:length], &number); err != nil {
		return fmt.Errorf("structured event contains an invalid JSON number: %w", err)
	}
	return nil
}

func (p *selectiveJSONReader) readNonSpace() (byte, error) {
	for {
		value, err := p.reader.ReadByte()
		if err != nil {
			return 0, err
		}
		if !isJSONSpace(value) {
			return value, nil
		}
	}
}

// retainedStructuredPath is the allowlist for fields consumed by the Claude
// and Codex reducers in provider.go and event_helpers.go. Reducer contract
// samples in TestStructuredEventsRejectChangedProviderContracts exercise this
// parser boundary so a missing retained path fails with a behavior-level test.
func retainedStructuredPath(path []string) bool {
	if len(path) == 1 {
		switch path[0] {
		case jsonTypeKey, "thread_id", "threadId", "id", jsonMessageKey, jsonTextKey, jsonReasonKey, jsonErrorKey, jsonResultKey, "session_id", "sessionId", "is_error", "subtype":
			return true
		}
	}
	if len(path) == 2 {
		return (path[0] == "event" && path[1] == jsonTypeKey) ||
			(path[0] == jsonErrorKey && (path[1] == jsonMessageKey || path[1] == jsonReasonKey)) ||
			(path[0] == "item" && (path[1] == jsonTypeKey || path[1] == jsonMessageKey || path[1] == jsonTextKey)) ||
			(path[0] == "delta" && (path[1] == jsonTypeKey || path[1] == jsonTextKey))
	}
	return len(path) == 3 && path[0] == "event" && path[1] == "delta" && (path[2] == jsonTypeKey || path[2] == jsonTextKey)
}

func setStructuredPath(result map[string]any, path []string, value any) {
	current := result
	for _, key := range path[:len(path)-1] {
		nested, ok := current[key].(map[string]any)
		if !ok {
			nested = make(map[string]any)
			current[key] = nested
		}
		current = nested
	}
	current[path[len(path)-1]] = value
}

func appendPath(path []string, key string) []string {
	result := make([]string, len(path)+1)
	copy(result, path)
	result[len(path)] = key
	return result
}

func unexpectedJSONEnd(err error) error {
	if err == io.EOF {
		return io.ErrUnexpectedEOF
	}
	return err
}

func isJSONSpace(value byte) bool {
	return value == ' ' || value == '\t' || value == '\n' || value == '\r'
}

func isHex(value byte) bool {
	return value >= '0' && value <= '9' || value >= 'a' && value <= 'f' || value >= 'A' && value <= 'F'
}
