package install

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/conn-castle/agent-layer/internal/launchers"
	"github.com/conn-castle/agent-layer/internal/messages"
)

const (
	upgradeSnapshotSchemaVersion = 1
	upgradeSnapshotDirRelPath    = ".agent-layer/state/upgrade-snapshots"
	upgradeSnapshotMaxRetained   = 20
)

var upgradeSnapshotSizeWarningBytes int64 = 50 * 1024 * 1024 // 50MB

type upgradeSnapshotStatus string

const (
	upgradeSnapshotStatusCreated            upgradeSnapshotStatus = "created"
	upgradeSnapshotStatusApplied            upgradeSnapshotStatus = "applied"
	upgradeSnapshotStatusAutoRolledBack     upgradeSnapshotStatus = "auto_rolled_back"
	upgradeSnapshotStatusManuallyRolledBack upgradeSnapshotStatus = "manually_rolled_back"
	upgradeSnapshotStatusRollbackFailed     upgradeSnapshotStatus = "rollback_failed"
)

type upgradeSnapshotEntryKind string

const (
	upgradeSnapshotEntryKindFile   upgradeSnapshotEntryKind = "file"
	upgradeSnapshotEntryKindDir    upgradeSnapshotEntryKind = "dir"
	upgradeSnapshotEntryKindAbsent upgradeSnapshotEntryKind = "absent"
)

type upgradeSnapshotEntry struct {
	Path          string                   `json:"path"`
	Kind          upgradeSnapshotEntryKind `json:"kind"`
	Perm          *uint32                  `json:"perm,omitempty"`
	ContentBase64 string                   `json:"content_base64,omitempty"`
}

type upgradeSnapshot struct {
	SchemaVersion int                    `json:"schema_version"`
	SnapshotID    string                 `json:"snapshot_id"`
	CreatedAtUTC  string                 `json:"created_at_utc"`
	Status        upgradeSnapshotStatus  `json:"status"`
	FailureStep   string                 `json:"failure_step,omitempty"`
	FailureError  string                 `json:"failure_error,omitempty"`
	Entries       []upgradeSnapshotEntry `json:"entries"`
}

type upgradeSnapshotFile struct {
	path      string
	createdAt time.Time
	id        string
}

// UpgradeSnapshotMetadata provides lightweight snapshot listing fields.
type UpgradeSnapshotMetadata struct {
	ID           string
	CreatedAtUTC string
	Status       string
}

// ListUpgradeSnapshots returns metadata for all available upgrade snapshots, sorted by creation time (newest first).
func ListUpgradeSnapshots(root string, sys System) ([]UpgradeSnapshotMetadata, error) {
	if strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf(messages.InstallRootRequired)
	}
	if sys == nil {
		return nil, fmt.Errorf(messages.InstallSystemRequired)
	}
	inst := &installer{root: root, sys: sys}
	files, err := inst.listUpgradeSnapshotFiles()
	if err != nil {
		return nil, err
	}
	// listUpgradeSnapshotFiles returns oldest first; we want newest first.
	out := make([]UpgradeSnapshotMetadata, 0, len(files))
	for i := len(files) - 1; i >= 0; i-- {
		snapshot, err := readUpgradeSnapshot(files[i].path, sys)
		if err != nil {
			// Skip unreadable/malformed snapshots instead of aborting the list.
			continue
		}
		out = append(out, UpgradeSnapshotMetadata{
			ID:           snapshot.SnapshotID,
			CreatedAtUTC: snapshot.CreatedAtUTC,
			Status:       string(snapshot.Status),
		})
	}
	return out, nil
}

