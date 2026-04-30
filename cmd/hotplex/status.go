package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/hrygo/hotplex/internal/config"
)

func newStatusCmd() *cobra.Command {
	var configPath string
	var format string

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Check gateway status",
		Long: "Check if the gateway is running by inspecting PID file and platform service manager, then pinging the health endpoint.\n" +
			"Exit code 0 = running, 1 = not running.",
		Example: `  hotplex status                     # Check if gateway is running
  hotplex status --format json        # JSON output`,
		RunE: func(cmd *cobra.Command, args []string) error {
			inst, findErr := findRunningGateway()

			type statusInfo struct {
				Running bool   `json:"running"`
				PID     int    `json:"pid,omitempty"`
				Source  string `json:"source,omitempty"`
				Health  string `json:"health,omitempty"`
				Error   string `json:"error,omitempty"`
			}

			info := statusInfo{}

			if findErr != nil {
				info.Error = findErr.Error()
				if format == "json" {
					enc := json.NewEncoder(os.Stdout)
					enc.SetIndent("", "  ")
					_ = enc.Encode(info)
				} else {
					fmt.Fprintf(os.Stderr, "gateway: %s\n", findErr.Error())
				}
				return fmt.Errorf("gateway: %w", findErr)
			}

			info.PID = inst.PID
			info.Source = string(inst.Source)
			info.Running = true

			addr := gatewayAddrFromConfig(configPath)
			client := &http.Client{Timeout: 3 * time.Second}
			resp, err := client.Get("http://" + addr + "/health")
			if err != nil {
				info.Health = "unreachable: " + err.Error()
			} else {
				_ = resp.Body.Close()
				info.Health = resp.Status
			}

			if format == "json" {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				_ = enc.Encode(info)
			} else {
				switch inst.Source {
				case sourcePID:
					fmt.Fprintf(os.Stderr, "gateway: running (PID %d)\n", inst.PID)
				case sourceService:
					fmt.Fprintf(os.Stderr, "gateway: running as service (%s, PID %d)\n", inst.Level, inst.PID)
				}
				fmt.Fprintf(os.Stderr, "  health: http://%s/health → %s\n", addr, info.Health)
			}
			return nil
		},
	}
	configFlag(cmd, &configPath)
	cmd.Flags().StringVar(&format, "format", "text", "output format (text, json)")
	return cmd
}

func gatewayAddrFromConfig(configPath string) string {
	absPath, err := config.ExpandAndAbs(configPath)
	if err != nil {
		return "localhost:8888"
	}
	loadEnvFile(filepath.Dir(absPath))
	cfg, err := config.Load(absPath, config.LoadOptions{})
	if err != nil {
		return "localhost:8888"
	}
	addr := cfg.Gateway.Addr
	if addr != "" && addr[0] == ':' {
		return "localhost" + addr
	}
	return addr
}
