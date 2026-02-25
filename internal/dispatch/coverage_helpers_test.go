package dispatch

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type nonTimeoutNetErr struct{}

func (nonTimeoutNetErr) Error() string   { return "temporary network error" }
func (nonTimeoutNetErr) Timeout() bool   { return false }
func (nonTimeoutNetErr) Temporary() bool { return true }

func TestParseSemver_Branches(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		ok   bool
	}{
		{name: "invalid segment count", raw: "1.2", ok: false},
		{name: "invalid major", raw: "x.2.3", ok: false},
		{name: "invalid minor", raw: "1.x.3", ok: false},
		{name: "invalid patch", raw: "1.2.x", ok: false},
		{name: "valid", raw: "1.2.3", ok: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, _, ok := parseSemver(tt.raw)
			if ok != tt.ok {
				t.Fatalf("parseSemver(%q) ok=%v, want %v", tt.raw, ok, tt.ok)
			}
		})
	}
}

func TestSemverAtLeast_Branches(t *testing.T) {
	tests := []struct {
		name string
		a    string
		b    string
		want bool
	}{
		{name: "invalid a", a: "invalid", b: "1.0.0", want: false},
		{name: "invalid b", a: "1.0.0", b: "invalid", want: false},
		{name: "major greater", a: "2.0.0", b: "1.9.9", want: true},
		{name: "major lower", a: "1.0.0", b: "2.0.0", want: false},
		{name: "minor greater", a: "1.3.0", b: "1.2.9", want: true},
		{name: "minor lower", a: "1.1.9", b: "1.2.0", want: false},
		{name: "patch equal", a: "1.2.3", b: "1.2.3", want: true},
		{name: "patch lower", a: "1.2.2", b: "1.2.3", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := semverAtLeast(tt.a, tt.b); got != tt.want {
				t.Fatalf("semverAtLeast(%q, %q)=%v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestCacheHelperBranches(t *testing.T) {
	t.Run("isTimeoutError false for non-net error", func(t *testing.T) {
		if isTimeoutError(errors.New("boom")) {
			t.Fatal("expected false for plain error")
		}
	})

	t.Run("isTimeoutError false for non-timeout net error", func(t *testing.T) {
		var err error = nonTimeoutNetErr{}
		if isTimeoutError(err) {
			t.Fatal("expected false for non-timeout net error")
		}
	})

	t.Run("maxDownloadBytes invalid value falls back", func(t *testing.T) {
		sys := &testSystem{
			GetenvFunc: func(key string) string {
				if key == "AL_MAX_DOWNLOAD_BYTES" {
					return "not-a-number"
				}
				return ""
			},
		}
		if got := maxDownloadBytesWithSystem(sys); got != defaultMaxDownloadBytes {
			t.Fatalf("expected default max bytes, got %d", got)
		}
	})

	t.Run("maxDownloadBytes non-positive falls back", func(t *testing.T) {
		sys := &testSystem{
			GetenvFunc: func(key string) string {
				if key == "AL_MAX_DOWNLOAD_BYTES" {
					return "0"
				}
				return ""
			},
		}
		if got := maxDownloadBytesWithSystem(sys); got != defaultMaxDownloadBytes {
			t.Fatalf("expected default max bytes, got %d", got)
		}
	})

	t.Run("downloadHTTPClientWithSystem nil sys uses shared client", func(t *testing.T) {
		if got := downloadHTTPClientWithSystem(nil); got != httpClient {
			t.Fatal("expected shared httpClient for nil system")
		}
	})

	t.Run("fetchChecksumWithSystem nil sys errors", func(t *testing.T) {
		if _, err := fetchChecksumWithSystem(nil, "1.0.0", "al-linux-amd64"); err == nil {
			t.Fatal("expected nil-system error")
		}
	})
}

func TestFetchChecksumWithSystem_ScannerRetryPath(t *testing.T) {
	origHTTP := httpClient
	origSleep := dispatchSleep
	t.Cleanup(func() {
		httpClient = origHTTP
		dispatchSleep = origSleep
	})
	dispatchSleep = func(time.Duration) {}

	readErr := io.ErrUnexpectedEOF
	httpClient = &http.Client{
		Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Body: io.NopCloser(&errorReaderAfterData{
					data: []byte("invalid-line-without-fields"),
					err:  readErr,
				}),
			}, nil
		}),
	}

	sys := &testSystem{}
	_, err := fetchChecksumWithSystem(sys, "1.0.0", "asset")
	if err == nil || !strings.Contains(err.Error(), "read") {
		t.Fatalf("expected read failure after retry path, got %v", err)
	}
}

type errorReaderAfterData struct {
	data []byte
	read bool
	err  error
}

func (r *errorReaderAfterData) Read(p []byte) (int, error) {
	if !r.read {
		r.read = true
		n := copy(p, r.data)
		return n, nil
	}
	return 0, r.err
}