func (inst *installer) createUpgradeSnapshot() (upgradeSnapshot, error) {
	entries, err := inst.captureUpgradeSnapshotEntries()
	if err != nil {
		return upgradeSnapshot{}, err
	}
	now := time.Now().UTC()
	snapshot := upgradeSnapshot{
		SchemaVersion: upgradeSnapshotSchemaVersion,
		SnapshotID:    newUpgradeSnapshotID(now),
		CreatedAtUTC:  now.Format(time.RFC3339),
		Status:        upgradeSnapshotStatusCreated,
		Entries:       entries,
	}
	if err := inst.writeUpgradeSnapshot(snapshot, true); err != nil {
		return upgradeSnapshot{}, err
	}
	_, _ = fmt.Fprintf(inst.warnOutput(), messages.InstallUpgradeSnapshotCreatedFmt, snapshot.SnapshotID, snapshot.SnapshotID)
	return snapshot, nil
}

func newUpgradeSnapshotID(now time.Time) string {
	return fmt.Sprintf("%s-%d", now.UTC().Format("20060102-150405"), now.UTC().UnixNano())
}

func (inst *installer) writeUpgradeSnapshot(snapshot upgradeSnapshot, pruneBeforeCreate bool) error {
	if err := validateUpgradeSnapshot(snapshot); err != nil {
		return fmt.Errorf("validate upgrade snapshot: %w", err)
	}
	if pruneBeforeCreate {
		if err := inst.pruneUpgradeSnapshots(upgradeSnapshotMaxRetained - 1); err != nil {
			return err
		}
	}
	dir := inst.upgradeSnapshotDirPath()
	if err := inst.sys.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf(messages.InstallFailedCreateDirForFmt, dir, err)
	}
	path := filepath.Join(dir, snapshot.SnapshotID+".json")
	if err := writeUpgradeSnapshotFile(path, snapshot, inst.sys); err != nil {
		return err
	}

	// Check size and warn if it exceeds threshold.
	if info, err := inst.sys.Stat(path); err == nil && info.Size() > upgradeSnapshotSizeWarningBytes {
		_, _ = fmt.Fprintf(inst.warnOutput(), messages.InstallUpgradeSnapshotLargeWarningFmt, path, info.Size()/1024/1024, upgradeSnapshotSizeWarningBytes/1024/1024)
	}
	return nil
}

func writeUpgradeSnapshotFile(path string, snapshot upgradeSnapshot, sys System) error {
	if err := validateUpgradeSnapshot(snapshot); err != nil {
		return fmt.Errorf("validate upgrade snapshot: %w", err)
	}
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal upgrade snapshot: %w", err)
	}
	data = append(data, '\n')
	if err := sys.WriteFileAtomic(path, data, 0o644); err != nil {
		return fmt.Errorf(messages.InstallFailedWriteFmt, path, err)
	}
	return nil
}

func readUpgradeSnapshot(path string, sys System) (upgradeSnapshot, error) {
	data, err := sys.ReadFile(path)
	if err != nil {
		return upgradeSnapshot{}, fmt.Errorf(messages.InstallFailedReadFmt, path, err)
	}
	var snapshot upgradeSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return upgradeSnapshot{}, fmt.Errorf("decode upgrade snapshot %s: %w", path, err)
	}
	if err := validateUpgradeSnapshot(snapshot); err != nil {
		return upgradeSnapshot{}, fmt.Errorf("validate upgrade snapshot %s: %w", path, err)
	}
	return snapshot, nil
}

func validateUpgradeSnapshot(snapshot upgradeSnapshot) error {
	if snapshot.SchemaVersion != upgradeSnapshotSchemaVersion {
		return fmt.Errorf("unsupported schema_version %d", snapshot.SchemaVersion)
	}
	if strings.TrimSpace(snapshot.SnapshotID) == "" {
		return fmt.Errorf("snapshot_id is required")
	}
	if strings.TrimSpace(snapshot.CreatedAtUTC) == "" {
		return fmt.Errorf("created_at_utc is required")
	}
	if _, err := time.Parse(time.RFC3339, snapshot.CreatedAtUTC); err != nil {
		return fmt.Errorf("invalid created_at_utc %q: %w", snapshot.CreatedAtUTC, err)
	}
	switch snapshot.Status {
	case upgradeSnapshotStatusCreated, upgradeSnapshotStatusApplied, upgradeSnapshotStatusAutoRolledBack, upgradeSnapshotStatusManuallyRolledBack, upgradeSnapshotStatusRollbackFailed:
	default:
		return fmt.Errorf("invalid status %q", snapshot.Status)
	}
	seen := make(map[string]struct{}, len(snapshot.Entries))
	for _, entry := range snapshot.Entries {
		if err := validateUpgradeSnapshotEntry(entry); err != nil {
			return err
		}
		if _, ok := seen[entry.Path]; ok {
			return fmt.Errorf("duplicate snapshot entry path %q", entry.Path)
		}
		seen[entry.Path] = struct{}{}
	}
	return nil
}

