package doctor

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/templates"
)

// loadCLISkillCatalogFunc loads the embedded CLI skill catalog shared with the
// wizard. Override in tests.
var loadCLISkillCatalogFunc = templates.LoadCLISkillCatalog

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
//   - Catalog entries without a declared binary (e.g. dispatch-agent) are skipped.
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
