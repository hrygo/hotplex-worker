package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	_ "github.com/hrygo/hotplex/internal/worker/claudecode"
	_ "github.com/hrygo/hotplex/internal/worker/opencodeserver"
	"github.com/hrygo/hotplex/pkg/aep"
)

var (
	version   = "v1.5.4"
	buildTime = "unknown"
)

func main() {
	if isServiceRun() {
		runAsWindowsService(extractServiceConfig())
		return
	}

	rootCmd := &cobra.Command{
		Use:   "hotplex",
		Short: "HotPlex Worker Gateway",
		Long: `HotPlex Worker Gateway — unified access layer for AI Coding Agent sessions.

WebSocket gateway abstracting Claude Code and OpenCode Server protocol differences.
Connects users across Web, Slack, and Feishu through one optimized binary.

Quick start:
  hotplex dev                  # Start in development mode
  hotplex gateway start        # Start production gateway
  hotplex onboard              # Interactive setup wizard
  hotplex doctor               # Run diagnostic checks`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	rootCmd.AddCommand(
		newGatewayCmd(),
		newDoctorCmd(),
		newSecurityCmd(),
		newOnboardCmd(),
		newVersionCmd(),
		newDevCmd(),
		newConfigCmd(),
		newStatusCmd(),
		newServiceCmd(),
		newUpdateCmd(),
		newSlackCmd(),
	)
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func versionString() string { return version }

func newSessionID() string {
	return aep.NewSessionID()
}
