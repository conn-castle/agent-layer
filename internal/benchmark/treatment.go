package benchmark

import (
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
	"time"

	"github.com/conn-castle/agent-layer/internal/gitenv"
	"github.com/conn-castle/agent-layer/internal/sync"
	"github.com/conn-castle/agent-layer/internal/update"
	"github.com/conn-castle/agent-layer/internal/version"
)

const treatmentContainerRoot = "/app"

const studyTreatmentPinSchema = "deepswe-study-treatment-pin-v1"

var benchmarkReleaseHTTPClient = &http.Client{Timeout: 30 * time.Second}
var benchmarkReleasesBaseURL = update.ReleasesBaseURL
var renameStudyTreatmentPin = os.Rename

//go:embed assets/pier_agent_layer.py assets/al_dispatch_gate.py assets/pricing.yaml
var treatmentAssets embed.FS

// TreatmentBundle is the secret-free, immutable effective Agent Layer input.
type TreatmentBundle struct {
	Root              string            `json:"root"`
	Manifest          TreatmentManifest `json:"manifest"`
	ManifestHash      string            `json:"manifest_hash"`
	LinuxArchitecture string            `json:"linux_architecture"`
	LinuxBinary       string            `json:"linux_binary"`
	LinuxBinarySHA256 string            `json:"linux_binary_sha256"`
	AdapterPath       string            `json:"adapter_path"`
	AdapterSHA256     string            `json:"adapter_sha256"`
	TemplatesCommit   string            `json:"templates_commit,omitempty"`
	TemplatesDirty    bool              `json:"templates_dirty"`
	// CredentialNames names the *only* host values that may cross the task
	// boundary. Values are never part of a bundle, manifest, or pin.
	CredentialNames   []string `json:"credential_names,omitempty"`
	RuntimeSourceKind string   `json:"runtime_source_kind"`
	RuntimeVersion    string   `json:"runtime_version,omitempty"`
}

// TreatmentManifest names only files that were actually injected. It cannot
// contain .env, project memory, runtime state, temporary data, or credentials.
type TreatmentManifest struct {
	SchemaVersion          string                  `json:"schema_version"`
	Mode                   string                  `json:"mode"`
	AgentTimeoutMultiplier float64                 `json:"agent_timeout_multiplier"`
	Files                  []TreatmentFile         `json:"files"`
	RequiredRoles          []string                `json:"required_dispatch_roles"`
	DispatchConfig         TreatmentDispatchConfig `json:"dispatch_config,omitempty"`
}

// TreatmentFile is the content-addressed declaration for one injected file.
type TreatmentFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// studyTreatmentPin is the immutable pin written by the study runner. It is
// deliberately separate from the predecessor matrix pin format, which is read
// only as historical evidence during recovery.
type studyTreatmentPin struct {
	SchemaVersion     string            `json:"schema_version"`
	PinID             string            `json:"pin_id"`
	Architecture      string            `json:"architecture"`
	ManifestHash      string            `json:"manifest_hash"`
	Manifest          TreatmentManifest `json:"manifest"`
	LinuxBinarySHA256 string            `json:"linux_binary_sha256,omitempty"`
	AdapterSHA256     string            `json:"adapter_sha256"`
	TemplatesCommit   string            `json:"templates_commit,omitempty"`
	TemplatesDirty    bool              `json:"templates_dirty"`
	CredentialNames   []string          `json:"credential_names,omitempty"`
	RuntimeSourceKind string            `json:"runtime_source_kind"`
	RuntimeVersion    string            `json:"runtime_version,omitempty"`
}

