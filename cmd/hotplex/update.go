package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/hrygo/hotplex/internal/cli/output"
	"github.com/hrygo/hotplex/internal/service"
	"github.com/hrygo/hotplex/internal/updater"
	"github.com/hrygo/hotplex/internal/worker/proc"
)

func newUpdateCmd() *cobra.Command {
	var checkOnly, yes, restart bool

	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update hotplex to the latest version",
		Long: `Check for updates and install the latest version of hotplex.

Downloads the binary from GitHub Releases, verifies the sha256 checksum,
and atomically replaces the running binary.

Supports all platforms: linux/amd64, linux/arm64, darwin/amd64, darwin/arm64,
windows/amd64, windows/arm64.`,
		Example: `  hotplex update              # Interactive update
  hotplex update --check      # Only check, don't download
  hotplex update -y           # Skip confirmation prompt
  hotplex update --restart    # Restart service after update`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), 60*time.Second)
			defer cancel()

			u := updater.New(versionString())

			fmt.Fprintf(os.Stderr, "  Current: %s\n", versionString())

			if updater.IsDocker() {
				fmt.Fprintf(os.Stderr, "  %s Running inside Docker — updates will be lost on container restart\n",
					output.StatusSymbol("warn"))
			}

			result, err := u.Check(ctx)
			if err != nil {
				return err
			}

			if !result.UpdateAvailable {
				fmt.Fprintf(os.Stderr, "  Already up-to-date (%s)\n", result.LatestVersion)
				return nil
			}

			fmt.Fprintf(os.Stderr, "  Update available: %s → %s\n",
				result.CurrentVersion, result.LatestVersion)

			if checkOnly {
				fmt.Fprintf(os.Stderr, "  Use %s to install\n", output.Bold("hotplex update"))
				return nil
			}

			if !yes {
				fmt.Fprintf(os.Stderr, "  Do you want to update to %s? [y/N] ", result.LatestVersion)
				reader := bufio.NewReader(os.Stdin)
				input, _ := reader.ReadString('\n')
				if input != "y\n" && input != "Y\n" && input != "yes\n" && input != "YES\n" {
					fmt.Fprintf(os.Stderr, "  Update cancelled\n")
					return nil
				}
			}

			// Check write permission before downloading.
			if _, err := updater.IsWritable(); err != nil {
				return err
			}

			// Download.
			fmt.Fprintf(os.Stderr, "  Downloading %s ...\n", result.AssetName)
			tmpPath, err := u.Download(ctx, result.DownloadURL)
			if err != nil {
				return err
			}
			defer func() { _ = os.Remove(tmpPath) }()

			// Verify checksum.
			fmt.Fprintf(os.Stderr, "  Verifying checksum...\n")
			if err := u.VerifyChecksum(ctx, result.ChecksumURL, result.AssetName, tmpPath); err != nil {
				return fmt.Errorf("checksum verification failed: %w", err)
			}

			// Detect running gateway before replacing.
			gatewayInst, gatewayErr := findRunningGateway()

			// Replace binary.
			fmt.Fprintf(os.Stderr, "  Installing...\n")
			if err := u.Replace(tmpPath); err != nil {
				return err
			}

			fmt.Fprintf(os.Stderr, "  %s Updated to %s\n",
				output.StatusSymbol("pass"), result.LatestVersion)

			// Handle service restart.
			if gatewayErr != nil {
				return nil
			}

			shouldRestart := restart || yes
			if !shouldRestart {
				fmt.Fprintf(os.Stderr, "  Gateway is running (PID %d). Restart now? [y/N] ", gatewayInst.PID)
				reader := bufio.NewReader(os.Stdin)
				input, _ := reader.ReadString('\n')
				shouldRestart = input == "y\n" || input == "Y\n"
			}

			if !shouldRestart {
				fmt.Fprintf(os.Stderr, "  Gateway will use the new binary on next restart\n")
				return nil
			}

			return restartAfterUpdate(gatewayInst)
		},
	}
	cmd.Flags().BoolVar(&checkOnly, "check", false, "only check for updates, do not download")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip confirmation prompt")
	cmd.Flags().BoolVar(&restart, "restart", false, "restart service after successful update")
	return cmd
}

// restartAfterUpdate stops and restarts the gateway after a binary update.
func restartAfterUpdate(inst *gatewayInstance) error {
	if err := stopGateway(inst); err != nil {
		return fmt.Errorf("stop gateway: %w", err)
	}
	fmt.Fprintf(os.Stderr, "  Stopped gateway (PID %d)\n", inst.PID)

	// Wait for process to exit.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if inst.Source == sourcePID {
			if err := proc.IsProcessAlive(inst.PID); err != nil {
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Restart using the appropriate mechanism.
	switch inst.Source {
	case sourceService:
		mgr := service.NewManager()
		if err := mgr.Start("hotplex", inst.Level); err != nil {
			return fmt.Errorf("start service: %w", err)
		}
		fmt.Fprintf(os.Stderr, "  %s Service restarted\n", output.StatusSymbol("pass"))
	case sourcePID:
		if err := startDaemon("", false); err != nil {
			return fmt.Errorf("start daemon: %w", err)
		}
		fmt.Fprintf(os.Stderr, "  %s Gateway restarted\n", output.StatusSymbol("pass"))
	}
	return nil
}
