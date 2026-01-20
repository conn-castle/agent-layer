package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/nicholasjconn/agent-layer/internal/install"
	alsync "github.com/nicholasjconn/agent-layer/internal/sync"
	"github.com/nicholasjconn/agent-layer/internal/wizard"
)

var runWizard = func(root string) error {
	return wizard.Run(root, wizard.NewHuhUI(), alsync.Run)
}

func newInstallCmd() *cobra.Command {
	var overwrite bool
	var noWizard bool

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Initialize Agent Layer in this repository",
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := getwd()
			if err != nil {
				return err
			}
			if err := install.Run(root, install.Options{Overwrite: overwrite}); err != nil {
				return err
			}
			if noWizard || !isTerminal() {
				return nil
			}
			run, err := promptYesNo(cmd.InOrStdin(), cmd.OutOrStdout(), "Run setup wizard now? (recommended)", true)
			if err != nil {
				return err
			}
			if !run {
				return nil
			}
			return runWizard(root)
		},
	}

	cmd.Flags().BoolVar(&overwrite, "overwrite", false, "Overwrite existing template files")
	cmd.Flags().BoolVar(&noWizard, "no-wizard", false, "Skip prompting to run the setup wizard after install")

	return cmd
}

// promptYesNo asks a yes/no question and returns the user's choice or an error.
// defaultYes controls the result when the user provides an empty response.
func promptYesNo(in io.Reader, out io.Writer, prompt string, defaultYes bool) (bool, error) {
	reader := bufio.NewReader(in)
	for {
		if defaultYes {
			if _, err := fmt.Fprintf(out, "%s [Y/n]: ", prompt); err != nil {
				return false, err
			}
		} else {
			if _, err := fmt.Fprintf(out, "%s [y/N]: ", prompt); err != nil {
				return false, err
			}
		}
		line, err := reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return false, err
		}
		response := strings.TrimSpace(line)
		if response == "" {
			if errors.Is(err, io.EOF) {
				return false, nil
			}
			if err == nil {
				return defaultYes, nil
			}
		}
		switch strings.ToLower(response) {
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		}
		if errors.Is(err, io.EOF) {
			return false, fmt.Errorf("invalid response %q", response)
		}
		if _, err := fmt.Fprintln(out, "Please enter y or n."); err != nil {
			return false, err
		}
	}
}