// pinStudyTreatmentBundle promotes the complete secret-free effective bundle
// before identity/evidence state is created. The pin is addressed solely by
// its verified manifest content, so it can be reused without trusting a
// mutable staging directory.
func pinStudyTreatmentBundle(repoRoot string, bundle *TreatmentBundle) (*TreatmentBundle, error) {
	if bundle == nil || bundle.ManifestHash == "" {
		return nil, fmt.Errorf("study treatment bundle is missing its manifest")
	}
	if bundle.LinuxArchitecture != benchmarkTaskContainerArchitecture {
		return nil, fmt.Errorf("study treatment bundle has unsupported task architecture %q", bundle.LinuxArchitecture)
	}
	stagedRoot := bundle.Root
	actual, err := treatmentManifest(bundle.Root, bundle.Manifest.Mode, bundle.Manifest.RequiredRoles, bundle.Manifest.DispatchConfig)
	if err != nil {
		return nil, fmt.Errorf("validate complete study bundle: %w", err)
	}
	hash, err := hashCanonical(actual)
	if err != nil || hash != bundle.ManifestHash {
		return nil, fmt.Errorf("study treatment bundle content does not match its manifest")
	}
	root := filepath.Join(repoRoot, ".agent-layer", "state", "benchmarks", "deepswe", "study-pins", bundle.ManifestHash)
	pinPath := filepath.Join(root, "pin.json")
	if _, err := os.Stat(pinPath); err == nil {
		pinned, err := loadPinnedStudyTreatmentBundle(root, bundle)
		if err != nil {
			return nil, err
		}
		_ = os.RemoveAll(stagedRoot)
		return pinned, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect study bundle pin: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(root), 0o700); err != nil {
		return nil, err
	}
	stage, err := os.MkdirTemp(filepath.Dir(root), ".study-pin-")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(stage) }()
	if err := copyRequiredTree(bundle.Root, filepath.Join(stage, "bundle")); err != nil {
		return nil, err
	}
	pin := studyTreatmentPin{SchemaVersion: studyTreatmentPinSchema, PinID: bundle.ManifestHash, ManifestHash: bundle.ManifestHash, Manifest: bundle.Manifest, Architecture: bundle.LinuxArchitecture, AdapterSHA256: bundle.AdapterSHA256, LinuxBinarySHA256: bundle.LinuxBinarySHA256, TemplatesCommit: bundle.TemplatesCommit, TemplatesDirty: bundle.TemplatesDirty, CredentialNames: bundle.CredentialNames, RuntimeSourceKind: bundle.RuntimeSourceKind, RuntimeVersion: bundle.RuntimeVersion}
	if err := writeJSON(filepath.Join(stage, "pin.json"), pin); err != nil {
		return nil, err
	}
	if err := renameStudyTreatmentPin(stage, root); err != nil {
		if _, statErr := os.Stat(pinPath); statErr != nil {
			return nil, fmt.Errorf("publish study bundle pin: %w", err)
		}
	}
	// A failed rename can mean another process won publication. Always reopen
	// and validate the pin it published, rather than validating that directory
	// with this caller's metadata.
	pinned, err := loadPinnedStudyTreatmentBundle(root, bundle)
	if err != nil {
		return nil, err
	}
	_ = os.RemoveAll(stagedRoot)
	return pinned, nil
}

// loadPinnedStudyTreatmentBundle treats the persisted pin as the authority for
// both metadata and content. expected is used only to reject a different
// immutable identity; validation itself is performed from the pin read from
// disk so a concurrent publisher cannot be accepted under caller metadata.
func loadPinnedStudyTreatmentBundle(root string, expected *TreatmentBundle) (*TreatmentBundle, error) {
	var pin studyTreatmentPin
	pinPath := filepath.Join(root, "pin.json")
	if err := readStudyJSON(pinPath, &pin); err != nil {
		return nil, fmt.Errorf("read study bundle pin: %w", err)
	}
	pinned, err := studyTreatmentBundleFromPin(root, pin)
	if err != nil {
		return nil, err
	}
	if !sameStudyTreatmentPinIdentity(pin, expected) {
		return nil, fmt.Errorf("study bundle pin %s conflicts with immutable content", expected.ManifestHash)
	}
	if err := validatePinnedStudyBundle(pinned); err != nil {
		return nil, err
	}
	return pinned, nil
}

