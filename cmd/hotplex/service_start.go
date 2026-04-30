package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/hrygo/hotplex/internal/cli/output"
	"github.com/hrygo/hotplex/internal/service"
	"github.com/hrygo/hotplex/internal/worker/proc"
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

// newServiceStopCmd stops the system service. Falls back to PID-based stop
// when the service manager reports the service as not installed.
func newServiceStopCmd() *cobra.Command {
	var levelStr string

	cmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop system service",
		RunE: func(cmd *cobra.Command, args []string) error {
			level, err := service.ParseLevel(levelStr)
			if err != nil {
				return err
			}

			mgr := service.NewManager()
			if err := mgr.Stop("hotplex", level); err != nil {
				// Fallback: try PID-based stop
				if pid, pidErr := readGatewayPID(); pidErr == nil {
					if termErr := proc.GracefulTerminate(pid); termErr != nil {
						return fmt.Errorf("service stop failed: %w; PID stop also failed: %w", err, termErr)
					}
					removeGatewayPID()
					fmt.Fprintf(os.Stderr, "  %s Stopped via PID %d (service stop unavailable)\n", output.StatusSymbol("pass"), pid)
					return nil
				}
				return err
			}

			fmt.Fprintf(os.Stderr, "  %s Stopped service %s\n", output.StatusSymbol("pass"), output.Dim(fmt.Sprintf("(%s)", level)))
			return nil
		},
	}
	cmd.Flags().StringVar(&levelStr, "level", "user", "service level: user or system")
	return cmd
}

func newServiceRestartCmd() *cobra.Command {
	return newServiceActionCmd("restart", "Restart system service", "restarted",
		func(m service.Manager, n string, l service.Level) error { return m.Restart(n, l) })
}
