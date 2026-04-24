package main

import (
	"encoding/json"
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

func newVersionCmd() *cobra.Command {
	var format string

	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Long: "Display build version, commit time, Go runtime, and platform information.\n" +
			"Supports text (default) and JSON output formats.",
		RunE: func(cmd *cobra.Command, args []string) error {
			info := struct {
				Version   string `json:"version"`
				BuildTime string `json:"build_time"`
				Go        string `json:"go"`
				OS        string `json:"os"`
				Arch      string `json:"arch"`
			}{
				Version:   versionString(),
				BuildTime: buildTime,
				Go:        runtime.Version(),
				OS:        runtime.GOOS,
				Arch:      runtime.GOARCH,
			}
			out := cmd.OutOrStdout()
			if format == "json" {
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				return enc.Encode(info)
			}
			_, _ = fmt.Fprintf(out, "hotplex %s\nBuild: %s\nGo: %s\nOS/Arch: %s/%s\n",
				info.Version, info.BuildTime, info.Go, info.OS, info.Arch) //nolint:errcheck // writing to cmd.OutOrStdout
			return nil
		},
	}
	cmd.Flags().StringVar(&format, "format", "text", "output format (text, json)")
	return cmd
}