func studyTreatmentBundleFromPin(root string, pin studyTreatmentPin) (*TreatmentBundle, error) {
	if pin.SchemaVersion != studyTreatmentPinSchema || pin.PinID == "" || pin.PinID != pin.ManifestHash ||
		pin.Architecture != benchmarkTaskContainerArchitecture || pin.ManifestHash == "" || pin.AdapterSHA256 == "" {
		return nil, fmt.Errorf("study bundle pin %s conflicts with immutable content: corrupt immutable metadata", pin.ManifestHash)
	}
	manifestHash, err := hashCanonical(pin.Manifest)
	if err != nil || manifestHash != pin.ManifestHash {
		return nil, fmt.Errorf("study bundle pin %s has a corrupt immutable manifest", pin.ManifestHash)
	}
	bundleRoot := filepath.Join(root, "bundle")
	bundle := &TreatmentBundle{
		Root:              bundleRoot,
		Manifest:          pin.Manifest,
		ManifestHash:      pin.ManifestHash,
		LinuxArchitecture: pin.Architecture,
		AdapterPath:       filepath.Join(bundleRoot, "adapter", "pier_agent_layer.py"),
		AdapterSHA256:     pin.AdapterSHA256,
		TemplatesCommit:   pin.TemplatesCommit,
		TemplatesDirty:    pin.TemplatesDirty,
		CredentialNames:   append([]string(nil), pin.CredentialNames...),
		RuntimeSourceKind: pin.RuntimeSourceKind,
		RuntimeVersion:    pin.RuntimeVersion,
		LinuxBinarySHA256: pin.LinuxBinarySHA256,
	}
	if bundle.LinuxBinarySHA256 != "" {
		bundle.LinuxBinary = filepath.Join(bundleRoot, ".agent-layer", "bin", "al-linux-"+bundle.LinuxArchitecture)
	}
	return bundle, nil
}

func sameStudyTreatmentPinIdentity(pin studyTreatmentPin, expected *TreatmentBundle) bool {
	if expected == nil {
		return false
	}
	expectedManifestHash, err := hashCanonical(expected.Manifest)
	return err == nil && expectedManifestHash == expected.ManifestHash &&
		pin.PinID == expected.ManifestHash && pin.Architecture == expected.LinuxArchitecture &&
		pin.ManifestHash == expected.ManifestHash && pin.AdapterSHA256 == expected.AdapterSHA256 &&
		pin.LinuxBinarySHA256 == expected.LinuxBinarySHA256 && pin.TemplatesCommit == expected.TemplatesCommit &&
		pin.TemplatesDirty == expected.TemplatesDirty && pin.RuntimeSourceKind == expected.RuntimeSourceKind &&
		pin.RuntimeVersion == expected.RuntimeVersion &&
		strings.Join(pin.CredentialNames, "\x00") == strings.Join(expected.CredentialNames, "\x00")
}

// validatePinnedStudyBundle rehashes every declared byte on every reuse. The
// manifest enumerates the complete bundle, so added files are rejected too.
func validatePinnedStudyBundle(bundle *TreatmentBundle) error {
	manifest, err := treatmentManifest(bundle.Root, bundle.Manifest.Mode, bundle.Manifest.RequiredRoles, bundle.Manifest.DispatchConfig)
	if err != nil {
		return fmt.Errorf("rehash pinned study bundle: %w", err)
	}
	hash, err := hashCanonical(manifest)
	if err != nil || hash != bundle.ManifestHash || !sameTreatmentFiles(manifest.Files, bundle.Manifest.Files) {
		return fmt.Errorf("pinned study bundle content differs from immutable manifest")
	}
	for _, item := range []struct{ path, want, label string }{{bundle.AdapterPath, bundle.AdapterSHA256, "adapter"}, {bundle.LinuxBinary, bundle.LinuxBinarySHA256, "runtime"}} {
		if item.want == "" {
			continue
		}
		data, readErr := os.ReadFile(item.path)
		if readErr != nil {
			return fmt.Errorf("read pinned %s: %w", item.label, readErr)
		}
		actual := sha256.Sum256(data)
		if !strings.EqualFold(item.want, hex.EncodeToString(actual[:])) {
			return fmt.Errorf("pinned %s bytes differ from immutable manifest", item.label)
		}
	}
	return nil
}

func sameTreatmentFiles(left, right []TreatmentFile) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