func validateUpgradeSnapshotEntry(entry upgradeSnapshotEntry) error {
	if strings.TrimSpace(entry.Path) == "" {
		return fmt.Errorf("snapshot entry path is required")
	}
	switch entry.Kind {
	case upgradeSnapshotEntryKindFile:
		if _, err := base64.StdEncoding.DecodeString(entry.ContentBase64); err != nil {
			return fmt.Errorf("file snapshot entry %s has invalid content_base64: %w", entry.Path, err)
		}
	case upgradeSnapshotEntryKindDir:
		if entry.ContentBase64 != "" {
			return fmt.Errorf("dir snapshot entry %s must not set content_base64", entry.Path)
		}
	case upgradeSnapshotEntryKindAbsent:
		if entry.ContentBase64 != "" {
			return fmt.Errorf("absent snapshot entry %s must not set content_base64", entry.Path)
		}
		if entry.Perm != nil {
			return fmt.Errorf("absent snapshot entry %s must not set perm", entry.Path)
		}
	default:
		return fmt.Errorf("invalid snapshot entry kind %q", entry.Kind)
	}
	return nil
}

func (inst *installer) pruneUpgradeSnapshots(retain int) error {
	if retain < 0 {
		return fmt.Errorf("retain must be non-negative, got %d", retain)
	}
	dir := inst.upgradeSnapshotDirPath()
	if _, err := inst.sys.Stat(dir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf(messages.InstallFailedStatFmt, dir, err)
	}
	files, err := inst.listUpgradeSnapshotFiles()
	if err != nil {
		return fmt.Errorf("list upgrade snapshots under %s: %w", dir, err)
	}
	if len(files) <= retain {
		return nil
	}
	for i := 0; i < len(files)-retain; i++ {
		if err := inst.sys.RemoveAll(files[i].path); err != nil {
			return fmt.Errorf("delete old upgrade snapshot %s: %w", files[i].path, err)
		}
	}
	return nil
}

func (inst *installer) listUpgradeSnapshotFiles() ([]upgradeSnapshotFile, error) {
	dir := inst.upgradeSnapshotDirPath()
	if _, err := inst.sys.Stat(dir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf(messages.InstallFailedStatFmt, dir, err)
	}
	files := make([]upgradeSnapshotFile, 0)
	if err := inst.sys.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".json") {
			return nil
		}
		snapshot, ok := readUpgradeSnapshotIfValid(path, inst.sys)
		if !ok {
			// Skip unreadable/malformed snapshots to ensure the list/prune
			// operations can continue. Malformed snapshots are rare and
			// should not block the entire upgrade lifecycle.
			return nil
		}
		createdAt, parseErr := time.Parse(time.RFC3339, snapshot.CreatedAtUTC)
		if parseErr != nil {
			return fmt.Errorf("parse created_at_utc for %s: %w", path, parseErr)
		}
		files = append(files, upgradeSnapshotFile{
			path:      path,
			createdAt: createdAt,
			id:        snapshot.SnapshotID,
		})
		return nil
	}); err != nil {
		return nil, err
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].createdAt.Equal(files[j].createdAt) {
			return files[i].id < files[j].id
		}
		return files[i].createdAt.Before(files[j].createdAt)
	})
	return files, nil
}

func readUpgradeSnapshotIfValid(path string, sys System) (upgradeSnapshot, bool) {
	snapshot, err := readUpgradeSnapshot(path, sys)
	if err != nil {
		return upgradeSnapshot{}, false
	}
	return snapshot, true
}

