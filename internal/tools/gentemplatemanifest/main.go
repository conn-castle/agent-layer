//go:build tools
// +build tools

package main

import (
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/conn-castle/agent-layer/internal/install"
	"github.com/conn-castle/agent-layer/internal/templates"
)

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
	root, err := filepath.Abs(*repoRoot)
	if err != nil {
		fatalf("resolve repo root: %v", err)
	}
	data, err := buildManifest(root, *ver, time.Now())
	if err != nil {
		fatalf("%v", err)
	}
	if err := os.MkdirAll(filepath.Dir(*output), 0o755); err != nil {
		fatalf("mkdir output dir: %v", err)
	}
	if err := os.WriteFile(*output, data, 0o644); err != nil {
		fatalf("write %s: %v", *output, err)
	}
}

// buildManifest encodes the release manifest from the templates and CLI skills
// catalog under root.
func buildManifest(root string, ver string, generatedAt time.Time) ([]byte, error) {
	sources, err := collectTemplateSources(root)
	if err != nil {
		return nil, fmt.Errorf("collect template sources: %w", err)
	}
	catalogPrefixes, err := catalogSkillPathPrefixes(root)
	if err != nil {
		return nil, fmt.Errorf("load CLI skills catalog prefixes: %w", err)
	}
	files, err := manifestFiles(sources)
	if err != nil {
		return nil, fmt.Errorf("build manifest entries: %w", err)
	}
	return install.EncodeReleaseTemplateManifest(ver, generatedAt, files, catalogPrefixes)
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
			return nil, fmt.Errorf("required retained template root %s: %w", name, err)
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
	dirs := []string{"instructions", "skills-catalog", "docs/agent-layer"}
	for _, dir := range dirs {
		absDir := filepath.Join(root, templateRoot, dir)
		if _, err := os.Stat(absDir); err != nil {
			return nil, fmt.Errorf("required retained template root %s: %w", dir, err)
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

// manifestFiles maps each destination path to its template bytes.
func manifestFiles(sources []templateSource) (map[string][]byte, error) {
	files := make(map[string][]byte, len(sources)*2)
	for _, source := range sources {
		for _, destPath := range source.dests {
			if _, exists := files[destPath]; exists {
				return nil, fmt.Errorf("duplicate destination path %s", destPath)
			}
			files[destPath] = source.content
		}
	}
	return files, nil
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
	case strings.HasPrefix(templatePath, "skills-catalog/"):
		// Catalog skills materialize at .agent-layer/skills/<id>/... when the
		// wizard installs them; mirror the destination so manifest paths match
		// the runtime classifier.
		suffix := strings.TrimPrefix(templatePath, "skills-catalog/")
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

func catalogSkillPathPrefixes(root string) ([]string, error) {
	data, err := os.ReadFile(filepath.Join(root, "internal", "templates", "cli-skills-catalog.toml"))
	if err != nil {
		return nil, err
	}
	entries, err := templates.ParseCLISkillCatalog(data, func(path string) ([]byte, error) {
		return os.ReadFile(filepath.Join(root, "internal", "templates", path))
	})
	if err != nil {
		return nil, err
	}
	var out []string
	for _, entry := range entries {
		if entry.Repository == "" {
			out = append(out, ".agent-layer/skills/"+entry.ID+"/")
		}
	}
	sort.Strings(out)
	return out, nil
}

func fatalf(format string, args ...any) {
	_, _ = fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
