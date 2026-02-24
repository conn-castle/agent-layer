package main

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/dispatch"
	"github.com/conn-castle/agent-layer/internal/doctor"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/update"
	"github.com/conn-castle/agent-layer/internal/warnings"
)

var (
	checkInstructions = warnings.CheckInstructions
	checkMCPServers   = warnings.CheckMCPServers
	checkPolicy       = warnings.CheckPolicy
)

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   messages.DoctorUse,
		Short: messages.DoctorShort,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			root, err := resolveRepoRoot()
			if err != nil {
				return err
			}

			_, _ = fmt.Fprintf(out, messages.DoctorHealthCheckFmt, root)

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
				switch {
				case err != nil && update.IsRateLimitError(err):
					updateResult.Status = doctor.StatusWarn
					updateResult.Message = messages.DoctorUpdateRateLimited
				case err != nil:
					updateResult.Status = doctor.StatusWarn
					updateResult.Message = fmt.Sprintf(messages.DoctorUpdateFailedFmt, err)
					updateResult.Recommendation = messages.DoctorUpdateFailedRecommend
				case result.CurrentIsDev:
					updateResult.Status = doctor.StatusWarn
					updateResult.Message = fmt.Sprintf(messages.DoctorUpdateDevBuildFmt, result.Latest)
					updateResult.Recommendation = messages.DoctorUpdateDevBuildRecommend
				case result.Outdated:
					updateResult.Status = doctor.StatusWarn
					updateResult.Message = fmt.Sprintf(messages.DoctorUpdateAvailableFmt, result.Latest, result.Current)
					updateResult.Recommendation = messages.DoctorUpdateAvailableRecommend
				default:
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
				printResult(out, r)
				if r.Status == doctor.StatusFail {
					hasFail = true
				}
			}

			// 5. Run Warning System (Instructions + MCP)
			// Only run if basic config loaded successfully, otherwise we might crash or be useless.
			var warningList []warnings.Warning
			if cfg != nil {
				_, _ = fmt.Fprintln(out, messages.DoctorWarningSystemHeader)

				// Instructions check
				instWarnings, err := checkInstructions(root, cfg.Config.Warnings.InstructionTokenThreshold)
				if err != nil {
					_, _ = fmt.Fprintln(out, color.RedString(messages.DoctorInstructionsCheckFailedFmt, err))
					hasFail = true
				} else {
					warningList = append(warningList, instWarnings...)
				}

				// MCP check (Doctor runs discovery)
				enabledServerIDs := enabledMCPServerIDs(cfg.Config.MCP.Servers)
				reportProgress, stopProgress := startMCPDiscoveryReporter(enabledServerIDs, out)
				mcpWarnings, err := checkMCPServers(cmd.Context(), cfg, nil, reportProgress)
				stopProgress()
				if err != nil {
					_, _ = fmt.Fprintln(out, color.RedString(messages.DoctorMCPCheckFailedFmt, err))
					hasFail = true
				} else {
					warningList = append(warningList, mcpWarnings...)
				}

				warningList = append(warningList, checkPolicy(cfg)...)
				noiseMode := cfg.Config.Warnings.NoiseMode
				if strings.EqualFold(strings.TrimSpace(noiseMode), warnings.NoiseModeQuiet) {
					noiseMode = warnings.NoiseModeDefault
				}
				warningList = warnings.ApplyNoiseControl(warningList, noiseMode)
			}

			if len(warningList) > 0 {
				for _, w := range warningList {
					_, _ = fmt.Fprintln(out, w.String())
					_, _ = fmt.Fprintln(out) // Spacer
				}
				hasFail = true // Warnings cause exit 1 per spec
				_, _ = fmt.Fprintln(out)
			}

			if hasFail {
				_, _ = fmt.Fprintln(out, color.RedString(messages.DoctorFailureSummary))
				return fmt.Errorf(messages.DoctorFailureError)
			} else {
				_, _ = fmt.Fprintln(out, color.GreenString(messages.DoctorSuccessSummary))
			}

			return nil
		},
	}
}

func printResult(out io.Writer, r doctor.Result) {
	var status string
	switch r.Status {
	case doctor.StatusOK:
		status = color.GreenString(messages.DoctorStatusOKLabel)
	case doctor.StatusWarn:
		status = color.YellowString(messages.DoctorStatusWarnLabel)
	case doctor.StatusFail:
		status = color.RedString(messages.DoctorStatusFailLabel)
	}

	_, _ = fmt.Fprintf(out, messages.DoctorResultLineFmt, status, r.CheckName, r.Message)
	if r.Recommendation != "" {
		printRecommendation(out, r.Recommendation)
	}
}

