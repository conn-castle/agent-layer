package envfile

import (
	"strings"
	"testing"
)

func TestParse_ScannerErrorOnOverlongLine(t *testing.T) {
	// bufio.Scanner reports ErrTooLong for tokens larger than its max token size.
	content := strings.Repeat("A", 1024*128)
	if _, err := Parse(content); err == nil || !strings.Contains(err.Error(), "read") {
		t.Fatalf("expected scanner read error for overlong line, got %v", err)
	}
}

func TestParseLine_SingleQuotedTooShort(t *testing.T) {
	_, _, ok, err := parseLine("KEY='")
	if ok {
		t.Fatalf("expected parseLine to report no parsed key/value on error")
	}
	if err == nil || !strings.Contains(err.Error(), "unterminated quoted value") {
		t.Fatalf("expected unterminated single-quoted value error, got %v", err)
	}
}

func TestUnescapeDoubleQuotedValue_CarriageReturnEscape(t *testing.T) {
	got := unescapeDoubleQuotedValue("line1\\rline2")
	if got != "line1\rline2" {
		t.Fatalf("unescapeDoubleQuotedValue carriage return = %q", got)
	}
}