func (inst *installer) rollbackUpgradeSnapshot(snapshot upgradeSnapshot, targets []string) error {
	return rollbackUpgradeSnapshotState(inst.root, inst.sys, snapshot, targets)
}

func (inst *installer) captureUpgradeSnapshotEntries() ([]upgradeSnapshotEntry, error) {
	targets := inst.upgradeSnapshotTargetPaths()
	entries := make(map[string]upgradeSnapshotEntry)
	for _, target := range targets {
		if err := inst.captureUpgradeSnapshotTarget(target, entries); err != nil {
			return nil, err
		}
	}
	out := make([]upgradeSnapshotEntry, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Path < out[j].Path
	})
	return out, nil
}

func (inst *installer) captureUpgradeSnapshotTarget(target string, entries map[string]upgradeSnapshotEntry) error {
	info, err := inst.sys.Stat(target)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			relPath, relErr := inst.repoRelativePath(target)
			if relErr != nil {
				return relErr
			}
			upsertUpgradeSnapshotEntry(entries, upgradeSnapshotEntry{
				Path: relPath,
				Kind: upgradeSnapshotEntryKindAbsent,
			})
			return nil
		}
		return fmt.Errorf(messages.InstallFailedStatFmt, target, err)
	}
	if info.IsDir() {
		return inst.captureUpgradeSnapshotDirectory(target, entries)
	}
	if !info.Mode().IsRegular() {
		relPath, relErr := inst.repoRelativePath(target)
		if relErr != nil {
			return relErr
		}
		return fmt.Errorf("unsupported file type for snapshot path %s", relPath)
	}
	return inst.captureUpgradeSnapshotFile(target, info.Mode(), entries)
}

func (inst *installer) captureUpgradeSnapshotDirectory(rootPath string, entries map[string]upgradeSnapshotEntry) error {
	return inst.sys.WalkDir(rootPath, func(path string, dirEntry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, infoErr := dirEntry.Info()
		if infoErr != nil {
			return infoErr
		}
		if dirEntry.IsDir() {
			relPath, relErr := inst.repoRelativePath(path)
			if relErr != nil {
				return relErr
			}
			upsertUpgradeSnapshotEntry(entries, upgradeSnapshotEntry{
				Path: relPath,
				Kind: upgradeSnapshotEntryKindDir,
				Perm: permToSnapshot(info.Mode()),
			})
			return nil
		}
		if !info.Mode().IsRegular() {
			relPath, relErr := inst.repoRelativePath(path)
			if relErr != nil {
				return relErr
			}
			return fmt.Errorf("unsupported file type for snapshot path %s", relPath)
		}
		return inst.captureUpgradeSnapshotFile(path, info.Mode(), entries)
	})
}

func (inst *installer) captureUpgradeSnapshotFile(path string, mode fs.FileMode, entries map[string]upgradeSnapshotEntry) error {
	relPath, err := inst.repoRelativePath(path)
	if err != nil {
		return err
	}
	content, err := inst.sys.ReadFile(path)
	if err != nil {
		return fmt.Errorf(messages.InstallFailedReadFmt, path, err)
	}
	upsertUpgradeSnapshotEntry(entries, upgradeSnapshotEntry{
		Path:          relPath,
		Kind:          upgradeSnapshotEntryKindFile,
		Perm:          permToSnapshot(mode),
		ContentBase64: base64.StdEncoding.EncodeToString(content),
	})
	return nil
}

func upsertUpgradeSnapshotEntry(entries map[string]upgradeSnapshotEntry, candidate upgradeSnapshotEntry) {
	current, exists := entries[candidate.Path]
	if !exists {
		entries[candidate.Path] = candidate
		return
	}
	if current.Kind == upgradeSnapshotEntryKindAbsent && candidate.Kind != upgradeSnapshotEntryKindAbsent {
		entries[candidate.Path] = candidate
	}
}

func (inst *installer) writeVersionFileTargetPaths() []string {
	return []string{filepath.Join(inst.root, ".agent-layer", "al.version")}
}

