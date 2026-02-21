package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
)

func TestPromptNumberedChoice_DefaultOnEmpty(t *testing.T) {
	in := strings.NewReader("\n")
	out := &bytes.Buffer{}
	idx, err := promptNumberedChoice(in, out, []string{"alpha", "beta"}, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if idx != 1 {
		t.Errorf("expected default index 1, got %d", idx)
	}
}

func TestPromptNumberedChoice_SelectsExplicit(t *testing.T) {
	in := strings.NewReader("1\n")
	out := &bytes.Buffer{}
	idx, err := promptNumberedChoice(in, out, []string{"alpha", "beta"}, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if idx != 0 {
		t.Errorf("expected index 0, got %d", idx)
	}
}

func TestPromptNumberedChoice_RetryOnInvalid(t *testing.T) {
	in := strings.NewReader("bad\n2\n")
	out := &bytes.Buffer{}
	idx, err := promptNumberedChoice(in, out, []string{"alpha", "beta"}, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if idx != 1 {
		t.Errorf("expected index 1, got %d", idx)
	}
	if !strings.Contains(out.String(), "Invalid choice") {
		t.Error("expected retry message in output")
	}
}

func TestPromptNumberedChoice_EOFReturnsDefault(t *testing.T) {
	in := strings.NewReader("")
	out := &bytes.Buffer{}
	idx, err := promptNumberedChoice(in, out, []string{"alpha", "beta"}, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if idx != 0 {
		t.Errorf("expected default index 0, got %d", idx)
	}
}

func TestPromptBoolChoice_SelectsTrue(t *testing.T) {
	in := strings.NewReader("1\n")
	out := &bytes.Buffer{}
	result, err := promptBoolChoice(in, out, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != true {
		t.Errorf("expected true, got %v", result)
	}
}

func TestPromptBoolChoice_DefaultFalse(t *testing.T) {
	in := strings.NewReader("\n")
	out := &bytes.Buffer{}
	result, err := promptBoolChoice(in, out, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != false {
		t.Errorf("expected false, got %v", result)
	}
	if strings.Contains(out.String(), "(recommended)") {
		t.Error("unexpected recommended marker in output")
	}
}

func TestPromptEnumChoice_SelectsOption(t *testing.T) {
	field := config.FieldDef{
		Key:  "test.field",
		Type: config.FieldEnum,
		Options: []config.FieldOption{
			{Value: "alpha", Description: "first option"},
			{Value: "beta", Description: "second option"},
		},
	}
	in := strings.NewReader("2\n")
	out := &bytes.Buffer{}
	result, err := promptEnumChoice(in, out, "alpha", field)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "beta" {
		t.Errorf("expected 'beta', got %v", result)
	}
}

func TestPromptConfigChoice_Bool(t *testing.T) {
	field := config.FieldDef{Key: "test.enabled", Type: config.FieldBool}
	in := strings.NewReader("1\n")
	out := &bytes.Buffer{}
	result, err := promptConfigChoice(in, out, "test.enabled", false, field)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != true {
		t.Errorf("expected true, got %v", result)
	}
}

func TestPromptConfigChoice_Enum(t *testing.T) {
	field := config.FieldDef{
		Key:  "test.mode",
		Type: config.FieldEnum,
		Options: []config.FieldOption{
			{Value: "a"},
			{Value: "b"},
		},
	}
	in := strings.NewReader("1\n")
	out := &bytes.Buffer{}
	result, err := promptConfigChoice(in, out, "test.mode", "b", field)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "a" {
		t.Errorf("expected 'a', got %v", result)
	}
}

func TestPromptBoolChoice_NonBoolValue_Errors(t *testing.T) {
	in := strings.NewReader("\n")
	out := &bytes.Buffer{}
	_, err := promptBoolChoice(in, out, "true") // string, not bool
	if err == nil {
		t.Fatal("expected error for non-bool manifest value")
	}
	if !strings.Contains(err.Error(), "expected bool") {
		t.Errorf("expected 'expected bool' in error, got: %s", err.Error())
	}
}

func TestPromptEnumChoice_UnknownValue_StrictEnum_Errors(t *testing.T) {
	field := config.FieldDef{
		Key:  "test.mode",
		Type: config.FieldEnum,
		Options: []config.FieldOption{
			{Value: "a"},
			{Value: "b"},
		},
		AllowCustom: false,
	}
	in := strings.NewReader("\n")
	out := &bytes.Buffer{}
	_, err := promptEnumChoice(in, out, "nonexistent", field)
	if err == nil {
		t.Fatal("expected error for unrecognized manifest value in strict enum")
	}
	if !strings.Contains(err.Error(), "not a valid option") {
		t.Errorf("expected 'not a valid option' in error, got: %s", err.Error())
	}
}

func TestPromptEnumChoice_UnknownValue_AllowCustom_DefaultsToFirst(t *testing.T) {
	field := config.FieldDef{
		Key:  "test.model",
		Type: config.FieldEnum,
		Options: []config.FieldOption{
			{Value: "alpha"},
			{Value: "beta"},
		},
		AllowCustom: true,
	}
	in := strings.NewReader("\n") // accept default
	out := &bytes.Buffer{}
	result, err := promptEnumChoice(in, out, "custom-value", field)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "alpha" {
		t.Errorf("expected 'alpha' (first option as default), got %v", result)
	}
}
