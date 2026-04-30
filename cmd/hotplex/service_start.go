package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/hrygo/hotplex/internal/cli/output"
	"github.com/hrygo/hotplex/internal/service"
)

func newServiceActionCmd(use, short, verb string, action func(service.Manager, string, service.Level) error) *cobra.Command {
	var levelStr string

	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		RunE: func(cmd *cobra.Command, args []string) error {
			level, err := service.ParseLevel(levelStr)
			if err != nil {
				return err
			}

			if err := action(service.NewManager(), "hotplex", level); err != nil {
				return err
			}

			fmt.Fprintf(os.Stderr, "  %s Service %s %s\n", output.StatusSymbol("pass"), verb, output.Dim(fmt.Sprintf("(%s)", level)))
			return nil
		},
	}

	cmd.Flags().StringVar(&levelStr, "level", "user", "service level: user or system")

	return cmd
}

func newServiceStartCmd() *cobra.Command {
	return newServiceActionCmd("start", "Start system service", "started",
		func(m service.Manager, n string, l service.Level) error { return m.Start(n, l) })
}

func newServiceStopCmd() *cobra.Command {
	return newServiceActionCmd("stop", "Stop system service", "stopped",
		func(m service.Manager, n string, l service.Level) error { return m.Stop(n, l) })
}

func newServiceRestartCmd() *cobra.Command {
	return newServiceActionCmd("restart", "Restart system service", "restarted",
		func(m service.Manager, n string, l service.Level) error { return m.Restart(n, l) })
}
