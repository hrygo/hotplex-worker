package main

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("hotplex %s\nBuild: %s\nGo: %s\nOS/Arch: %s/%s\n",
				versionString(), buildTime, runtime.Version(), runtime.GOOS, runtime.GOARCH)
		},
	}
}
