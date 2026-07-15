// Package chime filters provider hook events and plays the local notification sound.
package chime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"runtime"
)

const (
	// ProviderClaude identifies the Claude hook event schema.
	ProviderClaude = "claude"
	// ProviderCodex identifies the Codex hook event schema.
	ProviderCodex = "codex"
	// ProviderAntigravity identifies the Antigravity hook event schema.
	ProviderAntigravity  = "antigravity"
	codexResponse        = "{}\n"
	antigravityResponse  = "{\"decision\":\"allow\"}\n"
	operatingSystemLinux = "linux"
	maxHookEventBytes    = 32 * 1024
)

// SoundRunner starts the configured notification sound.
type SoundRunner interface {
	Play() error
}

// SoundRunnerFunc adapts a function to SoundRunner.
type SoundRunnerFunc func() error

// Play starts the function-backed notification sound.
func (f SoundRunnerFunc) Play() error { return f() }

// SystemSoundRunner starts the supported system notification sound asynchronously.
type SystemSoundRunner struct{}

// Play selects the operating system sound command, discards its standard
// streams, and releases it without waiting.
func (SystemSoundRunner) Play() error {
	command, args, err := systemSoundCommand(runtime.GOOS, exec.LookPath)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(context.Background(), command, args...) // #nosec G204 -- command is selected from fixed platform backends.
	cmd.Stdin = nil
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start notification sound: %w", err)
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// systemSoundCommand selects the single supported sound backend for an
// operating system. Linux support is conditional on canberra-gtk-play being on PATH.
func systemSoundCommand(goos string, lookPath func(string) (string, error)) (string, []string, error) {
	switch goos {
	case "darwin":
		return "/usr/bin/afplay", []string{"/System/Library/Sounds/Blow.aiff"}, nil
	case operatingSystemLinux:
		command, err := lookPath("canberra-gtk-play")
		if err != nil {
			return "", nil, fmt.Errorf("linux notification sound requires canberra-gtk-play on PATH: %w", err)
		}
		return command, []string{"--id=complete"}, nil
	default:
		return "", nil, fmt.Errorf("notification sound is unsupported on %s", goos)
	}
}

type stopEvent struct {
	HookEventName   string `json:"hook_event_name"`
	StopHookActive  *bool  `json:"stop_hook_active"`
	BackgroundTasks []any  `json:"background_tasks"`
	SessionCrons    []any  `json:"session_crons"`
}

type antigravityEvent struct {
	FullyIdle         *bool   `json:"fullyIdle"`
	TerminationReason *string `json:"terminationReason"`
	Error             *string `json:"error"`
}

// Handle validates one provider hook event, optionally starts a sound, and emits
// the provider's non-blocking response. Invalid metadata returns an error.
func Handle(provider string, stdin io.Reader, stdout io.Writer, stderr io.Writer, runner SoundRunner) error {
	play, response, err := decision(provider, stdin)
	if err != nil {
		return err
	}
	if play {
		if err := runner.Play(); err != nil {
			_, _ = fmt.Fprintf(stderr, "agent-layer chime: %v\n", err)
		}
	}
	if response != "" {
		if _, err := io.WriteString(stdout, response); err != nil {
			return fmt.Errorf("write %s hook response: %w", provider, err)
		}
	}
	return nil
}

func decision(provider string, stdin io.Reader) (bool, string, error) {
	var raw json.RawMessage
	decoder := json.NewDecoder(io.LimitReader(stdin, maxHookEventBytes))
	if err := decoder.Decode(&raw); err != nil {
		return false, "", fmt.Errorf("agent-layer chime: decode %s hook event: %w", provider, err)
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return false, "", fmt.Errorf("agent-layer chime: %s hook input contains trailing JSON", provider)
		}
		return false, "", fmt.Errorf("agent-layer chime: decode trailing %s hook input: %w", provider, err)
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return false, "", fmt.Errorf("agent-layer chime: %s hook event must be a JSON object", provider)
	}

	switch provider {
	case ProviderClaude, ProviderCodex:
		var event stopEvent
		if err := json.Unmarshal(raw, &event); err != nil {
			return false, "", fmt.Errorf("agent-layer chime: invalid %s hook event: %w", provider, err)
		}
		if event.HookEventName != "Stop" {
			return false, "", fmt.Errorf("agent-layer chime: %s hook_event_name must be Stop", provider)
		}
		if event.StopHookActive == nil {
			return false, "", fmt.Errorf("agent-layer chime: %s stop_hook_active must be a boolean", provider)
		}
		play := !*event.StopHookActive
		response := codexResponse
		if provider == ProviderClaude {
			play = play && len(event.BackgroundTasks) == 0 && len(event.SessionCrons) == 0
			response = ""
		}
		return play, response, nil
	case ProviderAntigravity:
		var event antigravityEvent
		if err := json.Unmarshal(raw, &event); err != nil {
			return false, "", fmt.Errorf("agent-layer chime: invalid antigravity hook event: %w", err)
		}
		if event.FullyIdle == nil {
			return false, "", errors.New("agent-layer chime: antigravity fullyIdle must be a boolean")
		}
		if event.TerminationReason == nil {
			return false, "", errors.New("agent-layer chime: antigravity terminationReason must be a string")
		}
		hasError := event.Error != nil && *event.Error != ""
		return *event.FullyIdle && *event.TerminationReason == "model_stop" && !hasError, antigravityResponse, nil
	default:
		return false, "", fmt.Errorf("agent-layer chime: unsupported provider %q", provider)
	}
}
