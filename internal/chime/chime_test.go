package chime

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func TestHandleProviderFilters(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, provider, input, wantOut string
		wantPlay                       bool
	}{
		{"claude accepted", ProviderClaude, `{"hook_event_name":"Stop","stop_hook_active":false}`, "", true},
		{"claude continuation", ProviderClaude, `{"hook_event_name":"Stop","stop_hook_active":true}`, "", false},
		{"claude background", ProviderClaude, `{"hook_event_name":"Stop","stop_hook_active":false,"background_tasks":[{}]}`, "", false},
		{"claude cron", ProviderClaude, `{"hook_event_name":"Stop","stop_hook_active":false,"session_crons":[{}]}`, "", false},
		{"codex accepted", ProviderCodex, `{"hook_event_name":"Stop","stop_hook_active":false,"future":true}`, "{}\n", true},
		{"codex continuation", ProviderCodex, `{"hook_event_name":"Stop","stop_hook_active":true}`, "{}\n", false},
		{"antigravity accepted", ProviderAntigravity, `{"fullyIdle":true,"terminationReason":"model_stop"}`, "{\"decision\":\"allow\"}\n", true},
		{"antigravity empty error", ProviderAntigravity, `{"fullyIdle":true,"terminationReason":"model_stop","error":""}`, "{\"decision\":\"allow\"}\n", true},
		{"antigravity busy", ProviderAntigravity, `{"fullyIdle":false,"terminationReason":"model_stop"}`, "{\"decision\":\"allow\"}\n", false},
		{"antigravity other termination", ProviderAntigravity, `{"fullyIdle":true,"terminationReason":"error"}`, "{\"decision\":\"allow\"}\n", false},
		{"antigravity error", ProviderAntigravity, `{"fullyIdle":true,"terminationReason":"model_stop","error":"boom"}`, "{\"decision\":\"allow\"}\n", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			plays := 0
			var stdout, stderr bytes.Buffer
			err := Handle(tc.provider, strings.NewReader(tc.input), &stdout, &stderr, SoundRunnerFunc(func() error {
				plays++
				return nil
			}))
			if err != nil {
				t.Fatalf("Handle: %v", err)
			}
			if got := stdout.String(); got != tc.wantOut {
				t.Fatalf("stdout = %q, want %q", got, tc.wantOut)
			}
			wantPlays := 0
			if tc.wantPlay {
				wantPlays = 1
			}
			if plays != wantPlays {
				t.Fatalf("sound plays = %d, want %d", plays, wantPlays)
			}
			if stderr.Len() != 0 {
				t.Fatalf("unexpected stderr: %q", stderr.String())
			}
		})
	}
}

func TestHandleRejectsMalformedOrIncompleteEvents(t *testing.T) {
	t.Parallel()
	tests := []struct{ name, provider, input, errorPart string }{
		{"malformed", ProviderClaude, `{`, "decode"},
		{"oversized", ProviderClaude, `{"hook_event_name":"Stop","stop_hook_active":false,"padding":"` + strings.Repeat("x", maxHookEventBytes) + `"}`, "exceeds"},
		{"oversized after valid event", ProviderClaude, `{"hook_event_name":"Stop","stop_hook_active":false}` + strings.Repeat(" ", maxHookEventBytes) + `x`, "exceeds"},
		{"trailing object", ProviderCodex, `{"hook_event_name":"Stop","stop_hook_active":false} {}`, "trailing JSON"},
		{"trailing garbage", ProviderCodex, `{"hook_event_name":"Stop","stop_hook_active":false} x`, "trailing"},
		{"non-object", ProviderClaude, `[]`, "JSON object"},
		{"null", ProviderClaude, `null`, "JSON object"},
		{"wrong event", ProviderClaude, `{"hook_event_name":"SubagentStop","stop_hook_active":false}`, "must be Stop"},
		{"missing event", ProviderCodex, `{"stop_hook_active":false}`, "must be Stop"},
		{"missing stop active", ProviderCodex, `{"hook_event_name":"Stop"}`, "must be a boolean"},
		{"wrong stop active", ProviderClaude, `{"hook_event_name":"Stop","stop_hook_active":"false"}`, "invalid claude"},
		{"wrong background tasks", ProviderClaude, `{"hook_event_name":"Stop","stop_hook_active":false,"background_tasks":{}}`, "invalid claude"},
		{"null background tasks", ProviderClaude, `{"hook_event_name":"Stop","stop_hook_active":false,"background_tasks":null}`, "invalid claude"},
		{"null session crons", ProviderClaude, `{"hook_event_name":"Stop","stop_hook_active":false,"session_crons":null}`, "invalid claude"},
		{"missing idle", ProviderAntigravity, `{"terminationReason":"model_stop"}`, "fullyIdle"},
		{"missing termination", ProviderAntigravity, `{"fullyIdle":true}`, "terminationReason"},
		{"wrong error", ProviderAntigravity, `{"fullyIdle":true,"terminationReason":"model_stop","error":{}}`, "invalid antigravity"},
		{"null error", ProviderAntigravity, `{"fullyIdle":true,"terminationReason":"model_stop","error":null}`, "invalid antigravity"},
		{"unsupported", "other", `{}`, "unsupported provider"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			plays := 0
			var stdout bytes.Buffer
			err := Handle(tc.provider, strings.NewReader(tc.input), &stdout, &bytes.Buffer{}, SoundRunnerFunc(func() error {
				plays++
				return nil
			}))
			if err == nil || !strings.Contains(err.Error(), tc.errorPart) {
				t.Fatalf("error = %v, want containing %q", err, tc.errorPart)
			}
			if plays != 0 || stdout.Len() != 0 {
				t.Fatalf("invalid event played %d sounds and wrote %q", plays, stdout.String())
			}
		})
	}
}

