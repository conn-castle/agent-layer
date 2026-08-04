package sync

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	yaml "go.yaml.in/yaml/v3"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/skilltree"
)

const promptHeaderTemplate = "<!--\n  GENERATED FILE\n  Source: %s\n  Regenerate: al sync\n-->\n"

const (
	generatedMarkerHeader     = "GENERATED FILE"
	generatedMarkerSource     = "Source: .agent-layer/"
	generatedMarkerRegenerate = "Regenerate: al sync"
)

// skillContentBuilder builds skill file content for a skill.
type skillContentBuilder func(cmd config.Skill) (string, error)

const (
	// stagingPrefix names the off-path directory a skill projection is built in
	// before it is published.
	stagingPrefix = ".al-stage-"
	// backupPrefix names the directory a previous projection is moved aside to
	// during publication so a failed swap can be rolled back.
	backupPrefix = ".al-backup-"
)

// writeSkillFiles projects every skill into skillsDir.
//
// Each skill's complete tree — its generated SKILL.md plus every regular
// resource file from its editable source — is built in an off-path staging
// directory and then published with a rollback-capable rename sequence, so a
// reader sees either the previous complete tree or the new complete tree. POSIX
// rename cannot replace a non-empty directory in one call, so publication moves
// the previous tree aside first and restores it if the swap fails.
func writeSkillFiles(sys System, skillsDir string, commands []config.Skill, buildContent skillContentBuilder) error {
	if err := sys.MkdirAll(skillsDir, 0o755); err != nil {
		return fmt.Errorf(messages.SyncCreateDirFailedFmt, skillsDir, err)
	}

	wanted := make(map[string]struct{}, len(commands))
	for _, cmd := range commands {
		if strings.Contains(cmd.Name, "..") || strings.ContainsAny(cmd.Name, `/\`) {
			return fmt.Errorf("invalid skill name %q: must not contain path separators or '..'", cmd.Name)
		}
		content, err := buildContent(cmd)
		if err != nil {
			return fmt.Errorf(messages.SyncWriteFileFailedFmt, filepath.Join(skillsDir, cmd.Name, skillManifestName), err)
		}
		wanted[cmd.Name] = struct{}{}
		if err := publishSkillProjection(sys, skillsDir, cmd, content); err != nil {
			return err
		}
	}

	return removeStaleSkillDirs(sys, skillsDir, wanted)
}

// publishSkillProjection stages one skill's complete projected tree and swaps
// it into place.
func publishSkillProjection(sys System, skillsDir string, skill config.Skill, content string) error {
	target := filepath.Join(skillsDir, skill.Name)
	staging := filepath.Join(skillsDir, stagingPrefix+skill.Name)
	backup := filepath.Join(skillsDir, backupPrefix+skill.Name)

	if err := recoverInterruptedPublication(sys, target, backup); err != nil {
		return err
	}
	if err := sys.RemoveAll(staging); err != nil {
		return fmt.Errorf(messages.SyncRemoveFailedFmt, staging, err)
	}
	if err := sys.RemoveAll(backup); err != nil {
		return fmt.Errorf(messages.SyncRemoveFailedFmt, backup, err)
	}
	if err := stageSkillProjection(sys, staging, skill, content); err != nil {
		_ = sys.RemoveAll(staging)
		return err
	}

	restored, err := swapSkillProjection(sys, target, staging, backup)
	if err != nil {
		_ = sys.RemoveAll(staging)
		if !restored {
			return err
		}
		return err
	}
	if err := sys.RemoveAll(backup); err != nil {
		return fmt.Errorf(messages.SyncRemoveFailedFmt, backup, err)
	}
	return nil
}

// recoverInterruptedPublication restores a previous projection that an
// interrupted publication left parked in its backup directory.
//
// Publication moves the live tree aside and then renames the staged tree into
// place. A process that dies between those two renames leaves the backup
// holding the only copy of the skill and no tree at the target. Inspecting that
// before clearing the backup is what makes projection restart-safe: the reader
// sees the previous complete tree instead of nothing at all.
func recoverInterruptedPublication(sys System, target string, backup string) error {
	if _, err := sys.Lstat(backup); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf(messages.SyncReadFailedFmt, backup, err)
	}
	if _, err := sys.Lstat(target); err == nil {
		// The swap completed; only its cleanup was interrupted.
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf(messages.SyncReadFailedFmt, target, err)
	}
	if err := sys.Rename(backup, target); err != nil {
		return fmt.Errorf(messages.SyncRenameFailedFmt, backup, target, err)
	}
	return nil
}

// stageSkillProjection materializes the generated manifest plus every source
// resource file into an empty staging directory.
func stageSkillProjection(sys System, staging string, skill config.Skill, content string) error {
	if err := sys.MkdirAll(staging, 0o755); err != nil {
		return fmt.Errorf(messages.SyncCreateDirFailedFmt, staging, err)
	}

	tree, err := skilltree.Read(sys, skill.SourceDir, skillNodePolicy(skill))
	if err != nil {
		return fmt.Errorf(messages.SyncReadFailedFmt, skill.SourceDir, err)
	}
	for _, file := range tree.Files() {
		if isSourceManifestPath(file.Path) {
			// The content builder owns the projected manifest.
			continue
		}
		if err := skilltree.ValidateRelativePath(file.Path); err != nil {
			return fmt.Errorf(messages.SyncReadFailedFmt, skill.SourceDir, err)
		}
		destPath := filepath.Join(staging, filepath.FromSlash(file.Path))
		if err := sys.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
			return fmt.Errorf(messages.SyncCreateDirFailedFmt, filepath.Dir(destPath), err)
		}
		if err := sys.WriteFileAtomic(destPath, file.Data, file.FileMode()); err != nil {
			return fmt.Errorf(messages.SyncWriteFileFailedFmt, destPath, err)
		}
	}

	manifestPath := filepath.Join(staging, skillManifestName)
	if err := sys.WriteFileAtomic(manifestPath, []byte(content), 0o644); err != nil {
		return fmt.Errorf(messages.SyncWriteFileFailedFmt, manifestPath, err)
	}
	return nil
}

// swapSkillProjection replaces target with staging. It reports whether a
// rollback restored the previous tree when the swap failed.
func swapSkillProjection(sys System, target string, staging string, backup string) (restored bool, err error) {
	hadPrevious := false
	if _, statErr := sys.Lstat(target); statErr == nil {
		if renameErr := sys.Rename(target, backup); renameErr != nil {
			return false, fmt.Errorf(messages.SyncRenameFailedFmt, target, backup, renameErr)
		}
		hadPrevious = true
	} else if !os.IsNotExist(statErr) {
		return false, fmt.Errorf(messages.SyncReadFailedFmt, target, statErr)
	}

	if renameErr := sys.Rename(staging, target); renameErr != nil {
		publishErr := fmt.Errorf(messages.SyncRenameFailedFmt, staging, target, renameErr)
		if !hadPrevious {
			return false, publishErr
		}
		if rollbackErr := sys.Rename(backup, target); rollbackErr != nil {
			return false, fmt.Errorf("%w; rollback of %s also failed: %v", publishErr, target, rollbackErr)
		}
		return true, publishErr
	}
	return false, nil
}

// skillNodePolicy selects the node policy for a skill's editable source.
// Imported sources are fully managed and reject unsafe node types; existing
// user-managed sources keep the historical symlink skip so a project that
// already contains one does not start failing ordinary sync.
func skillNodePolicy(skill config.Skill) skilltree.NodePolicy {
	if skill.Imported {
		return skilltree.PolicyStrict
	}
	return skilltree.PolicyLenient
}

// isSourceManifestPath reports whether a source tree path is the skill manifest
// the content builder regenerates.
func isSourceManifestPath(relativePath string) bool {
	return relativePath == skillManifestName || relativePath == lowercaseSkillManifestName
}

// WriteAgentSkills generates shared Agent Skills in .agents/skills/<name>/SKILL.md.
// sys performs filesystem operations, root is the projection root, and commands
// supplies the canonical loaded skills. It returns the first reconciliation error.
func WriteAgentSkills(sys System, root string, commands []config.Skill) error {
	skillsDir := filepath.Join(root, ".agents", "skills")
	return writeSkillFiles(sys, skillsDir, commands, buildAgentSkill)
}

// WriteClaudeSkills generates Claude Code skill files in .claude/skills/<name>/SKILL.md.
// sys performs filesystem operations, root is the projection root, and commands
// supplies the canonical loaded skills. It returns the first reconciliation error.
func WriteClaudeSkills(sys System, root string, commands []config.Skill) error {
	skillsDir := filepath.Join(root, ".claude", "skills")
	return writeSkillFiles(sys, skillsDir, commands, buildClaudeSkill)
}

// buildAgentSkill returns portable Agent Skills SKILL.md content.
func buildAgentSkill(cmd config.Skill) (string, error) {
	return buildSkill(cmd, false)
}

// buildClaudeSkill returns Claude Code SKILL.md content with Claude-specific
// frontmatter preserved.
func buildClaudeSkill(cmd config.Skill) (string, error) {
	return buildSkill(cmd, true)
}

// buildSkill renders a skill and optionally includes client-specific Claude fields.
func buildSkill(cmd config.Skill, includeClaudeFields bool) (string, error) {
	var builder strings.Builder
	frontMatter, err := buildSkillFrontMatter(cmd, includeClaudeFields)
	if err != nil {
		return "", err
	}
	builder.WriteString(frontMatter)
	fmt.Fprintf(&builder, promptHeaderTemplate, generatedSkillSourcePath(cmd))
	if cmd.Body != "" {
		builder.WriteString("\n")
		builder.WriteString(cmd.Body)
		if !strings.HasSuffix(cmd.Body, "\n") {
			builder.WriteString("\n")
		}
	}
	return builder.String(), nil
}

// buildSkillFrontMatter renders canonical fields and optional Claude extensions.
func buildSkillFrontMatter(cmd config.Skill, includeClaudeFields bool) (string, error) {
	root := &yaml.Node{Kind: yaml.MappingNode}
	appendFrontMatterScalar(root, "name", strings.TrimSpace(cmd.Name))
	appendFrontMatterDescription(root, strings.TrimSpace(cmd.Description))
	if includeClaudeFields {
		appendFrontMatterOptionalBoolean(root, "disable-model-invocation", cmd.DisableModelInvocation)
	}
	appendFrontMatterOptionalScalar(root, "license", strings.TrimSpace(cmd.License))
	appendFrontMatterOptionalScalar(root, "compatibility", strings.TrimSpace(cmd.Compatibility))
	appendFrontMatterMetadata(root, copyMetadata(cmd.Metadata))
	appendFrontMatterOptionalScalar(root, "allowed-tools", strings.TrimSpace(cmd.AllowedTools))

	var yamlBody strings.Builder
	encoder := yaml.NewEncoder(&yamlBody)
	encoder.SetIndent(2)
	if err := encoder.Encode(root); err != nil {
		return "", err
	}
	if err := encoder.Close(); err != nil {
		return "", err
	}

	var frontMatter strings.Builder
	frontMatter.WriteString("---\n")
	frontMatter.WriteString(strings.TrimSuffix(yamlBody.String(), "\n"))
	frontMatter.WriteString("\n---\n\n")
	return frontMatter.String(), nil
}

// appendFrontMatterOptionalBoolean appends a present boolean without inventing a default.
func appendFrontMatterOptionalBoolean(root *yaml.Node, key string, value *bool) {
	if value == nil {
		return
	}
	root.Content = append(root.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: strconv.FormatBool(*value)},
	)
}

func appendFrontMatterScalar(root *yaml.Node, key string, value string) {
	root.Content = append(root.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Value: value},
	)
}

func appendFrontMatterDescription(root *yaml.Node, description string) {
	style := yaml.FoldedStyle
	if strings.Contains(description, "\n") {
		style = yaml.LiteralStyle
	}
	root.Content = append(root.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: "description"},
		&yaml.Node{Kind: yaml.ScalarNode, Value: description, Style: style},
	)
}

func appendFrontMatterOptionalScalar(root *yaml.Node, key string, value string) {
	if value == "" {
		return
	}
	appendFrontMatterScalar(root, key, value)
}

func appendFrontMatterMetadata(root *yaml.Node, metadata map[string]string) {
	if len(metadata) == 0 {
		return
	}
	keys := make([]string, 0, len(metadata))
	for key := range metadata {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	metadataNode := &yaml.Node{Kind: yaml.MappingNode}
	for _, key := range keys {
		metadataNode.Content = append(metadataNode.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: key},
			&yaml.Node{Kind: yaml.ScalarNode, Value: metadata[key]},
		)
	}
	root.Content = append(root.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: "metadata"},
		metadataNode,
	)
}

func copyMetadata(metadata map[string]string) map[string]string {
	if len(metadata) == 0 {
		return nil
	}
	normalized := make(map[string]string, len(metadata))
	for key, value := range metadata {
		normalized[key] = value
	}
	return normalized
}

func generatedSkillSourcePath(cmd config.Skill) string {
	defaultPath := filepath.ToSlash(filepath.Join(".agent-layer", "skills", cmd.Name, "SKILL.md"))
	source := strings.TrimSpace(cmd.SourcePath)
	if source == "" {
		return defaultPath
	}
	normalized := filepath.ToSlash(source)
	if strings.HasPrefix(normalized, ".agent-layer/") {
		return normalized
	}
	marker := "/.agent-layer/"
	if idx := strings.LastIndex(normalized, marker); idx >= 0 {
		return normalized[idx+1:]
	}
	return defaultPath
}

func removeStaleSkillDirs(sys System, skillsDir string, wanted map[string]struct{}) error {
	entries, err := sys.ReadDir(skillsDir)
	if err != nil {
		return fmt.Errorf(messages.SyncReadFailedFmt, skillsDir, err)
	}

	var stale []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if _, ok := wanted[name]; ok {
			continue
		}
		skillPath := filepath.Join(skillsDir, name, "SKILL.md")
		isGenerated, err := hasGeneratedMarker(sys, skillPath)
		if err != nil {
			return err
		}
		if isGenerated {
			stale = append(stale, filepath.Join(skillsDir, name))
		}
	}

	sort.Strings(stale)
	for _, dir := range stale {
		if err := sys.RemoveAll(dir); err != nil {
			return fmt.Errorf(messages.SyncRemoveFailedFmt, dir, err)
		}
	}

	return nil
}

// cleanSharedAgentSkills removes generated .agents/skills entries when no shared-skill consumer is enabled.
func cleanSharedAgentSkills(sys System, root string) error {
	skillsDir := filepath.Join(root, ".agents", "skills")
	if _, err := sys.ReadDir(skillsDir); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf(messages.SyncReadFailedFmt, skillsDir, err)
	}
	return removeStaleSkillDirs(sys, skillsDir, map[string]struct{}{})
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
