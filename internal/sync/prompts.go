package sync

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/skilltree"
)

const (
	generatedMarkerHeader     = "GENERATED FILE"
	generatedMarkerSource     = "Source: .agent-layer/"
	generatedMarkerRegenerate = "Regenerate: al sync"
)

const (
	// canonicalSkillManifestName is the manifest filename every projected skill
	// directory uses, whichever spelling the source happens to use.
	canonicalSkillManifestName = "SKILL.md"
	// fallbackSkillManifestName is the legacy lowercase spelling internal/config
	// still accepts in a user-managed source.
	fallbackSkillManifestName = "skill.md"
)

const (
	// skillResourceRegularMode is the projected mode for a non-executable skill resource.
	skillResourceRegularMode os.FileMode = 0o644
	// skillResourceExecutableMode is the projected mode for an executable skill resource.
	skillResourceExecutableMode os.FileMode = 0o755
)

// ownedSkillsManifestName records which projected skill directories Agent Layer
// created, so a later sync can remove the ones that left the source set without
// touching a skill directory the user or the client put there.
//
// It lives beside the projected skills rather than inside any SKILL.md: a
// projected skill is an exact copy of its source, so there is nowhere in its
// content to record ownership. The leading dot keeps it out of the way of skill
// loaders, which look for `<dir>/SKILL.md`.
const ownedSkillsManifestName = ".agent-layer-skills.json"

// ownedSkillsManifestVersion is the only manifest schema this release writes.
const ownedSkillsManifestVersion = 2

// ownedSkillsManifest is the on-disk ownership record for one projected skills
// directory.
type ownedSkillsManifest struct {
	Version int               `json:"version"`
	Skills  []string          `json:"skills"`
	Hashes  map[string]string `json:"hashes,omitempty"`
}

// WriteAgentSkills projects the loaded skills into .agents/skills/.
// sys performs filesystem operations, root is the projection root, and skills
// supplies the canonical loaded skills. It returns the first reconciliation error.
func WriteAgentSkills(sys System, root string, skills []config.Skill) error {
	return writeSkillProjection(sys, filepath.Join(root, ".agents", "skills"), skills)
}

// WriteClaudeSkills projects the loaded skills into .claude/skills/.
// sys performs filesystem operations, root is the projection root, and skills
// supplies the canonical loaded skills. It returns the first reconciliation error.
func WriteClaudeSkills(sys System, root string, skills []config.Skill) error {
	return writeSkillProjection(sys, filepath.Join(root, ".claude", "skills"), skills)
}