func TestHandleAcceptsExactMaximumInput(t *testing.T) {
	t.Parallel()
	input := `{"hook_event_name":"Stop","stop_hook_active":false}`
	input += strings.Repeat(" ", maxHookEventBytes-len(input))
	plays := 0
	if err := Handle(ProviderClaude, strings.NewReader(input), io.Discard, io.Discard, SoundRunnerFunc(func() error {
		plays++
		return nil
	})); err != nil {
		t.Fatalf("Handle exact-limit input: %v", err)
	}
	if plays != 1 {
		t.Fatalf("sound plays = %d, want 1", plays)
	}
}

func TestHandleSoundFailureStillAllowsProvider(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	err := Handle(ProviderCodex, strings.NewReader(`{"hook_event_name":"Stop","stop_hook_active":false}`), &stdout, &stderr, SoundRunnerFunc(func() error {
		return errors.New("audio unavailable")
	}))
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if stdout.String() != "{}\n" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "audio unavailable") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

type failingWriter struct{ err error }

func (w failingWriter) Write([]byte) (int, error) { return 0, w.err }

func TestHandleReportsProviderResponseWriteFailure(t *testing.T) {
	t.Parallel()
	writeErr := errors.New("output closed")
	err := Handle(
		ProviderCodex,
		strings.NewReader(`{"hook_event_name":"Stop","stop_hook_active":true}`),
		failingWriter{err: writeErr},
		io.Discard,
		SoundRunnerFunc(func() error { return nil }),
	)
	if !errors.Is(err, writeErr) || !strings.Contains(err.Error(), "write codex hook response") {
		t.Fatalf("Handle error = %v, want wrapped response write failure", err)
	}
}

type fakeSoundProcess struct {
	startErr    error
	waitStarted chan struct{}
	releaseWait chan struct{}
}

func (p *fakeSoundProcess) Start() error { return p.startErr }

func (p *fakeSoundProcess) Wait() error {
	close(p.waitStarted)
	<-p.releaseWait
	return nil
}

func TestStartAndReapSoundLifecycle(t *testing.T) {
	t.Parallel()

	t.Run("start failure does not wait", func(t *testing.T) {
		t.Parallel()
		process := &fakeSoundProcess{
			startErr:    errors.New("start denied"),
			waitStarted: make(chan struct{}),
			releaseWait: make(chan struct{}),
		}
		err := startAndReapSound(process)
		if err == nil || !strings.Contains(err.Error(), "start denied") {
			t.Fatalf("startAndReapSound error = %v", err)
		}
		select {
		case <-process.waitStarted:
			t.Fatal("Wait must not run after Start fails")
		case <-time.After(20 * time.Millisecond):
		}
	})

	t.Run("successful start schedules non-blocking wait", func(t *testing.T) {
		t.Parallel()
		process := &fakeSoundProcess{
			waitStarted: make(chan struct{}),
			releaseWait: make(chan struct{}),
		}
		if err := startAndReapSound(process); err != nil {
			t.Fatalf("startAndReapSound: %v", err)
		}
		select {
		case <-process.waitStarted:
		case <-time.After(time.Second):
			t.Fatal("Wait was not scheduled after Start succeeded")
		}
		close(process.releaseWait)
	})
}

func TestSystemSoundCommand(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, goos, wantCommand string
		wantArgs                []string
		lookPath                func(string) (string, error)
		wantError               string
	}{
		{
			name:        "macOS system sound",
			goos:        "darwin",
			wantCommand: "/usr/bin/afplay",
			wantArgs:    []string{"/System/Library/Sounds/Blow.aiff"},
			lookPath:    func(string) (string, error) { return "", errors.New("must not be called") },
		},
		{
			name:        "Linux desktop event sound",
			goos:        operatingSystemLinux,
			wantCommand: "/usr/bin/canberra-gtk-play",
			wantArgs:    []string{"--id=complete"},
			lookPath:    func(string) (string, error) { return "/usr/bin/canberra-gtk-play", nil },
		},
		{
			name:      "Linux command missing",
			goos:      operatingSystemLinux,
			lookPath:  func(string) (string, error) { return "", errors.New("not found") },
			wantError: "requires canberra-gtk-play on PATH",
		},
		{
			name:      "unsupported operating system",
			goos:      "windows",
			lookPath:  func(string) (string, error) { return "", errors.New("must not be called") },
			wantError: "unsupported on windows",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			command, args, err := systemSoundCommand(tc.goos, tc.lookPath)
			if tc.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantError) {
					t.Fatalf("error = %v, want containing %q", err, tc.wantError)
				}
				return
			}
			if err != nil {
				t.Fatalf("systemSoundCommand: %v", err)
			}
			if command != tc.wantCommand || strings.Join(args, "\x00") != strings.Join(tc.wantArgs, "\x00") {
				t.Fatalf("command = %q %#v, want %q %#v", command, args, tc.wantCommand, tc.wantArgs)
			}
		})
	}
}
