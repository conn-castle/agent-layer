package doctor

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/musepolicy"
)

// CheckMusePolicy inspects native storage only for Muse-enabled projects.
func CheckMusePolicy(ctx context.Context, cfg *config.ProjectConfig) (results []Result) {
	if ctx.Err() != nil || cfg == nil || !config.IsAgentEnabled(cfg.Config.Agents.Muse.Enabled) {
		return nil
	}
	defer func() {
		if ctx.Err() != nil {
			results = nil
		}
	}()
	dir, err := musepolicy.ConfigDir()
	if err != nil {
		return musePolicyWarning(err)
	}
	root, err := filepath.EvalSymlinks(cfg.Root)
	if err != nil {
		return musePolicyWarning(err)
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return musePolicyWarning(err)
	}
	inspection, err := musepolicy.Inspect(ctx, dir, root)
	if ctx.Err() != nil {
		return nil // Interrupted doctor checks drop partial results.
	}
	if err != nil {
		return musePolicyWarning(err)
	}
	results = []Result{{Status: StatusOK, CheckName: messages.DoctorCheckNameMusePolicy,
		Message: fmt.Sprintf(messages.DoctorMusePolicyCountFmt, inspection.Total, len(inspection.Roots))}}
	if inspection.CurrentReceiptOrphan {
		results = append(results, Result{Status: StatusWarn, CheckName: messages.DoctorCheckNameMusePolicy,
			Message:        messages.DoctorMusePolicyCurrentReceiptOrphan,
			Recommendation: messages.DoctorMusePolicyCurrentReceiptOrphanRecommend})
	}
	if inspection.CurrentReceiptUnknown {
		results = append(results, Result{Status: StatusWarn, CheckName: messages.DoctorCheckNameMusePolicy,
			Message:        messages.DoctorMusePolicyCurrentReceiptUnknown,
			Recommendation: messages.DoctorMusePolicyCurrentReceiptUnknownRecommend})
	}
	if len(inspection.Orphans) > 0 {
		results = append(results, Result{Status: StatusWarn, CheckName: messages.DoctorCheckNameMusePolicy,
			Message:        fmt.Sprintf(messages.DoctorMusePolicyOrphansFmt, len(inspection.Orphans), formatMuseRoots(inspection.Orphans)),
			Recommendation: messages.DoctorMusePolicyOrphansRecommend})
	}
	if len(inspection.Unknown) > 0 {
		results = append(results, Result{Status: StatusWarn, CheckName: messages.DoctorCheckNameMusePolicy,
			Message:        fmt.Sprintf(messages.DoctorMusePolicyUnknownFmt, len(inspection.Unknown), formatMuseRoots(inspection.Unknown)),
			Recommendation: messages.DoctorMusePolicyUnknownRecommend})
	}
	if len(inspection.Nested) > 0 {
		results = append(results, Result{Status: StatusWarn, CheckName: messages.DoctorCheckNameMusePolicy,
			Message:        fmt.Sprintf(messages.DoctorMusePolicyNestedFmt, len(inspection.Nested), formatMuseRoots(inspection.Nested)),
			Recommendation: messages.DoctorMusePolicyNestedRecommend})
	}
	return results
}

func musePolicyWarning(err error) []Result {
	return []Result{{Status: StatusWarn, CheckName: messages.DoctorCheckNameMusePolicy,
		Message: fmt.Sprintf(messages.DoctorMusePolicyReadFailedFmt, err), Recommendation: messages.DoctorMusePolicyReadRecommend}}
}

func formatMuseRoots(roots []musepolicy.RootRules) string {
	const limit = 10
	var lines []string
	for i, entry := range roots {
		if i == limit {
			lines = append(lines, fmt.Sprintf(messages.DoctorMusePolicyMoreRootsFmt, len(roots)-limit))
			break
		}
		lines = append(lines, fmt.Sprintf("%s (%d rules)", entry.Root, entry.Count))
	}
	return strings.Join(lines, "; ")
}
