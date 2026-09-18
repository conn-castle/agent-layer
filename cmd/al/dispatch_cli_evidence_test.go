package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDispatchInspectAndOutputCLIAddressInvocationID(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".agent-layer"), 0o700); err != nil {
		t.Fatal(err)
	}
	id := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	dir := filepath.Join(root, ".agent-layer", "tmp", "runs", id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	events := filepath.Join(dir, "provider.events")
	if err := os.WriteFile(events, []byte("cli-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	record := map[string]any{
		"id":                     id,
		"name":                   "tiny-round-capacitor",
		"agent":                  "codex",
		"provider_version":       "0.144.1",
		"mode":                   "fresh",
		"state":                  "pending",
		"recovery_state":         "retry_safe",
		"revision":               1,
		"started_at":             now,
		"updated_at":             now,
		"answer_path":            filepath.Join(dir, "result.md"),
		"stdout_path":            filepath.Join(dir, "provider.stdout"),
		"stderr_path":            filepath.Join(dir, "provider.stderr"),
		"events_path":            events,
		"launch_protocol":        "intent-before-start",
		"provider_launch_intent": true,
		"termination_confirmed":  false,
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "dispatch.json"), append(encoded, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)

	inspect := newDispatchInspectCmd()
	var inspectOut bytes.Buffer
	inspect.SetOut(&inspectOut)
	inspect.SetArgs([]string{id})
	if err := inspect.Execute(); err != nil {
		t.Fatalf("al dispatch inspect: %v", err)
	}
	if !bytes.Contains(inspectOut.Bytes(), []byte(id)) {
		t.Fatalf("inspect CLI omitted invocation id: %s", inspectOut.Bytes())
	}
	var inspected struct {
		InvocationID string `json:"invocation_id"`
	}
	if err := json.Unmarshal(inspectOut.Bytes(), &inspected); err != nil {
		t.Fatalf("inspect CLI JSON: %v", err)
	}
	if inspected.InvocationID != id || bytes.Contains(inspectOut.Bytes(), []byte("launch_protocol")) {
		t.Fatalf("inspect CLI = %#v", inspected)
	}

	output := newDispatchOutputCmd()
	var outputOut bytes.Buffer
	output.SetOut(&outputOut)
	output.SetArgs([]string{id, "--artifact", "events"})
	if err := output.Execute(); err != nil {
		t.Fatalf("al dispatch output: %v", err)
	}
	var retrieved struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(outputOut.Bytes(), &retrieved); err != nil {
		t.Fatalf("output CLI JSON: %v", err)
	}
	if retrieved.Content != "cli-bytes" {
		t.Fatalf("output CLI = %#v", retrieved)
	}
}

func TestDispatchOutputRequiresArtifactFlag(t *testing.T) {
	output := newDispatchOutputCmd()
	output.SetOut(io.Discard)
	output.SetErr(io.Discard)
	output.SetArgs([]string{"tiny-round-capacitor"})
	err := output.Execute()
	if err == nil {
		t.Fatal("output accepted a missing --artifact flag")
	}
	if !strings.Contains(err.Error(), `required flag(s) "artifact" not set`) {
		t.Fatalf("output missing artifact error = %v", err)
	}
}
