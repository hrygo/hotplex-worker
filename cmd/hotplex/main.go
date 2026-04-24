package main

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/hotplex/hotplex-worker/pkg/aep"
)

var (
	version   = "v1.0.0"
	buildTime = "unknown"
)

func main() {
	rootCmd := &cobra.Command{
		Use:           "hotplex",
		Short:         "HotPlex Worker Gateway",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	rootCmd.AddCommand(
		newGatewayCmd(),
		newDoctorCmd(),
		newSecurityCmd(),
		newOnboardCmd(),
		newVersionCmd(),
	)
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func versionString() string { return version }

func newSessionID() string {
	return aep.NewSessionID()
}