// writeSkillProjection materializes every skill as an exact copy of its source
// tree and then removes the previously projected skills that are no longer
// wanted.
//
// The copy is byte-for-byte, including SKILL.md and the executable bit, so what
// an agent reads is exactly what the skill author wrote. Nothing is injected
// into the projected content, which is also why ownership is tracked in a
// sibling manifest instead of a marker inside SKILL.md. User-managed and
// imported sources go through this one path, so the two tiers cannot drift.
func writeSkillProjection(sys System, skillsDir string, skills []config.Skill) error {
	if err := sys.MkdirAll(skillsDir, 0o755); err != nil {
		return fmt.Errorf(messages.SyncCreateDirFailedFmt, skillsDir, err)
	}
	stagingRoot := filepath.Join(skillsDir, ".agent-layer-projection-staging")
	if err := recoverSkillProjection(sys, skillsDir, stagingRoot); err != nil {
		return err
	}

	previouslyOwned, err := loadOwnedSkills(sys, skillsDir)
	if err != nil {
		return err
	}

	owned := make(map[string]string, len(skills))
	type stagedSkill struct {
		name, target, staged, backup string
		targetExisted                bool
	}
	var stagedSkills []stagedSkill
	if err := sys.MkdirAll(filepath.Join(stagingRoot, "staged"), 0o755); err != nil {
		return fmt.Errorf(messages.SyncCreateDirFailedFmt, stagingRoot, err)
	}
	if err := sys.MkdirAll(filepath.Join(stagingRoot, "backup"), 0o755); err != nil {
		return fmt.Errorf(messages.SyncCreateDirFailedFmt, stagingRoot, err)
	}
	if err := sys.MkdirAll(filepath.Join(stagingRoot, "created"), 0o755); err != nil {
		return fmt.Errorf(messages.SyncCreateDirFailedFmt, stagingRoot, err)
	}
	for _, skill := range skills {
		if strings.Contains(skill.Name, "..") || strings.ContainsAny(skill.Name, `/\`) {
			return fmt.Errorf("invalid skill name %q: must not contain path separators or '..'", skill.Name)
		}
		if strings.TrimSpace(skill.SourceDir) == "" {
			return fmt.Errorf("skill %q has no source directory to project", skill.Name)
		}
		desired, err := desiredSkillResources(sys, skill.SourceDir)
		if err != nil {
			return err
		}
		desiredHash := desiredSkillHash(desired)
		target := filepath.Join(skillsDir, skill.Name)
		_, statErr := sys.Lstat(target)
		targetExists := statErr == nil
		if statErr != nil && !os.IsNotExist(statErr) {
			return fmt.Errorf(messages.SyncReadFailedFmt, target, statErr)
		}
		previousHash, wasOwned := previouslyOwned[skill.Name]
		if targetExists && !wasOwned {
			return fmt.Errorf("refusing to overwrite unowned client skill directory %s; move or remove it, then run 'al sync' again", target)
		}
		if targetExists && wasOwned && previousHash != "" {
			currentHash, hashErr := hashProjectedSkill(sys, target)
			if hashErr != nil {
				return hashErr
			}
			if currentHash != previousHash && currentHash != desiredHash {
				return fmt.Errorf("projected skill %s changed outside Agent Layer; preserve or remove those edits before syncing", target)
			}
		}
		staged := filepath.Join(stagingRoot, "staged", skill.Name)
		if err := materializeDesiredSkill(sys, staged, desired); err != nil {
			return err
		}
		if !targetExists {
			marker := filepath.Join(stagingRoot, "created", skill.Name)
			if err := sys.WriteFileAtomic(marker, nil, skillResourceRegularMode); err != nil {
				return fmt.Errorf(messages.SyncWriteFileFailedFmt, marker, err)
			}
		}
		stagedSkills = append(stagedSkills, stagedSkill{name: skill.Name, target: target, staged: staged, backup: filepath.Join(stagingRoot, "backup", skill.Name), targetExisted: targetExists})
		owned[skill.Name] = desiredHash
	}

	for name, previousHash := range previouslyOwned {
		if _, keep := owned[name]; keep {
			continue
		}
		target := filepath.Join(skillsDir, name)
		info, statErr := sys.Lstat(target)
		if os.IsNotExist(statErr) {
			continue
		}
		if statErr != nil {
			return fmt.Errorf(messages.SyncReadFailedFmt, target, statErr)
		}
		if previousHash != "" {
			currentHash, hashErr := hashProjectedSkill(sys, target)
			if hashErr != nil {
				return hashErr
			}
			if currentHash != previousHash {
				return fmt.Errorf("stale projected skill %s has local client-side changes; preserve or remove them before syncing", target)
			}
		}
		stagedSkills = append(stagedSkills, stagedSkill{name: name, target: target, backup: filepath.Join(stagingRoot, "backup", name), targetExisted: info != nil})
	}
	sort.Slice(stagedSkills, func(i, j int) bool { return stagedSkills[i].name < stagedSkills[j].name })
	applied := make([]stagedSkill, 0, len(stagedSkills))
	rollback := func() {
		for i := len(applied) - 1; i >= 0; i-- {
			record := applied[i]
			_ = sys.RemoveAll(record.target)
			if record.targetExisted {
				_ = sys.Rename(record.backup, record.target)
			}
		}
	}
	for _, record := range stagedSkills {
		if record.targetExisted {
			if err := sys.Rename(record.target, record.backup); err != nil {
				rollback()
				return fmt.Errorf("move projected skill %s aside: %w", record.target, err)
			}
		}
		applied = append(applied, record)
		if record.staged != "" {
			if err := sys.Rename(record.staged, record.target); err != nil {
				rollback()
				return fmt.Errorf("publish projected skill %s: %w", record.target, err)
			}
		}
	}
	if err := writeOwnedSkills(sys, skillsDir, owned); err != nil {
		rollback()
		return err
	}
	if err := sys.RemoveAll(stagingRoot); err != nil {
		return fmt.Errorf(messages.SyncRemoveFailedFmt, stagingRoot, err)
	}
	return nil
}

// copySkillTree materializes one skill's complete source tree at destDir.
func copySkillTree(sys System, skill config.Skill, destDir string) error {
	destInfo, err := sys.Lstat(destDir)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf(messages.SyncReadFailedFmt, destDir, err)
	}
	if err == nil && destInfo.Mode()&os.ModeSymlink != 0 {
		// A symlink where the skill directory belongs would send every write
		// outside the projection root.
		if err := removeSkillResourceNode(sys, destDir, destInfo); err != nil {
			return err
		}
	}
	if err := sys.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf(messages.SyncCreateDirFailedFmt, destDir, err)
	}
	return copyDirRecursive(sys, skill.SourceDir, destDir)
}

func desiredSkillResources(sys System, srcDir string) (map[string]desiredSkillResource, error) {
	desired := make(map[string]desiredSkillResource)
	if err := collectDesiredSkillResources(sys, srcDir, "", desired); err != nil {
		return nil, err
	}
	return desired, nil
}

func materializeDesiredSkill(sys System, destDir string, desired map[string]desiredSkillResource) error {
	if err := sys.RemoveAll(destDir); err != nil {
		return fmt.Errorf(messages.SyncRemoveFailedFmt, destDir, err)
	}
	if err := sys.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf(messages.SyncCreateDirFailedFmt, destDir, err)
	}
	paths := make([]string, 0, len(desired))
	for relativePath := range desired {
		paths = append(paths, relativePath)
	}
	sort.Strings(paths)
	for _, relativePath := range paths {
		resource := desired[relativePath]
		destPath := filepath.Join(destDir, relativePath)
		if resource.isDir {
			if err := sys.MkdirAll(destPath, 0o755); err != nil {
				return fmt.Errorf(messages.SyncCreateDirFailedFmt, destPath, err)
			}
			continue
		}
		if err := sys.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
			return fmt.Errorf(messages.SyncCreateDirFailedFmt, filepath.Dir(destPath), err)
		}
		if err := sys.WriteFileAtomic(destPath, resource.data, resource.mode); err != nil {
			return fmt.Errorf(messages.SyncWriteFileFailedFmt, destPath, err)
		}
	}
	return nil
}

func desiredSkillHash(desired map[string]desiredSkillResource) string {
	files := make([]skilltree.File, 0, len(desired))
	for relativePath, resource := range desired {
		if resource.isDir {
			continue
		}
		files = append(files, skilltree.File{Path: filepath.ToSlash(relativePath), Data: resource.data, Executable: resource.mode&0o100 != 0})
	}
	return skilltree.Hash(files)
}

func hashProjectedSkill(sys System, dir string) (string, error) {
	desired, err := desiredSkillResources(sys, dir)
	if err != nil {
		return "", err
	}
	return desiredSkillHash(desired), nil
}

func recoverSkillProjection(sys System, skillsDir string, stagingRoot string) error {
	if info, err := sys.Lstat(stagingRoot); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("projection staging path %s is not a real directory; remove it before syncing", stagingRoot)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf(messages.SyncReadFailedFmt, stagingRoot, err)
	}
	backupDir := filepath.Join(stagingRoot, "backup")
	entries, err := sys.ReadDir(backupDir)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf(messages.SyncReadFailedFmt, backupDir, err)
	}
	createdDir := filepath.Join(stagingRoot, "created")
	created, createdErr := sys.ReadDir(createdDir)
	if createdErr != nil && !os.IsNotExist(createdErr) {
		return fmt.Errorf(messages.SyncReadFailedFmt, createdDir, createdErr)
	}
	if createdErr == nil {
		for _, entry := range created {
			if !config.IsSafeSkillImportName(entry.Name()) || entry.IsDir() {
				return fmt.Errorf("projection recovery found unsafe creation marker %q in %s", entry.Name(), createdDir)
			}
			target := filepath.Join(skillsDir, entry.Name())
			if err := sys.RemoveAll(target); err != nil {
				return fmt.Errorf(messages.SyncRemoveFailedFmt, target, err)
			}
		}
	}
	if err == nil {
		for _, entry := range entries {
			if !config.IsSafeSkillImportName(entry.Name()) {
				return fmt.Errorf("projection recovery found unsafe backup name %q in %s", entry.Name(), backupDir)
			}
			backup := filepath.Join(backupDir, entry.Name())
			target := filepath.Join(skillsDir, entry.Name())
			if _, targetErr := sys.Lstat(target); targetErr == nil {
				if removeErr := sys.RemoveAll(backup); removeErr != nil {
					return fmt.Errorf(messages.SyncRemoveFailedFmt, backup, removeErr)
				}
			} else if os.IsNotExist(targetErr) {
				if renameErr := sys.Rename(backup, target); renameErr != nil {
					return fmt.Errorf("restore interrupted projected skill %s: %w", target, renameErr)
				}
			} else {
				return fmt.Errorf(messages.SyncReadFailedFmt, target, targetErr)
			}
		}
	}
	if err := sys.RemoveAll(stagingRoot); err != nil {
		return fmt.Errorf(messages.SyncRemoveFailedFmt, stagingRoot, err)
	}
	return nil
}

type desiredSkillResource struct {
	isDir bool
	data  []byte
	mode  os.FileMode
}

// copyDirRecursive reconciles the contents of srcDir into destDir: it
// materializes every source entry under destDir (handling file/directory type
// transitions) and deletes any destination entry that is absent from the source.
func copyDirRecursive(sys System, srcDir string, destDir string) error {
	desired := make(map[string]desiredSkillResource)
	if srcDir != "" {
		if err := collectDesiredSkillResources(sys, srcDir, "", desired); err != nil {
			return err
		}
	}

	paths := make([]string, 0, len(desired))
	for path := range desired {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, relativePath := range paths {
		resource := desired[relativePath]
		destPath := filepath.Join(destDir, relativePath)
		destInfo, err := sys.Lstat(destPath)
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf(messages.SyncReadFailedFmt, destPath, err)
		}
		if err == nil && (destInfo.IsDir() != resource.isDir || destInfo.Mode()&os.ModeSymlink != 0) {
			if err := removeSkillResourceNode(sys, destPath, destInfo); err != nil {
				return err
			}
		}

		if resource.isDir {
			if err := sys.MkdirAll(destPath, 0o755); err != nil {
				return fmt.Errorf(messages.SyncCreateDirFailedFmt, destPath, err)
			}
			continue
		}
		if err := sys.WriteFileAtomic(destPath, resource.data, resource.mode); err != nil {
			return fmt.Errorf(messages.SyncWriteFileFailedFmt, destPath, err)
		}
	}

	return removeStaleSkillResources(sys, destDir, "", desired)
}

// collectDesiredSkillResources recursively walks srcDir and records every desired
// resource in desired, keyed by path relative to the original source root:
// directories as markers and files with their exact bytes.
//
// The file set is exhaustive and identical for user-managed and imported skill
// sources: hidden files and dotfiles are part of a skill and are projected. Only
// config.IsIgnoredSkillResourceName entries are excluded, plus symlinks, which
// are never dereferenced. A legacy lowercase skill.md at the skill root is
// projected under the canonical name with its bytes untouched, so clients that
// only look for SKILL.md still find it. A missing srcDir contributes no entries;
// any other read error is returned wrapped with SyncReadFailedFmt.
func collectDesiredSkillResources(
	sys System,
	srcDir string,
	relativeDir string,
	desired map[string]desiredSkillResource,
) error {
	entries, err := sys.ReadDir(srcDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf(messages.SyncReadFailedFmt, srcDir, err)
	}

	for _, entry := range entries {
		name := entry.Name()
		if config.IsIgnoredSkillResourceName(name) {
			continue
		}

		srcPath := filepath.Join(srcDir, name)
		if relativeDir == "" && name == fallbackSkillManifestName {
			name = canonicalSkillManifestName
		}
		relativePath := filepath.Join(relativeDir, name)
		srcInfo, err := sys.Lstat(srcPath)
		if err != nil {
			return fmt.Errorf(messages.SyncReadFailedFmt, srcPath, err)
		}
		if srcInfo.Mode()&os.ModeSymlink != 0 {
			continue
		}

		if srcInfo.IsDir() {
			desired[relativePath] = desiredSkillResource{isDir: true}
			if err := collectDesiredSkillResources(sys, srcPath, relativePath, desired); err != nil {
				return err
			}
			continue
		}

		data, err := sys.ReadFile(srcPath)
		if err != nil {
			return fmt.Errorf(messages.SyncReadFailedFmt, srcPath, err)
		}
		desired[relativePath] = desiredSkillResource{data: data, mode: projectedResourceMode(srcInfo.Mode())}
	}
	return nil
}

// projectedResourceMode maps a source file mode onto the two modes a skill
// resource can have. The executable bit is content, so it is preserved; the
// remaining permission bits are normalized so a projected skill does not inherit
// an incidental umask or a restrictive source permission that would stop an
// agent from reading it.
func projectedResourceMode(mode os.FileMode) os.FileMode {
	if mode.Perm()&0o100 != 0 {
		return skillResourceExecutableMode
	}
	return skillResourceRegularMode
}

// removeStaleSkillResources recursively walks the destDir subtree rooted at
// relativeDir and removes every entry not present in desired, recursing into
// directories that are themselves desired.
func removeStaleSkillResources(
	sys System,
	destDir string,
	relativeDir string,
	desired map[string]desiredSkillResource,
) error {
	currentDir := filepath.Join(destDir, relativeDir)
	entries, err := sys.ReadDir(currentDir)
	if err != nil {
		return fmt.Errorf(messages.SyncReadFailedFmt, currentDir, err)
	}

	for _, entry := range entries {
		relativePath := filepath.Join(relativeDir, entry.Name())
		if resource, ok := desired[relativePath]; ok {
			if resource.isDir {
				if err := removeStaleSkillResources(sys, destDir, relativePath, desired); err != nil {
					return err
				}
			}
			continue
		}

		destPath := filepath.Join(destDir, relativePath)
		info, err := sys.Lstat(destPath)
		if err != nil {
			return fmt.Errorf(messages.SyncReadFailedFmt, destPath, err)
		}
		if err := removeSkillResourceNode(sys, destPath, info); err != nil {
			return err
		}
	}
	return nil
}

// removeSkillResourceNode removes a single destination node: RemoveAll for a
// directory, Remove for a file or symlink (which unlinks the symlink itself
// rather than following it). Failures are wrapped with SyncRemoveFailedFmt.
func removeSkillResourceNode(sys System, path string, info os.FileInfo) error {
	var err error
	if info.IsDir() {
		err = sys.RemoveAll(path)
	} else {
		err = sys.Remove(path)
	}
	if err != nil {
		return fmt.Errorf(messages.SyncRemoveFailedFmt, path, err)
	}
	return nil
}

// loadOwnedSkills reads the ownership manifest for a projected skills directory.
//
// A directory that was projected before the manifest existed has no record, so
// the skills Agent Layer generated there are adopted once by their retired
// generated header. That migration cannot misfire on new output: a projected
// skill is now an exact copy of its source and never carries the header.
func loadOwnedSkills(sys System, skillsDir string) (map[string]string, error) {
	path := filepath.Join(skillsDir, ownedSkillsManifestName)
	data, err := sys.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return adoptLegacyGeneratedSkills(sys, skillsDir)
		}
		return nil, fmt.Errorf(messages.SyncReadFailedFmt, path, err)
	}

	var header struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return nil, fmt.Errorf(
			"%s is unreadable (%v); delete it to let 'al sync' rebuild the projected skills it owns",
			path, err,
		)
	}
	if header.Version == 1 {
		var legacy struct {
			Skills []string `json:"skills"`
		}
		if err := json.Unmarshal(data, &legacy); err != nil {
			return nil, fmt.Errorf("%s is unreadable (%v)", path, err)
		}
		owned := make(map[string]string, len(legacy.Skills))
		for _, name := range legacy.Skills {
			if !config.IsSafeSkillImportName(name) {
				return nil, fmt.Errorf("%s contains unsafe skill name %q", path, name)
			}
			if _, duplicate := owned[name]; duplicate {
				return nil, fmt.Errorf("%s contains duplicate skill name %q", path, name)
			}
			owned[name] = ""
		}
		return owned, nil
	}
	if header.Version != ownedSkillsManifestVersion {
		return nil, fmt.Errorf(
			"%s has unsupported version %d; delete it to let 'al sync' rebuild the projected skills it owns",
			path, header.Version,
		)
	}
	var manifest ownedSkillsManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("%s is unreadable (%v)", path, err)
	}
	owned := make(map[string]string, len(manifest.Skills))
	for _, name := range manifest.Skills {
		if !config.IsSafeSkillImportName(name) {
			return nil, fmt.Errorf("%s contains unsafe skill name %q", path, name)
		}
		if _, duplicate := owned[name]; duplicate {
			return nil, fmt.Errorf("%s contains duplicate skill name %q", path, name)
		}
		hash := manifest.Hashes[name]
		if !validProjectedTreeHash(hash) {
			return nil, fmt.Errorf("%s has an invalid hash for skill %q", path, name)
		}
		owned[name] = hash
	}
	if len(manifest.Hashes) != len(owned) {
		return nil, fmt.Errorf("%s hashes do not match its owned skill names", path)
	}
	return owned, nil
}

func validProjectedTreeHash(hash string) bool {
	digest := strings.TrimPrefix(hash, skilltree.HashPrefix)
	if digest == hash || len(digest) != 64 {
		return false
	}
	_, err := hex.DecodeString(digest)
	return err == nil
}

// adoptLegacyGeneratedSkills claims the skill directories a pre-manifest release
// generated, identified by the header it used to inject into SKILL.md. It runs
// only until the first manifest is written.
func adoptLegacyGeneratedSkills(sys System, skillsDir string) (map[string]string, error) {
	entries, err := sys.ReadDir(skillsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, fmt.Errorf(messages.SyncReadFailedFmt, skillsDir, err)
	}
	owned := make(map[string]string, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		generated, err := hasGeneratedMarker(sys, filepath.Join(skillsDir, entry.Name(), canonicalSkillManifestName))
		if err != nil {
			return nil, err
		}
		if generated {
			owned[entry.Name()] = ""
		}
	}
	return owned, nil
}

// writeOwnedSkills records the projected skills Agent Layer owns. An empty set
// removes the manifest rather than leaving an empty file behind.
func writeOwnedSkills(sys System, skillsDir string, owned map[string]string) error {
	path := filepath.Join(skillsDir, ownedSkillsManifestName)
	if len(owned) == 0 {
		if err := sys.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf(messages.SyncRemoveFailedFmt, path, err)
		}
		return nil
	}

	names := make([]string, 0, len(owned))
	for name := range owned {
		names = append(names, name)
	}
	sort.Strings(names)

	data, err := sys.MarshalIndent(ownedSkillsManifest{Version: ownedSkillsManifestVersion, Skills: names, Hashes: owned}, "", "  ")
	if err != nil {
		return fmt.Errorf(messages.SyncWriteFileFailedFmt, path, err)
	}
	if err := sys.WriteFileAtomic(path, append(data, '\n'), skillResourceRegularMode); err != nil {
		return fmt.Errorf(messages.SyncWriteFileFailedFmt, path, err)
	}
	return nil
}

// cleanSharedAgentSkills removes the .agents/skills entries Agent Layer owns
// when no shared-skill consumer is enabled.
func cleanSharedAgentSkills(sys System, root string) error {
	skillsDir := filepath.Join(root, ".agents", "skills")
	if _, err := sys.ReadDir(skillsDir); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf(messages.SyncReadFailedFmt, skillsDir, err)
	}
	return writeSkillProjection(sys, skillsDir, nil)
}

// cleanLegacySkillOutputs removes retired Agent Layer-generated skill
// projection directories. Agent Layer claims exclusive ownership of these
// paths (see docs/SKILL-CLIENT-SPEC.md "Ownership of legacy projection
// paths") and removes them unconditionally. The canonical list lives in
// config.LegacySkillProjections.
func cleanLegacySkillOutputs(sys System, root string) error {
	for _, projection := range config.LegacySkillProjections {
		path := filepath.Join(append([]string{root}, projection.Dir...)...)
		if err := sys.RemoveAll(path); err != nil {
			return fmt.Errorf(messages.SyncRemoveFailedFmt, path, err)
		}
	}
	return nil
}

// hasGeneratedMarker reports whether a file carries Agent Layer's generated
// header. Instruction shims still use the header; projected skills no longer do
// and only read it during the one-time ownership adoption above.
func hasGeneratedMarker(sys System, path string) (bool, error) {
	data, err := sys.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf(messages.SyncReadFailedFmt, path, err)
	}
	content := string(data)
	return strings.Contains(content, generatedMarkerHeader) &&
		strings.Contains(content, generatedMarkerSource) &&
		strings.Contains(content, generatedMarkerRegenerate), nil
}