// BuildStudyTreatmentBundle stages the immutable input snapshot prepared for
// an experiment. A distributed study is reproducible from those declared bytes
// and the runner assets embedded in this executable alone. Its runtime target
// is the certified task container, not the machine preparing the study.
func BuildStudyTreatmentBundle(repoRoot string, experiment preparedStudyExperiment) (*TreatmentBundle, error) {
	targetArch := benchmarkTaskContainerArchitecture
	mode := TreatmentInstructionsOnly
	if experiment.Skills != "" {
		mode = TreatmentInstructionsAndSkills
	}
	stageParent := filepath.Join(repoRoot, ".agent-layer", "tmp")
	if err := os.MkdirAll(stageParent, 0o700); err != nil {
		return nil, err
	}
	root, err := os.MkdirTemp(stageParent, "benchmark-study-treatment-")
	if err != nil {
		return nil, fmt.Errorf("create study treatment staging: %w", err)
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
	if experiment.Config == "" {
		return nil, fmt.Errorf("study Agent Layer experiment requires config")
	}
	config := experiment.inputs.Config
	if config == "" {
		return nil, fmt.Errorf("study input snapshot is missing config")
	}
	configBytes, err := os.ReadFile(config) // #nosec G304 -- explicit study input validated before staging.
	if err != nil {
		return nil, fmt.Errorf("read study config: %w", err)
	}
	mcpContract, credentialNames, err := studyMCPContract(configBytes, config)
	if err != nil {
		return nil, fmt.Errorf("validate secret-free effective config: %w", err)
	}
	if err := copyRequiredFile(config, filepath.Join(layer, "config.toml")); err != nil {
		return nil, err
	}
	for _, input := range []struct{ name, path string }{{studyInputInstructions, experiment.inputs.Instructions}, {studyInputSkills, experiment.inputs.Skills}} {
		if input.path == "" {
			if err := os.MkdirAll(filepath.Join(layer, input.name), 0o700); err != nil {
				return nil, err
			}
			continue
		}
		if err := copyRequiredTree(input.path, filepath.Join(layer, input.name)); err != nil {
			return nil, err
		}
	}
	if mode == TreatmentInstructionsAndSkills {
		if experiment.EntryPrompt == "" {
			return nil, fmt.Errorf("study skills experiment requires entry_prompt")
		}
		if experiment.inputs.EntryPrompt == "" {
			return nil, fmt.Errorf("study input snapshot is missing entry_prompt")
		}
		prompt, err := readRegularNonempty(experiment.inputs.EntryPrompt)
		if err != nil {
			return nil, fmt.Errorf("read study entry_prompt: %w", err)
		}
		if bytes.Count(prompt, []byte("{{task}}")) != 1 {
			return nil, fmt.Errorf("study entry_prompt must contain exactly one literal {{task}} placeholder")
		}
		if err := os.WriteFile(filepath.Join(root, "workflow-prompt.md"), prompt, 0o600); err != nil {
			return nil, err
		}
	}
	// Sync needs syntactically resolved values to render normal client
	// projections. Use disposable sentinel values, then restore the canonical
	// empty marker before the bundle is hashed or uploaded.
	placeholderEnv := ""
	for _, name := range credentialNames {
		placeholderEnv += name + "=benchmark-placeholder\n"
	}
	if err := os.WriteFile(filepath.Join(layer, ".env"), []byte(placeholderEnv), 0o600); err != nil { // #nosec G703 -- layer is the controlled private staging root.
		return nil, err
	}
	// These are normal Agent Layer source inputs, deliberately generated empty
	// for a study rather than copied from the runner's source checkout.
	for _, name := range []string{"commands.allow", "gitignore.block"} {
		if err := os.WriteFile(filepath.Join(layer, name), nil, 0o600); err != nil {
			return nil, err
		}
	}
	if _, err := sync.Run(root); err != nil {
		return nil, fmt.Errorf("synchronize staged study inputs: %w", err)
	}
	if err := os.WriteFile(filepath.Join(layer, ".env"), []byte("# Intentionally empty; provider authentication is injected separately.\n"), 0o600); err != nil {
		return nil, err
	}
	if err := writeStudyMCPPreflight(filepath.Join(root, "mcp-preflight.json"), mcpContract); err != nil {
		return nil, fmt.Errorf("write MCP preflight contract: %w", err)
	}
	if err := rewriteTreatmentProjectionRoot(root, root, treatmentContainerRoot); err != nil {
		return nil, err
	}
	dispatch := defaultTreatmentDispatchConfig(experiment.model, experiment.effort)
	roles := append([]string(nil), experiment.RequiredDispatchRoles...)
	if mode != TreatmentInstructionsAndSkills {
		dispatch = TreatmentDispatchConfig{}
		roles = nil
	} else {
		data, marshalErr := json.Marshal(dispatch)
		if marshalErr != nil {
			return nil, fmt.Errorf("encode study dispatch targets: %w", marshalErr)
		}
		if err := os.WriteFile(filepath.Join(root, "dispatch-targets.json"), append(data, '\n'), 0o600); err != nil {
			return nil, err
		}
		if _, err := expectedDispatchSlots(roles, dispatch); err != nil {
			return nil, err
		}
	}
	adapter, err := treatmentAssets.ReadFile("assets/pier_agent_layer.py")
	if err != nil {
		return nil, err
	}
	adapterPath := filepath.Join(root, "adapter", "pier_agent_layer.py")
	if err := os.MkdirAll(filepath.Dir(adapterPath), 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(adapterPath, adapter, 0o600); err != nil {
		return nil, err
	}
	if mode == TreatmentInstructionsAndSkills {
		gate, err := treatmentAssets.ReadFile("assets/al_dispatch_gate.py")
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(filepath.Join(root, "adapter", "al_dispatch_gate.py"), gate, 0o600); err != nil {
			return nil, err
		}
	}
	// Every Agent Layer experiment needs the exact matching runtime: config-only
	// and instructions-only projections can contain normal MCP entries even when
	// they do not activate dispatch skills.
	binary, binaryHash := "", ""
	binary, binaryHash, err = buildDevelopmentLinuxBinary(repoRoot, root, targetArch)
	if err != nil {
		return nil, err
	}
	// Hash only after every effective adapter/runtime byte has been staged.
	// This is the content that is later atomically pinned and identified.
	manifest, err := treatmentManifest(root, mode, roles, dispatch)
	if err != nil {
		return nil, err
	}
	manifestHash, err := hashCanonical(manifest)
	if err != nil {
		return nil, err
	}
	adapterHash := sha256.Sum256(adapter)
	commit, dirty, err := verifiedDevelopmentProvenance()
	if err != nil {
		return nil, err
	}
	sourceKind, runtimeVersion := "development", ""
	if commit == "" {
		sourceKind = "release"
		if executable, versionErr := os.Executable(); versionErr == nil {
			if output, outputErr := exec.CommandContext(context.Background(), executable, "--version").Output(); outputErr == nil { // #nosec G204 -- executable is the current Agent Layer binary.
				if fields := strings.Fields(string(output)); len(fields) > 0 {
					runtimeVersion = strings.TrimPrefix(fields[0], "v")
				}
			}
		}
	}
	cleanup = false
	return &TreatmentBundle{Root: root, Manifest: manifest, ManifestHash: manifestHash, LinuxArchitecture: targetArch, LinuxBinary: binary, LinuxBinarySHA256: binaryHash, AdapterPath: adapterPath, AdapterSHA256: hex.EncodeToString(adapterHash[:]), TemplatesCommit: commit, TemplatesDirty: dirty, CredentialNames: credentialNames, RuntimeSourceKind: sourceKind, RuntimeVersion: runtimeVersion}, nil
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
	data, err := os.ReadFile(source) // #nosec G304 -- source is an explicit study input or verified evidence path.
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

func buildDevelopmentLinuxBinary(_ string, root, targetArch string) (string, string, error) {
	sourceRoot, commit, dirty, development, err := verifiedDevelopmentSourceCheckout()
	if err != nil {
		return "", "", err
	}
	if !development {
		return downloadReleasedLinuxBinary(root, targetArch)
	}
	target := filepath.Join(root, ".agent-layer", "bin", "al-linux-"+targetArch)
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return "", "", err
	}
	command := exec.CommandContext(context.Background(), "go", "build", "-trimpath", "-o", target, "./cmd/al") // #nosec G204 -- fixed trusted Go command and source path.
	command.Dir = sourceRoot
	command.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH="+targetArch)
	if output, err := command.CombinedOutput(); err != nil {
		return "", "", fmt.Errorf("build matching Linux Agent Layer binary: %w: %s", err, strings.TrimSpace(string(output)))
	}
	data, err := os.ReadFile(target) // #nosec G304 -- target is the binary path constructed in the restricted temporary bundle.
	if err != nil {
		return "", "", err
	}
	hash := sha256.Sum256(data)
	_ = commit
	_ = dirty
	return target, hex.EncodeToString(hash[:]), nil
}

// verifiedDevelopmentSourceCheckout refuses to treat a user project as Agent
// Layer merely because it happens to have .git. A development binary is tied
// to this package's absolute source path and, when available, its build VCS
// revision must match the checkout exactly. Trimpath/released binaries have no
// absolute source path and therefore always take the verified release path.
func verifiedDevelopmentSourceCheckout() (root, commit string, dirty bool, development bool, err error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok || !filepath.IsAbs(file) {
		return "", "", false, false, nil
	}
	root = filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	goMod, readErr := os.ReadFile(filepath.Join(root, "go.mod"))
	if readErr != nil {
		if !errors.Is(readErr, os.ErrNotExist) {
			return "", "", false, false, fmt.Errorf("read development source module: %w", readErr)
		}
		return "", "", false, false, nil
	}
	if !strings.Contains(string(goMod), "module github.com/conn-castle/agent-layer") {
		return "", "", false, false, nil
	}
	if _, statErr := os.Stat(filepath.Join(root, ".git")); statErr != nil {
		if errors.Is(statErr, os.ErrNotExist) {
			return "", "", false, false, nil
		}
		return "", "", false, false, fmt.Errorf("inspect development source checkout: %w", statErr)
	}
	revision := exec.CommandContext(context.Background(), "git", "-C", root, "rev-parse", "HEAD") // #nosec G204 -- fixed command in verified source root.
	revision.Env = gitenv.WithoutDiscovery()
	data, commandErr := revision.Output()
	if commandErr != nil {
		return "", "", false, false, fmt.Errorf("resolve development source revision: %w", commandErr)
	}
	commit = strings.TrimSpace(string(data))
	if info, ok := debug.ReadBuildInfo(); ok {
		if info.Main.Version != "" && info.Main.Version != "(devel)" {
			// A released/distributed binary must never silently rebuild from a
			// coincidentally available source tree.
			return "", "", false, false, nil
		}
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" && setting.Value != "" && !strings.HasPrefix(commit, setting.Value) {
				return "", "", false, false, fmt.Errorf("development binary revision %s does not match source checkout %s", setting.Value, commit)
			}
			if setting.Key == "vcs.modified" && setting.Value == "true" {
				dirty = true
			}
		}
	}
	status := exec.CommandContext(context.Background(), "git", "-C", root, "status", "--porcelain", "--", "internal", "cmd", "go.mod", "go.sum") // #nosec G204 -- fixed command in verified source root.
	status.Env = gitenv.WithoutDiscovery()
	if data, statusErr := status.Output(); statusErr != nil {
		return "", "", false, false, fmt.Errorf("inspect development source state: %w", statusErr)
	} else if len(bytes.TrimSpace(data)) > 0 {
		dirty = true
	}
	return root, commit, dirty, true, nil
}

