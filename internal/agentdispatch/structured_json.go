package agentdispatch

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
)

const (
	structuredJSONBufferBytes = 64 * 1024
	structuredJSONMaxDepth    = 256
	structuredJSONKeyBytes    = 256
	jsonErrorKey              = "error"
	jsonFalseLiteral          = "false"
	jsonMessageKey            = "message"
	jsonTextKey               = "text"
	jsonTypeKey               = "type"
)

// selectiveJSONReader validates one JSON value while retaining only the small
// set of fields used by Agent Dispatch. Unselected strings are scanned without
// buffering their contents, so command output does not determine memory use.
type selectiveJSONReader struct {
	reader *bufio.Reader
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

func newSelectiveJSONReader(reader io.Reader) *selectiveJSONReader {
	return &selectiveJSONReader{reader: bufio.NewReaderSize(reader, structuredJSONBufferBytes)}
}

func (p *selectiveJSONReader) discard() error {
	_, err := io.Copy(io.Discard, p.reader)
	return err
}

func (p *selectiveJSONReader) next() (map[string]any, error) {
	first, err := p.readNonSpace()
	if err != nil {
		return nil, err
	}
	if first != '{' {
		return nil, fmt.Errorf("structured event must be a JSON object")
	}
	value := make(map[string]any)
	if err := p.readObject(value, nil, 1); err != nil {
		return nil, err
	}
	return value, nil
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
	for {
		if err := p.readValue(result, path, depth+1); err != nil {
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
		return p.readObject(result, path, depth)
	case '[':
		return p.readArray(result, path, depth)
	case '"':
		keep := retainedStructuredPath(path)
		limit := 0
		if keep {
			limit = -1
		}
		value, _, err := p.readString(limit)
		if err != nil {
			return err
		}
		if keep {
			setStructuredPath(result, path, value)
		}
		return nil
	case 't':
		if err := p.readLiteral("rue"); err != nil {
			return err
		}
		if retainedStructuredPath(path) {
			setStructuredPath(result, path, true)
		}
		return nil
	case 'f':
		if err := p.readLiteral(jsonFalseLiteral[1:]); err != nil {
			return err
		}
		if retainedStructuredPath(path) {
			setStructuredPath(result, path, false)
		}
		return nil
	case 'n':
		return p.readLiteral("ull")
	default:
		if first == '-' || first >= '0' && first <= '9' {
			return p.readNumber(first)
		}
		return fmt.Errorf("structured event contains invalid JSON value")
	}
}

func (p *selectiveJSONReader) readString(limit int) (string, bool, error) {
	var encoded bytes.Buffer
	complete := limit != 0
	if complete {
		encoded.WriteByte('"')
	}
	escaped := false
	for {
		value, err := p.reader.ReadByte()
		if err != nil {
			return "", false, unexpectedJSONEnd(err)
		}
		if !escaped && value < 0x20 {
			return "", false, fmt.Errorf("structured event string contains an unescaped control character")
		}
		if complete {
			if limit < 0 || encoded.Len() < limit+2 {
				encoded.WriteByte(value)
			} else {
				complete = false
			}
		}
		if escaped {
			if value == 'u' {
				for range 4 {
					hex, readErr := p.reader.ReadByte()
					if readErr != nil {
						return "", false, unexpectedJSONEnd(readErr)
					}
					if !isHex(hex) {
						return "", false, fmt.Errorf("structured event string contains an invalid unicode escape")
					}
					if complete {
						if limit < 0 || encoded.Len() < limit+2 {
							encoded.WriteByte(hex)
						} else {
							complete = false
						}
					}
				}
			} else if !bytes.ContainsRune([]byte(`"\\/bfnrt`), rune(value)) {
				return "", false, fmt.Errorf("structured event string contains an invalid escape")
			}
			escaped = false
			continue
		}
		if value == '\\' {
			escaped = true
			continue
		}
		if value == '"' {
			if !complete {
				return "", false, nil
			}
			decoded, err := strconv.Unquote(encoded.String())
			if err != nil {
				return "", false, fmt.Errorf("decode structured event string: %w", err)
			}
			return decoded, true, nil
		}
	}
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

func retainedStructuredPath(path []string) bool {
	if len(path) == 1 {
		switch path[0] {
		case jsonTypeKey, "thread_id", "threadId", "id", jsonMessageKey, jsonTextKey, "reason", jsonErrorKey, "result", "session_id", "sessionId", "is_error", "subtype":
			return true
		}
	}
	if len(path) == 2 {
		return (path[0] == "event" && path[1] == jsonTypeKey) ||
			(path[0] == jsonErrorKey && (path[1] == jsonMessageKey || path[1] == "reason")) ||
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
