package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/conn-castle/agent-layer/internal/dispatch"
	"github.com/conn-castle/agent-layer/internal/doctor"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/warnings"
)

var (
	checkInstructions = warnings.CheckInstructions
	checkMCPServers   = warnings.CheckMCPServers
)

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   messages.DoctorUse,
		Short: messages.DoctorShort,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveRepoRoot()
			if err != nil {
				return err
			}

			fmt.Printf(messages.DoctorHealthCheckFmt, root)

			var allResults []doctor.Result

			// 1. Check Structure
			allResults = append(allResults, doctor.CheckStructure(root)...)

			// 2. Check Config
			configResults, cfg := doctor.CheckConfig(root)
			allResults = append(allResults, configResults...)

			updateResult := doctor.Result{CheckName: messages.DoctorCheckNameUpdate}
			if strings.TrimSpace(os.Getenv(dispatch.EnvNoNetwork)) != "" {
				updateResult.Status = doctor.StatusWarn
				updateResult.Message = fmt.Sprintf(messages.DoctorUpdateSkippedFmt, dispatch.EnvNoNetwork)
				updateResult.Recommendation = fmt.Sprintf(messages.DoctorUpdateSkippedRecommendFmt, dispatch.EnvNoNetwork)
			} else {
				result, err := checkForUpdate(cmd.Context(), Version)
				if err != nil {
					updateResult.Status = doctor.StatusWarn
					updateResult.Message = fmt.Sprintf(messages.DoctorUpdateFailedFmt, err)
					updateResult.Recommendation = messages.DoctorUpdateFailedRecommend
				} else if result.CurrentIsDev {
					updateResult.Status = doctor.StatusWarn
					updateResult.Message = fmt.Sprintf(messages.DoctorUpdateDevBuildFmt, result.Latest)
					updateResult.Recommendation = messages.DoctorUpdateDevBuildRecommend
				} else if result.Outdated {
					updateResult.Status = doctor.StatusWarn
					updateResult.Message = fmt.Sprintf(messages.DoctorUpdateAvailableFmt, result.Latest, result.Current)
					updateResult.Recommendation = messages.DoctorUpdateAvailableRecommend
				} else {
					updateResult.Status = doctor.StatusOK
					updateResult.Message = fmt.Sprintf(messages.DoctorUpToDateFmt, result.Current)
				}
			}
			allResults = append(allResults, updateResult)

			if cfg != nil {
				// 3. Check Secrets
				allResults = append(allResults, doctor.CheckSecrets(cfg)...)

				// 4. Check Agents
				allResults = append(allResults, doctor.CheckAgents(cfg)...)
			}

			hasFail := false
			for _, r := range allResults {
				printResult(r)
				if r.Status == doctor.StatusFail {
					hasFail = true
				}
			}

			// 5. Run Warning System (Instructions + MCP)
			// Only run if basic config loaded successfully, otherwise we might crash or be useless.
			var warningList []warnings.Warning
			if cfg != nil {
				fmt.Println(messages.DoctorWarningSystemHeader)

				// Instructions check
				instWarnings, err := checkInstructions(root, cfg.Config.Warnings.InstructionTokenThreshold)
				if err != nil {
					color.Red(messages.DoctorInstructionsCheckFailedFmt, err)
					hasFail = true
				} else {
					warningList = append(warningList, instWarnings...)
				}

				// MCP check (Doctor runs discovery)
				mcpWarnings, err := checkMCPServers(context.Background(), cfg, nil)
				if err != nil {
					color.Red(messages.DoctorMCPCheckFailedFmt, err)
					hasFail = true
				} else {
					warningList = append(warningList, mcpWarnings...)
				}
			}

			if len(warningList) > 0 {
				fmt.Println()
				for _, w := range warningList {
					fmt.Println(w.String())
					fmt.Println() // Spacer
				}
				hasFail = true // Warnings cause exit 1 per spec
			}

			fmt.Println()
			if hasFail {
				color.Red(messages.DoctorFailureSummary)
				return fmt.Errorf(messages.DoctorFailureError)
			} else {
				color.Green(messages.DoctorSuccessSummary)
			}

			return nil
		},
	}
}

func printResult(r doctor.Result) {
	var status string
	switch r.Status {
	case doctor.StatusOK:
		status = color.GreenString(messages.DoctorStatusOKLabel)
	case doctor.StatusWarn:
		status = color.YellowString(messages.DoctorStatusWarnLabel)
	case doctor.StatusFail:
		status = color.RedString(messages.DoctorStatusFailLabel)
	}

	fmt.Printf(messages.DoctorResultLineFmt, status, r.CheckName, r.Message)
	if r.Recommendation != "" {
		fmt.Printf(messages.DoctorRecommendationFmt, r.Recommendation)
	}
}