func verifiedDevelopmentProvenance() (string, bool, error) {
	_, commit, dirty, development, err := verifiedDevelopmentSourceCheckout()
	if err != nil || !development {
		return "", false, err
	}
	return commit, dirty, nil
}

// downloadReleasedLinuxBinary mirrors the installer security contract. A
// checkout is the only development path; without one, the running released
// executable must name an exact semver release and its Linux artifact must
// match the release checksums file. There is intentionally no fallback.
func downloadReleasedLinuxBinary(root, targetArch string) (string, string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", "", fmt.Errorf("locate released Agent Layer executable: %w", err)
	}
	output, err := exec.CommandContext(context.Background(), executable, "--version").Output() // #nosec G204 -- current executable only.
	if err != nil {
		return "", "", fmt.Errorf("read released Agent Layer version: %w", err)
	}
	fields := strings.Fields(string(output))
	if len(fields) == 0 {
		return "", "", fmt.Errorf("released Agent Layer executable did not report a version")
	}
	release, err := version.Normalize(strings.TrimPrefix(fields[0], "v"))
	if err != nil {
		return "", "", fmt.Errorf("released benchmark binary requires an exact release version: %w", err)
	}
	asset := "al-linux-" + targetArch
	base := strings.TrimRight(benchmarkReleasesBaseURL, "/") + "/download/v" + release
	sums, err := downloadReleaseAsset(base + "/checksums.txt")
	if err != nil {
		return "", "", fmt.Errorf("download released Agent Layer checksums: %w", err)
	}
	expected, err := checksumForReleaseAsset(sums, asset)
	if err != nil {
		return "", "", err
	}
	binary, err := downloadReleaseAsset(base + "/" + asset)
	if err != nil {
		return "", "", fmt.Errorf("download released Linux Agent Layer runtime: %w", err)
	}
	actual := sha256.Sum256(binary)
	actualHex := hex.EncodeToString(actual[:])
	if !strings.EqualFold(expected, actualHex) {
		return "", "", fmt.Errorf("released Linux Agent Layer runtime checksum mismatch for %s", asset)
	}
	target := filepath.Join(root, ".agent-layer", "bin", asset)
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return "", "", err
	}
	if err := os.WriteFile(target, binary, 0o700); err != nil { // #nosec G306 -- the downloaded runtime must be executable and is written into the private bundle.
		return "", "", fmt.Errorf("write released Linux Agent Layer runtime: %w", err)
	}
	return target, actualHex, nil
}

