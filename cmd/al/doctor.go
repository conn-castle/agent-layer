package main

import (
	"context"
	"fmt"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/nicholasjconn/agent-layer/internal/doctor"
	"github.com/nicholasjconn/agent-layer/internal/warnings"
)

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Report missing secrets, disabled servers, and common misconfigurations",
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := getwd()
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
				instWarnings, err := warnings.CheckInstructions(root, cfg.Config.Warnings.InstructionTokenThreshold)
				if err != nil {
					color.Red("Failed to check instructions: %v", err)
					hasFail = true
				} else {
					warningList = append(warningList, instWarnings...)
				}

				// MCP check (Doctor runs discovery)
				mcpWarnings, err := warnings.CheckMCPServers(context.Background(), cfg, nil)
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
