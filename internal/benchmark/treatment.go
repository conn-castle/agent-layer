package benchmark

import (
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/conn-castle/agent-layer/internal/sync"
)

const treatmentContainerRoot = "/app"

//go:embed assets/pier_agent_layer.py assets/al_dispatch_gate.py assets/workflow-prompt.md assets/pricing.yaml
var treatmentAssets embed.FS

// TreatmentBundle is the secret-free, immutable effective Agent Layer input.
type TreatmentBundle struct {
	Root              string            `json:"root"`
	Manifest          TreatmentManifest `json:"manifest"`
	ManifestHash      string            `json:"manifest_hash"`
	LinuxBinary       string            `json:"linux_binary"`
	LinuxBinarySHA256 string            `json:"linux_binary_sha256"`
	AdapterPath       string            `json:"adapter_path"`
	AdapterSHA256     string            `json:"adapter_sha256"`
	TemplatesCommit   string            `json:"templates_commit,omitempty"`
	TemplatesDirty    bool              `json:"templates_dirty"`
}

// TreatmentManifest names only files that were actually injected. It cannot
// contain .env, project memory, runtime state, temporary data, or credentials.
type TreatmentManifest struct {
	SchemaVersion          string          `json:"schema_version"`
	Mode                   string          `json:"mode"`
	AgentTimeoutMultiplier float64         `json:"agent_timeout_multiplier"`
	Files                  []TreatmentFile `json:"files"`
	RequiredRoles          []string        `json:"required_dispatch_roles"`
}

