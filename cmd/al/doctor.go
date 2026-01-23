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
	"github.com/conn-castle/agent-layer/internal/warnings"
)

var (
	checkInstructions = warnings.CheckInstructions
	checkMCPServers   = warnings.CheckMCPServers
)

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Report missing secrets, disabled servers, and common misconfigurations",
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveRepoRoot()
			if err != nil {
				return err
			}

			fmt.Printf("🏥 Checking Agent Layer health in %s...\n\n", root)

			var allResults []doctor.Result

			// 1. Check Structure
			allResults = append(allResults, doctor.CheckStructure(root)...)

			// 2. Check Config
			configResults, cfg := doctor.CheckConfig(root)
			allResults = append(allResults, configResults...)

			updateResult := doctor.Result{CheckName: "Update"}
			if strings.TrimSpace(os.Getenv(dispatch.EnvNoNetwork)) != "" {
				updateResult.Status = doctor.StatusWarn
				updateResult.Message = fmt.Sprintf("Update check skipped because %s is set", dispatch.EnvNoNetwork)
				updateResult.Recommendation = fmt.Sprintf("Unset %s to check for updates.", dispatch.EnvNoNetwork)
			} else {
				result, err := checkForUpdate(context.Background(), Version)
				if err != nil {
					updateResult.Status = doctor.StatusWarn
					updateResult.Message = fmt.Sprintf("Failed to check for updates: %v", err)
					updateResult.Recommendation = "Verify network access and try again."
				} else if result.CurrentIsDev {
					updateResult.Status = doctor.StatusWarn
					updateResult.Message = fmt.Sprintf("Running dev build; latest release is %s", result.Latest)
					updateResult.Recommendation = "Install a release build to use version pinning and dispatch."
				} else if result.Outdated {
					updateResult.Status = doctor.StatusWarn
					updateResult.Message = fmt.Sprintf("Update available: %s (current %s)", result.Latest, result.Current)
					updateResult.Recommendation = "Upgrade the global CLI or update your repo pin if needed."
				} else {
					updateResult.Status = doctor.StatusOK
					updateResult.Message = fmt.Sprintf("Agent Layer is up to date (%s)", result.Current)
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
				fmt.Println("\n🔍 Running warning system checks...")

				// Instructions check
				instWarnings, err := checkInstructions(root, cfg.Config.Warnings.InstructionTokenThreshold)
				if err != nil {
					color.Red("Failed to check instructions: %v", err)
					hasFail = true
				} else {
					warningList = append(warningList, instWarnings...)
				}

				// MCP check (Doctor runs discovery)
				mcpWarnings, err := checkMCPServers(context.Background(), cfg, nil)
				if err != nil {
					color.Red("Failed to check MCP servers: %v", err)
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
				color.Red("❌ Some checks failed or triggered warnings. Please address the issues above.")
				return fmt.Errorf("doctor checks failed")
			} else {
				color.Green("✅ All systems go! Agent Layer is ready.")
			}

			return nil
		},
	}
}

func printResult(r doctor.Result) {
	var status string
	switch r.Status {
	case doctor.StatusOK:
		status = color.GreenString("[OK]  ")
	case doctor.StatusWarn:
		status = color.YellowString("[WARN]")
	case doctor.StatusFail:
		status = color.RedString("[FAIL]")
	}

	fmt.Printf("%s %-10s %s\n", status, r.CheckName, r.Message)
	if r.Recommendation != "" {
		fmt.Printf("       💡 %s\n", r.Recommendation)
	}
}
