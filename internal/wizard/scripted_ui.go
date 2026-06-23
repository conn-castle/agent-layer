package wizard

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"sort"
	"strings"
)

// ScriptedUI implements UI from a strict JSON answer file.
type ScriptedUI struct {
	answers scriptedAnswers
	used    map[string]struct{}
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
	collectUnused := func(kind string, answers map[string]struct{}) {
		for answerKey := range answers {
			usedKey := scriptedAnswerKey(kind, answerKey)
			if _, ok := ui.used[usedKey]; !ok {
				unused = append(unused, kind+": "+answerKey)
			}
		}
	}
	collectUnused("select", stringMapKeys(ui.answers.Select))
	collectUnused("multi_select", stringSliceMapKeys(ui.answers.MultiSelect))
	collectUnused("confirm", boolMapKeys(ui.answers.Confirm))
	collectUnused("input", stringMapKeys(ui.answers.Input))
	collectUnused("secret_input", stringMapKeys(ui.answers.SecretInput))
	if len(unused) == 0 {
		return nil
	}
	sort.Strings(unused)
	return fmt.Errorf("wizard answers contain unused prompt(s): %v", unused)
}

// Select applies a scripted single-choice answer for title.
func (ui *ScriptedUI) Select(title string, options []string, current *string) error {
	answer, answerKey, ok := lookupStringScriptedAnswer(ui.answers.Select, title)
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
	answer, answerKey, ok := lookupStringSliceScriptedAnswer(ui.answers.MultiSelect, title)
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
	answer, answerKey, ok := lookupBoolScriptedAnswer(ui.answers.Confirm, title)
	if !ok {
		return missingScriptedAnswer("confirm", title)
	}
	*value = answer
	ui.markUsed("confirm", answerKey)
	return nil
}

// Input applies a scripted text answer for title.
func (ui *ScriptedUI) Input(title string, value *string) error {
	answer, answerKey, ok := lookupStringScriptedAnswer(ui.answers.Input, title)
	if !ok {
		return missingScriptedAnswer("input", title)
	}
	*value = answer
	ui.markUsed("input", answerKey)
	return nil
}

// SecretInput applies a scripted secret answer for title.
func (ui *ScriptedUI) SecretInput(title string, value *string) error {
	answer, answerKey, ok := lookupStringScriptedAnswer(ui.answers.SecretInput, title)
	if !ok {
		return missingScriptedAnswer("secret_input", title)
	}
	*value = answer
	ui.markUsed("secret_input", answerKey)
	return nil
}

// Note accepts informational wizard screens without requiring scripted answers.
func (ui *ScriptedUI) Note(string, string) error {
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

func lookupStringScriptedAnswer(answers map[string]string, title string) (string, string, bool) {
	if answer, ok := answers[title]; ok {
		return answer, title, true
	}
	shortTitle := firstScriptedTitleLine(title)
	answer, ok := answers[shortTitle]
	return answer, shortTitle, ok
}

func lookupStringSliceScriptedAnswer(answers map[string][]string, title string) ([]string, string, bool) {
	if answer, ok := answers[title]; ok {
		return answer, title, true
	}
	shortTitle := firstScriptedTitleLine(title)
	answer, ok := answers[shortTitle]
	return answer, shortTitle, ok
}

func lookupBoolScriptedAnswer(answers map[string]bool, title string) (bool, string, bool) {
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

func stringMapKeys(in map[string]string) map[string]struct{} {
	out := make(map[string]struct{}, len(in))
	for key := range in {
		out[key] = struct{}{}
	}
	return out
}

func stringSliceMapKeys(in map[string][]string) map[string]struct{} {
	out := make(map[string]struct{}, len(in))
	for key := range in {
		out[key] = struct{}{}
	}
	return out
}

func boolMapKeys(in map[string]bool) map[string]struct{} {
	out := make(map[string]struct{}, len(in))
	for key := range in {
		out[key] = struct{}{}
	}
	return out
}