// TreatmentFile is the content-addressed declaration for one injected file.
type TreatmentFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// BuildTreatmentBundle stages the canonical shipped templates, synchronizes
// that isolated stage, and includes only its provider-effective projections.
// Repository-local .agent-layer customizations are never treatment inputs. The
// caller owns cleanup after the task adapter has consumed the bundle.
func BuildTreatmentBundle(repoRoot, targetArch, mode string, model Model, effort string) (*TreatmentBundle, error) {
	if targetArch != "amd64" && targetArch != "arm64" {
		return nil, fmt.Errorf("unsupported benchmark Linux architecture %q", targetArch)
	}
	if !validTreatmentMode(mode) {
		return nil, fmt.Errorf("unsupported benchmark treatment mode %q", mode)
	}
	parsedModel, parsedEffort, err := ParseModelSelection(model.Name + ":" + effort)
	if err != nil || parsedModel != model || parsedEffort != effort {
		return nil, fmt.Errorf("unsupported benchmark treatment target %s:%s", model.Name, effort)
	}
	stageParent := filepath.Join(repoRoot, ".agent-layer", "tmp")
	if err := os.MkdirAll(stageParent, 0o700); err != nil {
		return nil, fmt.Errorf("create benchmark staging parent: %w", err)
	}
	root, err := os.MkdirTemp(stageParent, "benchmark-treatment-")
	if err != nil {
		return nil, fmt.Errorf("create benchmark treatment staging: %w", err)
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(root)
		}
	}()
	layer := filepath.Join(root, ".agent-layer")
	if err := os.MkdirAll(layer, 0o700); err != nil {
		return nil, err
	}
	for _, projection := range []struct{ source, destination string }{
		{filepath.Join(repoRoot, "internal", "templates", "instructions"), filepath.Join(layer, "instructions")},
		{filepath.Join(repoRoot, "internal", "templates", "docs", "agent-layer"), filepath.Join(root, "docs", "agent-layer")},
	} {
		if err := copyRequiredTree(projection.source, projection.destination); err != nil {
			return nil, fmt.Errorf("copy clean Agent Layer structural projection: %w", err)
		}
	}
	if mode == TreatmentInstructionsAndSkills {
		if err := copyRequiredTree(filepath.Join(repoRoot, "internal", "templates", "skills"), filepath.Join(layer, "skills")); err != nil {
			return nil, fmt.Errorf("copy Agent Layer skills: %w", err)
		}
		if err := copyRequiredTree(filepath.Join(repoRoot, "internal", "templates", "skills-catalog", "agent-dispatch"), filepath.Join(layer, "skills", "agent-dispatch")); err != nil {
			return nil, fmt.Errorf("copy Agent Dispatch skill: %w", err)
		}
	} else if err := os.MkdirAll(filepath.Join(layer, "skills"), 0o700); err != nil {
		return nil, fmt.Errorf("create empty instructions-only skill source: %w", err)
	}
	for _, name := range []string{"commands.allow", "gitignore.block"} {
		if err := copyRequiredFile(filepath.Join(repoRoot, "internal", "templates", name), filepath.Join(layer, name)); err != nil {
			return nil, err
		}
	}
	requiredRoles := []string(nil)
	if mode == TreatmentInstructionsAndSkills {
		requiredRoles = []string{requiredRolePlanReviewer, requiredRoleImplementer, requiredRoleCodeReviewer}
	}
	configPath := filepath.Join(layer, "config.toml")
	if err := writeNormalizedDispatchConfig(configPath, requiredRoles, model, effort); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(layer, ".env"), []byte("# Intentionally empty; provider authentication is injected separately.\n"), 0o600); err != nil {
		return nil, fmt.Errorf("write empty benchmark environment marker: %w", err)
	}
	if _, err := sync.Run(root); err != nil {
		return nil, fmt.Errorf("synchronize staged Agent Layer treatment: %w", err)
	}
	if err := rewriteTreatmentProjectionRoot(root, root, treatmentContainerRoot); err != nil {
		return nil, fmt.Errorf("normalize staged Agent Layer treatment root: %w", err)
	}
	if mode == TreatmentInstructionsAndSkills {
		workflow, err := treatmentAssets.ReadFile("assets/workflow-prompt.md")
		if err != nil {
			return nil, fmt.Errorf("read embedded benchmark workflow prompt: %w", err)
		}
		if err := os.WriteFile(filepath.Join(root, "workflow-prompt.md"), workflow, 0o600); err != nil {
			return nil, fmt.Errorf("write benchmark workflow prompt: %w", err)
		}
	}
	binary, binaryChecksum := "", ""
	if mode == TreatmentInstructionsAndSkills {
		var err error
		binary, binaryChecksum, err = buildDevelopmentLinuxBinary(repoRoot, root, targetArch)
		if err != nil {
			return nil, err
		}
	}
	adapter, err := treatmentAssets.ReadFile("assets/pier_agent_layer.py")
	if err != nil {
		return nil, fmt.Errorf("read embedded Pier treatment adapter: %w", err)
	}
	adapterPath := filepath.Join(root, "adapter", "pier_agent_layer.py")
	if err := os.MkdirAll(filepath.Dir(adapterPath), 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(adapterPath, adapter, 0o600); err != nil {
		return nil, fmt.Errorf("write Pier treatment adapter: %w", err)
	}
	if mode == TreatmentInstructionsAndSkills {
		gate, err := treatmentAssets.ReadFile("assets/al_dispatch_gate.py")
		if err != nil {
			return nil, fmt.Errorf("read embedded benchmark dispatch gate: %w", err)
		}
		if err := os.WriteFile(filepath.Join(root, "adapter", "al_dispatch_gate.py"), gate, 0o600); err != nil {
			return nil, fmt.Errorf("write benchmark dispatch gate: %w", err)
		}
	}
	adapterHash := sha256.Sum256(adapter)
	manifest, err := treatmentManifest(root, mode, requiredRoles)
	if err != nil {
		return nil, err
	}
	manifestHash, err := hashCanonical(manifest)
	if err != nil {
		return nil, err
	}
	templatesCommit, templatesDirty, err := templatesProvenance(context.Background(), repoRoot)
	if err != nil {
		return nil, err
	}
	cleanup = false
	return &TreatmentBundle{Root: root, Manifest: manifest, ManifestHash: manifestHash, LinuxBinary: binary, LinuxBinarySHA256: binaryChecksum, AdapterPath: adapterPath, AdapterSHA256: hex.EncodeToString(adapterHash[:]), TemplatesCommit: templatesCommit, TemplatesDirty: templatesDirty}, nil
}

