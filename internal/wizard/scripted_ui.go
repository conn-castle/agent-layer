package wizard

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"maps"
	"os"
	"slices"
	"sort"
	"strings"
)

// ScriptedUI implements UI from a strict JSON answer file.
type ScriptedUI struct {
	answers scriptedAnswers
	used    map[string]struct{}
	out     io.Writer
}

type scriptedAnswers struct {
	Select      map[string]string   `json:"select"`
	MultiSelect map[string][]string `json:"multi_select"`
	Confirm     map[string]bool     `json:"confirm"`
	Input       map[string]string   `json:"input"`
	SecretInput map[string]string   `json:"secret_input"`
}

// NewScriptedUIFromFile loads a scripted wizard UI from a JSON answer file.
func NewScriptedUIFromFile(path string) (*ScriptedUI, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- path is an explicit user-provided wizard answer file.
	if err != nil {
		return nil, err
	}
	var answers scriptedAnswers
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&answers); err != nil {
		return nil, fmt.Errorf("decode wizard answers %s: %w", path, err)
	}
	var extra any
	if err := decoder.Decode(&extra); err == nil {
		return nil, fmt.Errorf("decode wizard answers %s: multiple JSON values", path)
	} else if !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("decode wizard answers %s: trailing data: %w", path, err)
	}
	return &ScriptedUI{answers: answers, used: make(map[string]struct{})}, nil
}

// AssertComplete returns an error when the answer file contains unused prompts.
func (ui *ScriptedUI) AssertComplete() error {
	var unused []string
	collectUnused := func(kind string, answerKeys iter.Seq[string]) {
		for answerKey := range answerKeys {
			usedKey := scriptedAnswerKey(kind, answerKey)
			if _, ok := ui.used[usedKey]; !ok {
				unused = append(unused, kind+": "+answerKey)
			}
		}
	}
	collectUnused("select", maps.Keys(ui.answers.Select))
	collectUnused("multi_select", maps.Keys(ui.answers.MultiSelect))
	collectUnused("confirm", maps.Keys(ui.answers.Confirm))
	collectUnused("input", maps.Keys(ui.answers.Input))
	collectUnused("secret_input", maps.Keys(ui.answers.SecretInput))
	if len(unused) == 0 {
		return nil
	}
	sort.Strings(unused)
	return fmt.Errorf("wizard answers contain unused prompt(s): %v", unused)
}

// Select applies a scripted single-choice answer for title.
func (ui *ScriptedUI) Select(title string, options []string, current *string) error {
	answer, answerKey, ok := lookupScriptedAnswer(ui.answers.Select, title)
	if !ok {
		return missingScriptedAnswer("select", title)
	}
	if !slices.Contains(options, answer) {
		return fmt.Errorf("wizard select answer %q for %q is not one of %v", answer, title, options)
	}
	*current = answer
	ui.markUsed("select", answerKey)
	return nil
}

// MultiSelect applies a scripted multi-choice answer for title.
func (ui *ScriptedUI) MultiSelect(title string, options []string, selected *[]string) error {
	title, _, _ = strings.Cut(title, cliSkillsStatusHeading)
	answer, answerKey, ok := lookupScriptedAnswer(ui.answers.MultiSelect, title)
	if !ok {
		return missingScriptedAnswer("multi_select", title)
	}
	for _, value := range answer {
		if !slices.Contains(options, value) {
			return fmt.Errorf("wizard multi-select answer %q for %q is not one of %v", value, title, options)
		}
	}

	*selected = append((*selected)[:0], answer...)
	ui.markUsed("multi_select", answerKey)
	return nil
}

// Confirm applies a scripted yes/no answer for title.
func (ui *ScriptedUI) Confirm(title string, value *bool) error {
	return applyScriptedAnswer(ui, "confirm", ui.answers.Confirm, title, value)
}

// Input applies a scripted text answer for title.
func (ui *ScriptedUI) Input(title string, value *string) error {
	return applyScriptedAnswer(ui, "input", ui.answers.Input, title, value)
}

// SecretInput applies a scripted secret answer for title.
func (ui *ScriptedUI) SecretInput(title string, value *string) error {
	return applyScriptedAnswer(ui, "secret_input", ui.answers.SecretInput, title, value)
}

// Note emits informational wizard screens without consuming scripted answers.
func (ui *ScriptedUI) Note(title, body string) error {
	out := ui.out
	if out == nil {
		out = os.Stderr
	}
	_, _ = fmt.Fprintf(out, "%s\n%s\n", title, body)
	return nil
}

func (ui *ScriptedUI) markUsed(kind string, title string) {
	ui.used[scriptedAnswerKey(kind, title)] = struct{}{}
}

func scriptedAnswerKey(kind string, title string) string {
	return kind + "\x00" + title
}

func missingScriptedAnswer(kind string, title string) error {
	return fmt.Errorf("wizard answers missing %s prompt %q", kind, title)
}

// applyScriptedAnswer stores the scripted answer for title in value and marks it used.
func applyScriptedAnswer[T any](ui *ScriptedUI, kind string, answers map[string]T, title string, value *T) error {
	answer, answerKey, ok := lookupScriptedAnswer(answers, title)
	if !ok {
		return missingScriptedAnswer(kind, title)
	}
	*value = answer
	ui.markUsed(kind, answerKey)
	return nil
}

// lookupScriptedAnswer matches the full title first, then its first line.
func lookupScriptedAnswer[T any](answers map[string]T, title string) (T, string, bool) {
	if answer, ok := answers[title]; ok {
		return answer, title, true
	}
	shortTitle := firstScriptedTitleLine(title)
	answer, ok := answers[shortTitle]
	return answer, shortTitle, ok
}

func firstScriptedTitleLine(title string) string {
	first, _, _ := strings.Cut(title, "\n")
	return first
}
