package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/nicholasjconn/agent-layer/internal/sync"
)

func newSyncCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Regenerate client outputs from .agent-layer",
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := getwd()
			if err != nil {
				return err
			}
			warnings, err := sync.Run(root)
			if err != nil {
				return err
			}
			if len(warnings) > 0 {
				for _, w := range warnings {
					fmt.Fprintln(os.Stderr, w.String())
				}
				return fmt.Errorf("sync completed with warnings")
			}
			return nil
		},
	}

	return cmd
}
