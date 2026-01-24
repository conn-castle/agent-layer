package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/conn-castle/agent-layer/internal/messages"
)

func newStubCmd(name string) *cobra.Command {
	return &cobra.Command{
		Use:   name,
		Short: fmt.Sprintf(messages.StubShortFmt, name),
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf(messages.StubNotImplementedFmt, name)
		},
	}
}