func rewriteTreatmentProjectionRoot(root, from, to string) error {
	if strings.TrimSpace(from) == "" || strings.TrimSpace(to) == "" || from == to {
		return fmt.Errorf("treatment projection root rewrite requires distinct non-empty paths")
	}
	restricted, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer func() { _ = restricted.Close() }()
	fromBytes, toBytes := []byte(filepath.ToSlash(from)), []byte(filepath.ToSlash(to))
	return fs.WalkDir(restricted.FS(), ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("treatment contains non-regular file %s", path)
		}
		data, err := restricted.ReadFile(path)
		if err != nil {
			return err
		}
		rewritten := bytes.ReplaceAll(data, fromBytes, toBytes)
		if bytes.Equal(data, rewritten) {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		return restricted.WriteFile(path, rewritten, info.Mode().Perm())
	})
}

func copyRequiredFile(source, destination string) error {
	data, err := os.ReadFile(source) // #nosec G304 -- source is an explicit effective projection selected by BuildTreatmentBundle.
	if err != nil {
		return fmt.Errorf("read %s: %w", source, err)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return err
	}
	return os.WriteFile(destination, data, 0o600) // #nosec G703 -- destination is constructed from the controlled temporary bundle root.
}

func copyRequiredTree(source, destination string) error {
	info, err := os.Stat(source)
	if err != nil {
		return fmt.Errorf("read %s: %w", source, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("required projection %s is not a directory", source)
	}
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("effective projection contains non-regular file %s", path)
		}
		return copyRequiredFile(path, target)
	})
}

func writeNormalizedDispatchConfig(path string, requiredRoles []string, model Model, effort string) error {
	roles := append([]string(nil), requiredRoles...)
	sort.Strings(roles)
	for index := range roles {
		if strings.TrimSpace(roles[index]) == "" {
			return fmt.Errorf("treatment requires a non-empty dispatch role")
		}
	}
	content := "[approvals]\nmode = \"yolo\"\n\n[dispatch]\nmax_depth = 3\n\n[agents.antigravity]\nenabled = false\n\n"
	if model.Adapter == adapterClaudeCode {
		content += fmt.Sprintf(
			"[agents.claude]\nenabled = true\nmodel = %q\nreasoning_effort = %q\n\n[agents.codex]\nenabled = false\n\n",
			dispatchModel(model),
			effort,
		)
	} else {
		content += fmt.Sprintf(
			"[agents.claude]\nenabled = false\n\n[agents.codex]\nenabled = true\nmodel = %q\nreasoning_effort = %q\n\n",
			dispatchModel(model),
			effort,
		)
	}
	content += "[agents.codex.agent_specific.features]\napps = false\nplugins = false\nbrowser_use = false\nin_app_browser = false\ncomputer_use = false\n\n[agents.claude_vscode]\nenabled = false\n\n[agents.vscode]\nenabled = false\n\n[agents.copilot_cli]\nenabled = false\n\n[notifications]\nchime = false\n\n[mcp]\n"
	if len(roles) > 0 {
		content += "\n# Benchmark-required dispatch roles: " + strings.Join(roles, ", ") + "\n"
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o600)
}