func (inst *installer) writeTemplateFilesTargetPaths() []string {
	paths := make([]string, 0, len(inst.managedTemplateFiles())+len(inst.agentOnlyFiles()))
	for _, file := range inst.managedTemplateFiles() {
		paths = append(paths, file.path)
	}
	for _, file := range inst.agentOnlyFiles() {
		paths = append(paths, file.path)
	}
	return uniqueNormalizedPaths(paths)
}

func (inst *installer) writeTemplateDirsTargetPaths() []string {
	paths := make([]string, 0, len(inst.managedTemplateDirs())+len(inst.memoryTemplateDirs()))
	for _, dir := range inst.managedTemplateDirs() {
		paths = append(paths, dir.destRoot)
	}
	for _, dir := range inst.memoryTemplateDirs() {
		paths = append(paths, dir.destRoot)
	}
	return uniqueNormalizedPaths(paths)
}

func (inst *installer) runMigrationsTargetPaths() []string {
	return uniqueNormalizedPaths(inst.migrationRollbackTargets)
}

func (inst *installer) updateGitignoreTargetPaths() []string {
	return []string{filepath.Join(inst.root, ".gitignore")}
}

func (inst *installer) writeVSCodeLaunchersTargetPaths() []string {
	return uniqueNormalizedPaths(launchers.VSCodePaths(inst.root).All())
}

func (inst *installer) handleUnknownsTargetPaths() []string {
	return uniqueNormalizedPaths(inst.unknowns)
}

func (inst *installer) upgradeSnapshotTargetPaths() []string {
	root := inst.root
	paths := make(map[string]struct{})
	add := func(path string) {
		paths[filepath.Clean(path)] = struct{}{}
	}

	add(filepath.Join(root, ".agent-layer", "al.version"))
	add(filepath.Join(root, ".agent-layer", ".gitignore"))
	for _, file := range inst.managedTemplateFiles() {
		add(file.path)
	}
	for _, dir := range inst.managedTemplateDirs() {
		add(dir.destRoot)
	}
	for _, dir := range inst.memoryTemplateDirs() {
		add(dir.destRoot)
	}
	add(filepath.Join(root, ".gitignore"))
	for _, path := range launchers.VSCodePaths(root).All() {
		add(path)
	}
	for _, path := range inst.runMigrationsTargetPaths() {
		add(path)
	}
	// Snapshot capture depends on scanUnknowns() having populated inst.unknowns
	// before the transaction begins. This ensures rollback can restore unknown
	// paths that may be deleted in handleUnknowns().
	for _, path := range inst.unknowns {
		add(path)
	}

	out := make([]string, 0, len(paths))
	for path := range paths {
		out = append(out, path)
	}
	sort.Strings(out)
	return out
}

func uniqueNormalizedPaths(paths []string) []string {
	if len(paths) == 0 {
		return nil
	}
	dedup := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		clean := filepath.Clean(path)
		if clean == "." || clean == "" {
			continue
		}
		dedup[clean] = struct{}{}
	}
	out := make([]string, 0, len(dedup))
	for path := range dedup {
		out = append(out, path)
	}
	sort.Strings(out)
	return out
}

func (inst *installer) repoRelativePath(path string) (string, error) {
	rel, err := filepath.Rel(inst.root, path)
	if err != nil {
		return "", err
	}
	rel = filepath.Clean(rel)
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("path %s is outside repo root %s", path, inst.root)
	}
	return normalizeRelPath(rel), nil
}

func (inst *installer) upgradeSnapshotDirPath() string {
	return filepath.Join(inst.root, filepath.FromSlash(upgradeSnapshotDirRelPath))
}

func permToSnapshot(mode fs.FileMode) *uint32 {
	perm := uint32(mode.Perm())
	return &perm
}

func permFromSnapshot(perm *uint32, fallback fs.FileMode) fs.FileMode {
	if perm == nil {
		return fallback
	}
	return fs.FileMode(*perm) & os.ModePerm
}
