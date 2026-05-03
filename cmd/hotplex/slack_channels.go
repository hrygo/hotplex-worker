package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	slackcli "github.com/hrygo/hotplex/internal/cli/slack"
)

func newSlackListChannelsCmd() *cobra.Command {
	var types string
	var limit int
	var configPath string
	var useJSON bool

	cmd := &cobra.Command{
		Use:   "list-channels",
		Short: "List channels and DMs",
		Long:  `List Slack channels, DMs, and group DMs.`,
		Example: `  hotplex slack list-channels --types im
  hotplex slack list-channels --types im,public_channel,private_channel --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, client, err := slackcli.LoadConfigAndClient(configPath)
			if err != nil {
				return err
			}

			channels, err := slackcli.ListChannels(cmd.Context(), client, types, limit)
			if err != nil {
				return err
			}

			if useJSON {
				return json.NewEncoder(os.Stdout).Encode(channels)
			}

			if len(channels) == 0 {
				fmt.Fprintln(os.Stderr, "No channels found.")
				return nil
			}

			fmt.Printf("%-20s %-30s %-10s\n", "ID", "NAME", "TYPE")
			for _, ch := range channels {
				typ := "Channel"
				if ch.IsIM {
					typ = "DM"
				} else if ch.IsGroup {
					typ = "Group"
				}
				fmt.Printf("%-20s %-30s %-10s\n", ch.ID, ch.Name, typ)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&types, "types", "im", "channel types (comma-separated: im,public_channel,private_channel)")
	cmd.Flags().IntVarP(&limit, "limit", "n", 100, "max results")
	cmd.Flags().BoolVar(&useJSON, "json", false, "JSON output")
	configFlag(cmd, &configPath)

	return cmd
}
