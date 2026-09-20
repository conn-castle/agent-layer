package main

import (
	"github.com/spf13/cobra"

	"github.com/conn-castle/agent-layer/internal/clients/muse"
	"github.com/conn-castle/agent-layer/internal/config"
)

func newMuseCmd() *cobra.Command {
	return newNoSyncLaunchCmd("muse [args...]", "Launch Muse Code for this repository", "muse", func(cfg *config.Config) *bool { return cfg.Agents.Muse.Enabled }, muse.Launch)
}
