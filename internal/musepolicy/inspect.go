package musepolicy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type policyDocument struct {
	Version int               `json:"schema_version"`
	Rules   []json.RawMessage `json:"rules"`
}

func readDocument(path string) (policyDocument, error) {
	document := policyDocument{Version: 2}
	data, err := readRegular(path)
	if err != nil || data == nil {
		return document, err
	}
	// A present document must explicitly declare the supported schema.
	document.Version = 0
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&document); err != nil {
		return document, fmt.Errorf("read Muse approval policy %s: %w", path, err)
	}
	if !json.Valid(data) || document.Version != 2 {
		return document, fmt.Errorf("muse approval policy %s must use schema_version 2; open it with supported Muse before syncing", path)
	}
	return document, nil
}

type commandRule struct {
	Reason     string `json:"reason"`
	Effect     string `json:"effect"`
	Durability string `json:"durability"`
	Rule       struct {
		Kind string `json:"kind"`
		Root string `json:"workspace_root"`
		Argv any    `json:"argv_prefix"`
	} `json:"rule"`
	valid     bool
	validArgv bool
	argv      []string
}

func decodeCommandRule(raw json.RawMessage) commandRule {
	var rule commandRule
	decoder := json.NewDecoder(bytes.NewReader(raw))
	// Unrecognized numeric argv values must not invalidate ownership metadata.
	decoder.UseNumber()
	rule.valid = decoder.Decode(&rule) == nil
	args, ok := rule.Rule.Argv.([]any)
	if !rule.valid || !ok || len(args) == 0 {
		return rule
	}
	rule.argv = make([]string, len(args))
	for i, arg := range args {
		value, ok := arg.(string)
		if !ok {
			return rule
		} // Includes null elements from this same decode.
		rule.argv[i] = value
	}
	rule.validArgv = rule.argv[0] != ""
	return rule
}

// Ignore argv shape so current-root cleanup can retire malformed owned rules.
func (rule commandRule) matchesWorkspace(root string) bool {
	return rule.valid && rule.Reason == "agent-layer:"+root && rule.Effect == policyAllow &&
		rule.Durability == policyLocalPersistent && rule.Rule.Kind == shellCommandArgvPrefix && rule.Rule.Root == root
}

func (rule commandRule) strictlyOwned() bool {
	root := rule.Rule.Root
	return rule.matchesWorkspace(root) && filepath.IsAbs(root) && filepath.Clean(root) == root && rule.validArgv
}

// RootRules describes the number of strictly owned rules at an exact root.
type RootRules struct {
	Root  string
	Count int
}

// Inspection is a read-only snapshot of native Agent Layer command grants.
type Inspection struct {
	Total                 int
	Roots                 []RootRules
	Orphans               []RootRules
	Unknown               []RootRules
	Nested                []RootRules
	CurrentReceiptOrphan  bool
	CurrentReceiptUnknown bool
}

// Inspect shares ownership classification and orphan detection with sync.
// Missing native storage is an empty policy. It never creates files or locks.
func Inspect(ctx context.Context, directory, currentRoot string) (Inspection, error) {
	if err := ctx.Err(); err != nil {
		return Inspection{}, err
	}
	var result Inspection
	info, err := os.Lstat(directory)
	if os.IsNotExist(err) {
		return result, ctx.Err()
	}
	if err != nil {
		return result, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return result, fmt.Errorf("muse policy directory must be a real directory: %s", directory)
	}
	document, err := readDocument(filepath.Join(directory, "approval-policy.json"))
	if err != nil {
		return result, err
	}
	counts := make(map[string]int)
	for _, raw := range document.Rules {
		if err := ctx.Err(); err != nil {
			return Inspection{}, err
		}
		rule := decodeCommandRule(raw)
		if rule.strictlyOwned() {
			counts[rule.Rule.Root]++
			result.Total++
		}
	}
	roots := make([]string, 0, len(counts))
	for root := range counts {
		roots = append(roots, root)
	}
	sort.Strings(roots)
	for _, root := range roots {
		if err := ctx.Err(); err != nil {
			return Inspection{}, err
		}
		entry := RootRules{Root: root, Count: counts[root]}
		result.Roots = append(result.Roots, entry)
		state := ownershipState(directory, root)
		current := root == currentRoot
		if !current && currentRoot != "" {
			current, _ = SameDirectory(currentRoot, root)
		}
		if err := ctx.Err(); err != nil {
			return Inspection{}, err
		}
		if current && state != ownershipValid {
			if state == ownershipOrphan {
				result.CurrentReceiptOrphan = true
			} else {
				result.CurrentReceiptUnknown = true
			}
			continue
		}
		if state == ownershipOrphan {
			result.Orphans = append(result.Orphans, entry)
			continue
		}
		if state == ownershipUnknown {
			result.Unknown = append(result.Unknown, entry)
			continue
		}
		path := filepath.ToSlash(root)
		if strings.Contains(path, "/.agent-layer/tmp/") || strings.Contains(path, "/.codex/worktrees/") {
			result.Nested = append(result.Nested, entry)
			continue
		}
		for ancestor := filepath.Dir(root); ancestor != root; ancestor = filepath.Dir(ancestor) {
			if counts[ancestor] > 0 {
				result.Nested = append(result.Nested, entry)
				break
			}
			if ancestor == filepath.Dir(ancestor) {
				break
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return Inspection{}, err
	}
	return result, nil
}