func buildDevelopmentLinuxBinary(repoRoot, root, targetArch string) (string, string, error) {
	if _, err := os.Stat(filepath.Join(repoRoot, ".git")); err != nil {
		return "", "", fmt.Errorf("a released binary must use a checksum-verified matching Linux artifact; development benchmark binaries require a source checkout with .git: %w", err)
	}
	target := filepath.Join(root, ".agent-layer", "bin", "al-linux-"+targetArch)
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return "", "", err
	}
	command := exec.CommandContext(context.Background(), "go", "build", "-trimpath", "-o", target, "./cmd/al") // #nosec G204 -- fixed trusted Go command and source path.
	command.Dir = repoRoot
	command.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH="+targetArch)
	if output, err := command.CombinedOutput(); err != nil {
		return "", "", fmt.Errorf("build matching Linux Agent Layer binary: %w: %s", err, strings.TrimSpace(string(output)))
	}
	data, err := os.ReadFile(target) // #nosec G304 -- target is the binary path constructed in the restricted temporary bundle.
	if err != nil {
		return "", "", err
	}
	hash := sha256.Sum256(data)
	return target, hex.EncodeToString(hash[:]), nil
}

func treatmentManifest(root, mode string, requiredRoles []string) (TreatmentManifest, error) {
	if !validTreatmentMode(mode) {
		return TreatmentManifest{}, fmt.Errorf("unsupported benchmark treatment mode %q", mode)
	}
	files := make([]TreatmentFile, 0)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("treatment contains non-regular file %s", path)
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		clean := filepath.ToSlash(relative)
		if (strings.Contains(clean, ".env") && clean != ".agent-layer/.env") || strings.HasPrefix(clean, ".agent-layer/state/") || strings.HasPrefix(clean, ".agent-layer/tmp/") {
			return fmt.Errorf("forbidden treatment content %s", clean)
		}
		data, err := os.ReadFile(path) // #nosec G122,G304 -- path is a regular file found below the controlled treatment bundle.
		if err != nil {
			return err
		}
		if clean == ".agent-layer/.env" && string(data) != "# Intentionally empty; provider authentication is injected separately.\n" {
			return fmt.Errorf("forbidden treatment content %s", clean)
		}
		hash := sha256.Sum256(data)
		files = append(files, TreatmentFile{Path: clean, SHA256: hex.EncodeToString(hash[:])})
		return nil
	})
	if err != nil {
		return TreatmentManifest{}, err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	roles := append([]string(nil), requiredRoles...)
	sort.Strings(roles)
	timeoutMultiplier := 1.0
	if mode == TreatmentInstructionsAndSkills {
		timeoutMultiplier = skillsAgentTimeoutFactor
	}
	return TreatmentManifest{
		SchemaVersion: TreatmentSchemaVersion, Mode: mode,
		AgentTimeoutMultiplier: timeoutMultiplier,
		Files:                  files, RequiredRoles: roles,
	}, nil
}

// templatesProvenance attributes a treatment bundle to the commit its skills
// and instructions came from, and reports whether that tree has uncommitted
// edits. A dirty tree still produces a content-addressed manifest hash, but the
// run cannot be traced back to anything another person can check out.
// Non-repository roots (hermetic tests) report no provenance rather than
// failing: attribution is meaningful only inside a checkout.
func templatesProvenance(ctx context.Context, repoRoot string) (commit string, dirty bool, err error) {
	templates := filepath.Join("internal", "templates")
	_, statErr := os.Stat(filepath.Join(repoRoot, ".git"))
	if errors.Is(statErr, os.ErrNotExist) {
		return "", false, nil
	}
	if statErr != nil {
		return "", false, fmt.Errorf("inspect benchmark repository root: %w", statErr)
	}
	revision, err := exec.CommandContext(ctx, "git", "-C", repoRoot, "rev-parse", "HEAD").Output() // #nosec G204 -- every argument is a fixed literal; repoRoot is the caller's own repository path.
	if err != nil {
		return "", false, fmt.Errorf("resolve templates commit: %w", err)
	}
	status, err := exec.CommandContext(ctx, "git", "-C", repoRoot, "status", "--porcelain", "--", templates).Output() // #nosec G204 -- every argument is a fixed literal; repoRoot is the caller's own repository path.
	if err != nil {
		return "", false, fmt.Errorf("resolve templates worktree state: %w", err)
	}
	return strings.TrimSpace(string(revision)), len(strings.TrimSpace(string(status))) > 0, nil
}
