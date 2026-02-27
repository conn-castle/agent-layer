//go:build tools
// +build tools

package main

import (
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/conn-castle/agent-layer/internal/version"
)

const (
	schemaVersion = 1

	policyMemoryEntries = "memory_entries_v1"
	policyMemoryRoadmap = "memory_roadmap_v1"
	policyAllowlist     = "allowlist_lines_v1"

	markerEntriesStart = "<!-- ENTRIES START -->"
	markerPhasesStart  = "<!-- PHASES START -->"
)

type manifestFileEntry struct {
	Path               string          `json:"path"`
	FullHashNormalized string          `json:"full_hash_normalized"`
	PolicyID           string          `json:"policy_id,omitempty"`
	PolicyPayload      json.RawMessage `json:"policy_payload,omitempty"`
}

type templateManifest struct {
	SchemaVersion int                 `json:"schema_version"`
	Version       string              `json:"version"`
	GeneratedAt   string              `json:"generated_at_utc"`
	Files         []manifestFileEntry `json:"files"`
	Metadata      map[string]any      `json:"metadata,omitempty"`
}

type memoryPolicyPayload struct {
	Marker             string `json:"marker"`
	ManagedSectionHash string `json:"managed_section_hash"`
}

type allowlistPolicyPayload struct {
	UpstreamSet     []string `json:"upstream_set"`
	UpstreamSetHash string   `json:"upstream_set_hash"`
}

type templateSource struct {
	templatePath string
	content      []byte
	dests        []string
}

func main() {
	ver := flag.String("version", "", "release version (for example v0.8.0 or 0.8.0)")
	output := flag.String("output", "", "output manifest path")
	repoRoot := flag.String("repo-root", ".", "repository root")
	flag.Parse()

	if strings.TrimSpace(*ver) == "" {
		fatalf("--version is required")
	}
	if strings.TrimSpace(*output) == "" {
		fatalf("--output is required")
	}
	normalizedVersion, err := version.Normalize(*ver)
	if err != nil {
		fatalf("normalize version %q: %v", *ver, err)
	}
	root, err := filepath.Abs(*repoRoot)
	if err != nil {
		fatalf("resolve repo root: %v", err)
	}

	sources, err := collectTemplateSources(root)
	if err != nil {
		fatalf("collect template sources: %v", err)
	}
	entries, err := buildManifestEntries(sources)
	if err != nil {
		fatalf("build manifest entries: %v", err)
	}
	manifest := templateManifest{
		SchemaVersion: schemaVersion,
		Version:       normalizedVersion,
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
		Files:         entries,
		Metadata: map[string]any{
			"source_version": normalizedVersion,
		},
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		fatalf("encode manifest: %v", err)
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(*output), 0o755); err != nil {
		fatalf("mkdir output dir: %v", err)
	}
	if err := os.WriteFile(*output, data, 0o644); err != nil {
		fatalf("write %s: %v", *output, err)
	}
}

func collectTemplateSources(root string) ([]templateSource, error) {
	templateRoot := "internal/templates"
	// Only include upgrade-managed root templates in the manifest. User-owned seed-only
	// files (.agent-layer/config.toml, .agent-layer/.env) and agent-only internal files
	// (.agent-layer/.gitignore) are intentionally excluded.
	rootFiles := []string{"commands.allow", "gitignore.block"}
	sources := make([]templateSource, 0, 64)
	for _, name := range rootFiles {
		absPath := filepath.Join(root, templateRoot, name)
		if _, err := os.Stat(absPath); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		content, err := os.ReadFile(absPath)
		if err != nil {
			return nil, err
		}
		sources = append(sources, templateSource{
			templatePath: name,
			content:      content,
			dests:        templateDestPaths(name),
		})
	}
	dirs := []string{"instructions", "skills", "docs/agent-layer"}
	for _, dir := range dirs {
		absDir := filepath.Join(root, templateRoot, dir)
		if _, err := os.Stat(absDir); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		var paths []string
		err := filepath.WalkDir(absDir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			rel, relErr := filepath.Rel(filepath.Join(root, templateRoot), path)
			if relErr != nil {
				return relErr
			}
			paths = append(paths, filepath.ToSlash(rel))
			return nil
		})
		if err != nil {
			return nil, err
		}
		sort.Strings(paths)
		for _, templatePath := range paths {
			absPath := filepath.Join(root, templateRoot, templatePath)
			content, err := os.ReadFile(absPath)
			if err != nil {
				return nil, err
			}
			sources = append(sources, templateSource{
				templatePath: templatePath,
				content:      content,
				dests:        templateDestPaths(templatePath),
			})
		}
	}
	return sources, nil
}