func downloadReleaseAsset(url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	response, err := benchmarkReleaseHTTPClient.Do(req) //nolint:gosec // base is the configured release endpoint.
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected HTTP status %s", response.Status)
	}
	return io.ReadAll(io.LimitReader(response.Body, 100<<20))
}

func checksumForReleaseAsset(sums []byte, asset string) (string, error) {
	for _, line := range strings.Split(string(sums), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name := strings.TrimPrefix(strings.TrimPrefix(fields[len(fields)-1], "./"), "*")
		if name != asset {
			continue
		}
		if len(fields[0]) != 64 {
			return "", fmt.Errorf("released checksum for %s is invalid", asset)
		}
		if _, err := hex.DecodeString(fields[0]); err != nil {
			return "", fmt.Errorf("released checksum for %s is invalid", asset)
		}
		return fields[0], nil
	}
	return "", fmt.Errorf("released checksums do not contain %s", asset)
}

func treatmentManifest(root, mode string, requiredRoles []string, dispatchConfig ...TreatmentDispatchConfig) (TreatmentManifest, error) {
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
		if (strings.Contains(clean, ".env") && clean != agentLayerEnvPath) || strings.HasPrefix(clean, ".agent-layer/state/") || strings.HasPrefix(clean, ".agent-layer/tmp/") {
			return fmt.Errorf("forbidden treatment content %s", clean)
		}
		data, err := os.ReadFile(path) // #nosec G122,G304 -- path is a regular file found below the controlled treatment bundle.
		if err != nil {
			return err
		}
		if clean == agentLayerEnvPath && string(data) != "# Intentionally empty; provider authentication is injected separately.\n" {
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
	manifest := TreatmentManifest{
		SchemaVersion: TreatmentSchemaVersion, Mode: mode,
		// This is a study-wide resource contract, not a treatment advantage.
		// Bare, instructions-only, skills-only, and full experiments all receive
		// the same sufficient multi-agent allowance.
		AgentTimeoutMultiplier: skillsAgentTimeoutFactor,
		Files:                  files, RequiredRoles: roles,
	}
	if len(dispatchConfig) > 0 {
		manifest.DispatchConfig = dispatchConfig[0]
	}
	return manifest, nil
}
