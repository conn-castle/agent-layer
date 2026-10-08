package doctor

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/skillimport"
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
// Remote entries inspect stable local config/lock/tree evidence and report
// ownership conflicts. Pending recovery is reported with sync guidance, never
// performed by doctor. Embedded entries use their real local tier. No fetches occur.
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
	checkedBinaries := map[string]bool{}
	for _, entry := range entries {
		installed := false
		if entry.Repository != "" {
			members, err := skillimport.InspectCatalogState(cfg.Root, entry)
			if err != nil {
				recommendation := "Preserve source tiers and repair the reported local evidence; doctor does not fetch or recover."
				if errors.Is(err, skillimport.ErrPendingRecovery) {
					recommendation = "Preserve live data and staging evidence; run al sync to recover the interrupted import, then rerun doctor."
				}
				return []Result{{Status: StatusFail, CheckName: messages.DoctorCheckNameCLISkills, Message: err.Error(), Recommendation: recommendation}}
			}
			for _, member := range members {
				if member.Problem != "" && !member.ForeignOwned {
					results = append(results, Result{Status: StatusFail, CheckName: messages.DoctorCheckNameCLISkills, Message: member.Name + ": " + member.Problem, Recommendation: "Preserve both tiers and resolve ownership with al skills commands; doctor does not fetch."})
				}
				if member.Imported || member.Legacy || member.ForeignOwned {
					installed = true
				}
			}
		} else {
			tier := filepath.Join(cfg.Root, ".agent-layer", "skills")
			if info, err := os.Lstat(tier); err != nil || !info.IsDir() {
				continue
			}
			info, err := os.Lstat(filepath.Join(tier, entry.ID))
			installed = err == nil && info.IsDir()
		}
		if !installed || entry.Binary == "" || checkedBinaries[entry.Binary] {
			continue
		}
		checkedBinaries[entry.Binary] = true
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