func buildManifestEntries(sources []templateSource) ([]manifestFileEntry, error) {
	entries := make([]manifestFileEntry, 0, len(sources)*2)
	seen := make(map[string]struct{}, len(sources)*2)
	for _, source := range sources {
		normalized := normalizeTemplateContent(string(source.content))
		fullHash := hashString(normalized)
		for _, destPath := range source.dests {
			if _, exists := seen[destPath]; exists {
				return nil, fmt.Errorf("duplicate destination path %s", destPath)
			}
			seen[destPath] = struct{}{}
			policyID := ownershipPolicyForPath(destPath)
			payload, err := ownershipPolicyPayload(policyID, source.content)
			if err != nil {
				return nil, fmt.Errorf("build policy payload for %s: %w", destPath, err)
			}
			entries = append(entries, manifestFileEntry{
				Path:               destPath,
				FullHashNormalized: fullHash,
				PolicyID:           policyID,
				PolicyPayload:      payload,
			})
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Path < entries[j].Path
	})
	return entries, nil
}

func templateDestPaths(templatePath string) []string {
	switch {
	case templatePath == "config.toml":
		return []string{".agent-layer/config.toml"}
	case templatePath == "commands.allow":
		return []string{".agent-layer/commands.allow"}
	case templatePath == "env":
		return []string{".agent-layer/.env"}
	case templatePath == "agent-layer.gitignore":
		return []string{".agent-layer/.gitignore"}
	case templatePath == "gitignore.block":
		return []string{".agent-layer/gitignore.block"}
	case strings.HasPrefix(templatePath, "instructions/"):
		suffix := strings.TrimPrefix(templatePath, "instructions/")
		return []string{filepath.ToSlash(filepath.Join(".agent-layer/instructions", suffix))}
	case strings.HasPrefix(templatePath, "skills/"):
		suffix := strings.TrimPrefix(templatePath, "skills/")
		return []string{filepath.ToSlash(filepath.Join(".agent-layer/skills", suffix))}
	case strings.HasPrefix(templatePath, "docs/agent-layer/"):
		suffix := strings.TrimPrefix(templatePath, "docs/agent-layer/")
		return []string{
			filepath.ToSlash(filepath.Join("docs/agent-layer", suffix)),
			filepath.ToSlash(filepath.Join(".agent-layer/templates/docs", suffix)),
		}
	default:
		return nil
	}
}

func ownershipPolicyForPath(relPath string) string {
	switch relPath {
	case ".agent-layer/commands.allow":
		return policyAllowlist
	case "docs/agent-layer/ROADMAP.md":
		return policyMemoryRoadmap
	case "docs/agent-layer/ISSUES.md", "docs/agent-layer/BACKLOG.md", "docs/agent-layer/DECISIONS.md", "docs/agent-layer/COMMANDS.md":
		return policyMemoryEntries
	default:
		return ""
	}
}

func ownershipPolicyPayload(policyID string, content []byte) (json.RawMessage, error) {
	switch policyID {
	case "":
		return nil, nil
	case policyMemoryEntries:
		hash, err := hashManagedMarkerSection(string(content), markerEntriesStart)
		if err != nil {
			return nil, err
		}
		data, err := json.Marshal(memoryPolicyPayload{Marker: markerEntriesStart, ManagedSectionHash: hash})
		if err != nil {
			return nil, err
		}
		return data, nil
	case policyMemoryRoadmap:
		hash, err := hashManagedMarkerSection(string(content), markerPhasesStart)
		if err != nil {
			return nil, err
		}
		data, err := json.Marshal(memoryPolicyPayload{Marker: markerPhasesStart, ManagedSectionHash: hash})
		if err != nil {
			return nil, err
		}
		return data, nil
	case policyAllowlist:
		set, setHash := parseAllowlistSet(normalizeTemplateContent(string(content)))
		data, err := json.Marshal(allowlistPolicyPayload{UpstreamSet: set, UpstreamSetHash: setHash})
		if err != nil {
			return nil, err
		}
		return data, nil
	default:
		return nil, fmt.Errorf("unknown policy %q", policyID)
	}
}

func parseAllowlistSet(normalizedContent string) ([]string, string) {
	lines := strings.Split(normalizedContent, "\n")
	seen := make(map[string]struct{})
	set := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		set = append(set, trimmed)
	}
	sort.Strings(set)
	builder := strings.Builder{}
	for _, line := range set {
		builder.WriteString(line)
		builder.WriteString("\n")
	}
	return set, hashString(builder.String())
}

func hashManagedMarkerSection(content string, marker string) (string, error) {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	lines := strings.Split(normalized, "\n")
	markerLineIndex := -1
	for idx, line := range lines {
		if strings.TrimSpace(line) != marker {
			continue
		}
		if markerLineIndex >= 0 {
			return "", fmt.Errorf("marker %q appears more than once as a standalone line", marker)
		}
		markerLineIndex = idx
	}
	if markerLineIndex < 0 {
		return "", fmt.Errorf("marker %q missing", marker)
	}
	builder := strings.Builder{}
	for idx := 0; idx <= markerLineIndex; idx++ {
		builder.WriteString(lines[idx])
		builder.WriteString("\n")
	}
	managed := normalizeTemplateContent(builder.String())
	return hashString(managed), nil
}

func normalizeTemplateContent(content string) string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	return strings.TrimRight(content, "\n") + "\n"
}

func hashString(content string) string {
	sum := sha256.Sum256([]byte(content))
	return fmt.Sprintf("%x", sum[:])
}

func fatalf(format string, args ...any) {
	_, _ = fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
