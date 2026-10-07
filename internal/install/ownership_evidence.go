package install

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/templates"
)

// The checks below read the evidence recorded for a differing or removable
// managed file (repo-local baseline, pinned release manifest, legacy docs
// snapshot) and fail when that evidence is unreadable or malformed. They return
// no result: planning and overwrite review only need the validation.

// parseOwnershipComparable reports whether relPath's content can be compared.
// An unparseable local file skips evidence reads, as it did during classification.
func parseOwnershipComparable(relPath string, content []byte) (ownershipComparable, bool) {
	comp, _, err := classifyComparable(relPath, content)
	if err != nil {
		return ownershipComparable{}, false
	}
	return comp, true
}

func (inst *installer) checkTemplateDiffEvidence(relPath, templatePath string) error {
	localPath := filepath.Join(inst.root, filepath.FromSlash(relPath))
	localBytes, err := inst.sys.ReadFile(localPath)
	if err != nil {
		return err
	}
	templateBytes, err := templates.Read(templatePath)
	if err != nil {
		return err
	}
	return inst.checkOwnershipEvidence(relPath, localBytes, templateBytes, false)
}

func (inst *installer) checkRemovalEvidence(relPath string) error {
	localPath := filepath.Join(inst.root, filepath.FromSlash(relPath))
	info, err := inst.sys.Lstat(localPath)
	if errors.Is(err, os.ErrNotExist) {
		// A path a migration has yet to create has no content to check.
		return nil
	}
	if err != nil {
		return fmt.Errorf(messages.InstallFailedStatFmt, localPath, err)
	}
	if !info.Mode().IsRegular() {
		return nil
	}
	localBytes, err := inst.sys.ReadFile(localPath)
	if err != nil {
		return err
	}
	if strings.HasPrefix(relPath, ".agent-layer/templates/docs/") {
		return nil
	}
	return inst.checkOwnershipEvidence(relPath, localBytes, nil, true)
}

func (inst *installer) checkOwnershipEvidence(relPath string, localBytes, templateBytes []byte, orphan bool) error {
	localComp, ok := parseOwnershipComparable(relPath, localBytes)
	if !ok {
		return nil
	}
	if !orphan {
		_, reasonCode, err := classifyComparable(relPath, templateBytes)
		if err != nil {
			return fmt.Errorf("parse target comparable for %s (%s): %w", relPath, reasonCode, err)
		}
	}
	return inst.checkBaselineEvidence(relPath, localComp)
}

func (inst *installer) checkBaselineEvidence(relPath string, localComp ownershipComparable) error {
	canonicalState, err := readManagedBaselineState(inst.root, inst.sys)
	if err == nil {
		if entry, ok := manifestFileMap(canonicalState.Files)[relPath]; ok {
			_, parseErr := comparableFromManifestEntry(entry)
			return parseErr
		}
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	// A broken pin counts as no pin so the plan can still preview its repair.
	pinVersion, pinErr := readCurrentPinVersion(inst.root, inst.sys)
	if pinErr != nil && !errors.As(pinErr, new(invalidPinError)) {
		return pinErr
	}
	if pinVersion != "" {
		manifest, manifestErr := loadTemplateManifestByVersion(pinVersion)
		switch {
		case manifestErr == nil:
			if entry, ok := manifestFileMap(manifest.Files)[relPath]; ok {
				comp, parseErr := comparableFromManifestEntry(entry)
				if parseErr != nil {
					return parseErr
				}
				switch {
				case comp.PolicyID != localComp.PolicyID:
					// A different policy falls through to legacy evidence.
				case comparableKey(comp) == comparableKey(localComp):
					return nil
				default:
					if _, checkErr := matchAnyOtherManifest(relPath, pinVersion, comparableKey(localComp)); checkErr != nil {
						return checkErr
					}
				}
			}
		case !errors.Is(manifestErr, os.ErrNotExist):
			return manifestErr
		}
	}
	return inst.checkLegacyDocsBaseline(relPath)
}

func (inst *installer) checkLegacyDocsBaseline(relPath string) error {
	if !strings.HasPrefix(relPath, "docs/agent-layer/") {
		return nil
	}
	suffix := strings.TrimPrefix(relPath, "docs/agent-layer/")
	legacyPath := filepath.Join(inst.root, ".agent-layer", "templates", "docs", filepath.FromSlash(suffix))
	legacyBytes, err := inst.sys.ReadFile(legacyPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	_, reasonCode, compErr := classifyComparable(relPath, legacyBytes)
	if compErr != nil {
		if reasonCode == "" {
			reasonCode = ownershipReasonPolicyPayloadInvalid
		}
		return fmt.Errorf("parse legacy baseline comparable for %s (%s): %w", relPath, reasonCode, compErr)
	}
	return nil
}

func matchAnyOtherManifest(relPath string, pinnedVersion string, key string) (bool, error) {
	manifests, err := loadAllTemplateManifests()
	if err != nil {
		return false, err
	}
	for versionValue, manifest := range manifests {
		if versionValue == pinnedVersion {
			continue
		}
		entries := manifestFileMap(manifest.Files)
		entry, ok := entries[relPath]
		if !ok {
			continue
		}
		comp, compErr := comparableFromManifestEntry(entry)
		if compErr != nil {
			return false, compErr
		}
		if comparableKey(comp) == key {
			return true, nil
		}
	}
	return false, nil
}

func classifyComparable(relPath string, content []byte) (ownershipComparable, string, error) {
	comp, err := buildOwnershipComparable(relPath, content)
	if err == nil {
		return comp, "", nil
	}
	var compErr ownershipComparableError
	if errors.As(err, &compErr) {
		return ownershipComparable{}, compErr.reasonCode, compErr
	}
	return ownershipComparable{}, ownershipReasonPolicyPayloadInvalid, err
}