// printRecommendation renders a multi-line recommendation with consistent indentation.
func printRecommendation(out io.Writer, recommendation string) {
	lines := strings.Split(recommendation, "\n")
	for i, line := range lines {
		if i == 0 {
			_, _ = fmt.Fprintf(out, "%s%s\n", messages.DoctorRecommendationPrefix, line)
			continue
		}
		if line == "" {
			_, _ = fmt.Fprintf(out, "%s\n", messages.DoctorRecommendationIndent)
			continue
		}
		_, _ = fmt.Fprintf(out, "%s%s\n", messages.DoctorRecommendationIndent, line)
	}
}

func countEnabledMCPServers(servers []config.MCPServer) int {
	return len(enabledMCPServerIDs(servers))
}

// enabledMCPServerIDs returns the configured IDs for enabled MCP servers.
func enabledMCPServerIDs(servers []config.MCPServer) []string {
	ids := make([]string, 0, len(servers))
	for _, server := range servers {
		if server.Enabled != nil && *server.Enabled {
			ids = append(ids, server.ID)
		}
	}
	return ids
}

// startMCPDiscoveryReporter prints progress events from MCP discovery and returns a reporter + stop function.
// serverIDs is the ordered list of enabled MCP server IDs; returns a nil reporter when none are enabled.
func startMCPDiscoveryReporter(serverIDs []string, out io.Writer) (warnings.MCPDiscoveryStatusFunc, func()) {
	_, _ = fmt.Fprintf(out, messages.DoctorMCPCheckStartFmt, len(serverIDs))
	if len(serverIDs) == 0 {
		_, _ = fmt.Fprintln(out, messages.DoctorMCPCheckDone)
		return nil, func() {}
	}

	_, _ = fmt.Fprintln(out)
	reporter := newMCPDiscoveryReporter(serverIDs, isTerminal(), out)
	reporter.start()

	var once sync.Once
	stop := func() {
		once.Do(func() {
			reporter.stop()
			_, _ = fmt.Fprintln(out, messages.DoctorMCPCheckDone)
		})
	}

	return reporter.report, stop
}

// mcpDiscoveryReporter serializes and renders MCP discovery progress updates.
type mcpDiscoveryReporter struct {
	out           io.Writer
	mu            sync.RWMutex
	ids           []string
	useSpinner    bool
	events        chan warnings.MCPDiscoveryEvent
	stopCh        chan struct{}
	doneCh        chan struct{}
	statuses      map[string]warnings.MCPDiscoveryStatus
	errors        map[string]error
	spinnerFrames []string
	spinnerIndex  int
	rendered      bool
}

// newMCPDiscoveryReporter constructs a progress reporter for MCP discovery output.
func newMCPDiscoveryReporter(serverIDs []string, useSpinner bool, out io.Writer) *mcpDiscoveryReporter {
	ids := append([]string(nil), serverIDs...)
	return &mcpDiscoveryReporter{
		out:           out,
		ids:           ids,
		useSpinner:    useSpinner,
		events:        make(chan warnings.MCPDiscoveryEvent, len(ids)*2),
		stopCh:        make(chan struct{}),
		doneCh:        make(chan struct{}),
		statuses:      make(map[string]warnings.MCPDiscoveryStatus, len(ids)),
		errors:        make(map[string]error, len(ids)),
		spinnerFrames: []string{"|", "/", "-", "\\"},
	}
}

func (r *mcpDiscoveryReporter) start() {
	if r.useSpinner {
		r.render(false)
	} else {
		for _, id := range r.ids {
			_, _ = fmt.Fprintln(r.out, r.formatLine(id))
		}
	}

	go r.run()
}

func (r *mcpDiscoveryReporter) report(event warnings.MCPDiscoveryEvent) {
	select {
	case <-r.doneCh:
		return
	default:
	}

	select {
	case r.events <- event:
	case <-r.doneCh:
	}
}

func (r *mcpDiscoveryReporter) stop() {
	close(r.stopCh)
	<-r.doneCh
}

