package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/nicholasjconn/agent-layer/internal/sync"
)

// ErrSyncCompletedWithWarnings is returned when sync completes but warnings were generated.
var ErrSyncCompletedWithWarnings = errors.New("sync completed with warnings")

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
				return ErrSyncCompletedWithWarnings
			}
			return nil
		},
	}

	return cmd
}
