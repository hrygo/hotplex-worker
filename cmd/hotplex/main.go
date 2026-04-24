package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	_ "github.com/hrygo/hotplex/internal/worker/claudecode"
	_ "github.com/hrygo/hotplex/internal/worker/opencodeserver"
	_ "github.com/hrygo/hotplex/internal/worker/pi"
	"github.com/hrygo/hotplex/pkg/aep"
)

var (
	version   = "v1.0.0"
	buildTime = "unknown"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "hotplex",
		Short: "HotPlex Worker Gateway",
		Long: `HotPlex Worker Gateway — unified access layer for AI Coding Agent sessions.

WebSocket gateway abstracting Claude Code, OpenCode Server, and Pi-mono protocol differences.
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
