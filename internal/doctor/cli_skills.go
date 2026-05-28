package doctor

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	toml "github.com/pelletier/go-toml/v2"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/templates"
)

// cliSkillCatalogTemplatePath is the embedded CLI skill catalog used by doctor.
// It mirrors the wizard's catalog so both surfaces report the same skill set.
const cliSkillCatalogTemplatePath = "cli-skills-catalog.toml"

var cliSkillCatalogIDPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?$`)

// cliSkillCatalogEntry is the typed slice doctor parses from the catalog file.
// Doctor uses ID and Binary; the wizard reads more fields from the same file.
// Keeping a private mirror here avoids an internal/wizard dependency from
// internal/doctor.
type cliSkillCatalogEntry struct {
	ID     string `toml:"id"`
	Binary string `toml:"binary"`
}

// isSafeCLISkillCatalogID returns true for single-segment catalog ids that can
// be used as .agent-layer/skills/<id>/ directory names.
func isSafeCLISkillCatalogID(id string) bool {
	return cliSkillCatalogIDPattern.MatchString(id) && !strings.Contains(id, "/") && !strings.Contains(id, `\`)
}

// loadCLISkillCatalogForDoctor parses the embedded catalog into the doctor's
// internal slice. Override via loadCLISkillCatalogFunc in tests.
var loadCLISkillCatalogFunc = func() ([]cliSkillCatalogEntry, error) {
	data, err := templates.Read(cliSkillCatalogTemplatePath)
	if err != nil {
		return nil, err
	}
	var doc struct {
		CLISkills []cliSkillCatalogEntry `toml:"cli_skills"`
	}
	if err := toml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	seen := make(map[string]struct{}, len(doc.CLISkills))
	for idx, entry := range doc.CLISkills {
		if !isSafeCLISkillCatalogID(entry.ID) {
			return nil, fmt.Errorf("CLI skills catalog entry %d has invalid id %q", idx, entry.ID)
		}
		if _, ok := seen[entry.ID]; ok {
			return nil, fmt.Errorf("CLI skills catalog entry %d duplicates id %q", idx, entry.ID)
		}
		seen[entry.ID] = struct{}{}
	}
	return doc.CLISkills, nil
}

// CheckCLISkills verifies that each catalog skill present on disk has its
// required CLI binary on PATH. Per memory feedback_doctor_nonblocking.md, this
// check never gates agent execution; failures are surfaced as Result entries
// only.
//
// Behavior summary:
//   - If the catalog file is missing or malformed, emit a single FAIL result.
//   - For each catalog entry with a declared binary, check `.agent-layer/skills/<id>/`:
//     present + binary on PATH → no result
//     present + binary missing  → FAIL result
//     absent                    → no result (user opted out)
//   - Catalog entries without a declared binary (e.g. agent-dispatch) are skipped.
func CheckCLISkills(cfg *config.ProjectConfig) []Result {
	if cfg == nil {
		return nil
	}
	entries, err := loadCLISkillCatalogFunc()
	if err != nil {
		return []Result{{
			Status:         StatusFail,
			CheckName:      messages.DoctorCheckNameCLISkills,
			Message:        fmt.Sprintf(messages.DoctorCLISkillCatalogLoadFailedFmt, err),
			Recommendation: messages.DoctorCLISkillCatalogLoadRecommend,
		}}
	}

	var results []Result
	for _, entry := range entries {
		if entry.Binary == "" {
			continue
		}
		dir := filepath.Join(cfg.Root, ".agent-layer", "skills", entry.ID)
		info, statErr := os.Stat(dir)
		if statErr != nil || !info.IsDir() {
			// Catalog skill not installed (or path is a file) — nothing to check.
			continue
		}
		if _, lookErr := lookPathFunc(entry.Binary); lookErr != nil {
			results = append(results, Result{
				Status:         StatusFail,
				CheckName:      messages.DoctorCheckNameCLISkills,
				Message:        fmt.Sprintf(messages.DoctorCLISkillBinaryMissingFmt, entry.ID, entry.Binary),
				Recommendation: fmt.Sprintf(messages.DoctorCLISkillBinaryMissingRecommend, entry.Binary),
			})
			continue
		}
		results = append(results, Result{
			Status:    StatusOK,
			CheckName: messages.DoctorCheckNameCLISkills,
			Message:   fmt.Sprintf(messages.DoctorCLISkillBinaryOKFmt, entry.ID, entry.Binary),
		})
	}
	return results
}
