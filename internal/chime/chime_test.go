package chime

import (
	"bytes"
	"errors"
	"strings"
	"testing"
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
		{"claude null optional arrays", ProviderClaude, `{"hook_event_name":"Stop","stop_hook_active":false,"background_tasks":null,"session_crons":null}`, "", true},
		{"codex accepted", ProviderCodex, `{"hook_event_name":"Stop","stop_hook_active":false,"future":true}`, "{}\n", true},
		{"codex continuation", ProviderCodex, `{"hook_event_name":"Stop","stop_hook_active":true}`, "{}\n", false},
		{"antigravity accepted", ProviderAntigravity, `{"fullyIdle":true,"terminationReason":"model_stop"}`, "{\"decision\":\"allow\"}\n", true},
		{"antigravity null error", ProviderAntigravity, `{"fullyIdle":true,"terminationReason":"model_stop","error":null}`, "{\"decision\":\"allow\"}\n", true},
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
		{"oversized", ProviderClaude, `{"hook_event_name":"Stop","stop_hook_active":false,"padding":"` + strings.Repeat("x", maxHookEventBytes) + `"}`, "decode"},
		{"trailing object", ProviderCodex, `{"hook_event_name":"Stop","stop_hook_active":false} {}`, "trailing JSON"},
		{"trailing garbage", ProviderCodex, `{"hook_event_name":"Stop","stop_hook_active":false} x`, "trailing"},
		{"non-object", ProviderClaude, `[]`, "JSON object"},
		{"null", ProviderClaude, `null`, "JSON object"},
		{"wrong event", ProviderClaude, `{"hook_event_name":"SubagentStop","stop_hook_active":false}`, "must be Stop"},
		{"missing event", ProviderCodex, `{"stop_hook_active":false}`, "must be Stop"},
		{"missing stop active", ProviderCodex, `{"hook_event_name":"Stop"}`, "must be a boolean"},
		{"wrong stop active", ProviderClaude, `{"hook_event_name":"Stop","stop_hook_active":"false"}`, "invalid claude"},
		{"wrong background tasks", ProviderClaude, `{"hook_event_name":"Stop","stop_hook_active":false,"background_tasks":{}}`, "invalid claude"},
		{"missing idle", ProviderAntigravity, `{"terminationReason":"model_stop"}`, "fullyIdle"},
		{"missing termination", ProviderAntigravity, `{"fullyIdle":true}`, "terminationReason"},
		{"wrong error", ProviderAntigravity, `{"fullyIdle":true,"terminationReason":"model_stop","error":{}}`, "invalid antigravity"},
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