func (r *mcpDiscoveryReporter) run() {
	var ticker *time.Ticker
	var tickCh <-chan time.Time
	if r.useSpinner {
		ticker = time.NewTicker(120 * time.Millisecond)
		tickCh = ticker.C
	}
	if ticker != nil {
		defer ticker.Stop()
	}

	for {
		select {
		case event := <-r.events:
			r.applyEvent(event)
			if r.useSpinner {
				r.render(true)
				continue
			}
			if event.Status != warnings.MCPDiscoveryStatusStart {
				_, _ = fmt.Fprintln(r.out, formatMCPDiscoveryEvent(event))
			}
		case <-tickCh:
			r.advanceSpinner()
			r.render(true)
		case <-r.stopCh:
			r.drainEvents()
			r.finalizePending()
			if r.useSpinner {
				r.render(true)
			}
			close(r.doneCh)
			return
		}
	}
}

func (r *mcpDiscoveryReporter) applyEvent(event warnings.MCPDiscoveryEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.statuses[event.ServerID] = event.Status
	if event.Err != nil {
		r.errors[event.ServerID] = event.Err
	} else {
		delete(r.errors, event.ServerID)
	}
}

func (r *mcpDiscoveryReporter) drainEvents() {
	for {
		select {
		case event := <-r.events:
			r.applyEvent(event)
			if !r.useSpinner && event.Status != warnings.MCPDiscoveryStatusStart {
				_, _ = fmt.Fprintln(r.out, formatMCPDiscoveryEvent(event))
			}
		default:
			return
		}
	}
}

func (r *mcpDiscoveryReporter) finalizePending() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, id := range r.ids {
		status, ok := r.statuses[id]
		if !ok || status == warnings.MCPDiscoveryStatusStart {
			r.statuses[id] = warnings.MCPDiscoveryStatusDone
		}
	}
}

func (r *mcpDiscoveryReporter) advanceSpinner() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.spinnerFrames) == 0 {
		return
	}
	r.spinnerIndex = (r.spinnerIndex + 1) % len(r.spinnerFrames)
}

func (r *mcpDiscoveryReporter) render(moveCursor bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.useSpinner || len(r.ids) == 0 {
		return
	}
	if r.rendered && moveCursor {
		_, _ = fmt.Fprintf(r.out, "\x1b[%dA", len(r.ids))
	}
	for _, id := range r.ids {
		_, _ = fmt.Fprint(r.out, "\r\x1b[2K")
		_, _ = fmt.Fprintln(r.out, r.formatLineLocked(id))
	}
	r.rendered = true
}

func (r *mcpDiscoveryReporter) formatLine(serverID string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.formatLineLocked(serverID)
}

func (r *mcpDiscoveryReporter) formatLineLocked(serverID string) string {
	switch r.statusForLocked(serverID) {
	case warnings.MCPDiscoveryStatusDone:
		return fmt.Sprintf("  - %s: done", serverID)
	case warnings.MCPDiscoveryStatusError:
		if err := r.errors[serverID]; err != nil {
			return fmt.Sprintf("  - %s: error (%v)", serverID, err)
		}
		return fmt.Sprintf("  - %s: error", serverID)
	default:
		if r.useSpinner {
			return fmt.Sprintf("  - %s: %s working", serverID, r.spinnerFrames[r.spinnerIndex])
		}
		return fmt.Sprintf("  - %s: starting", serverID)
	}
}

func (r *mcpDiscoveryReporter) statusFor(serverID string) warnings.MCPDiscoveryStatus {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.statusForLocked(serverID)
}

func (r *mcpDiscoveryReporter) statusForLocked(serverID string) warnings.MCPDiscoveryStatus {
	if status, ok := r.statuses[serverID]; ok {
		return status
	}
	return warnings.MCPDiscoveryStatusStart
}

// formatMCPDiscoveryEvent renders a discovery event as a human-readable string.
func formatMCPDiscoveryEvent(event warnings.MCPDiscoveryEvent) string {
	switch event.Status {
	case warnings.MCPDiscoveryStatusStart:
		return fmt.Sprintf("  - %s: starting", event.ServerID)
	case warnings.MCPDiscoveryStatusDone:
		return fmt.Sprintf("  - %s: done", event.ServerID)
	case warnings.MCPDiscoveryStatusError:
		if event.Err != nil {
			return fmt.Sprintf("  - %s: error (%v)", event.ServerID, event.Err)
		}
		return fmt.Sprintf("  - %s: error", event.ServerID)
	default:
		return fmt.Sprintf("  - %s: %s", event.ServerID, event.Status)
	}
}
