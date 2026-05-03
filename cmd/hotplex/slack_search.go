package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	slackcli "github.com/hrygo/hotplex/internal/cli/slack"
)

func newSlackSearchCmd() *cobra.Command {
	var query, searchType string
	var limit int
	var configPath string
	var useJSON bool

	cmd := &cobra.Command{
		Use:   "search",
		Short: "Search messages and files",
		Long:  `Search Slack for messages and files matching a query.`,
		Example: `  hotplex slack search --query "deploy failure" --type messages
  hotplex slack search -q "report" --type files --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, client, err := slackcli.LoadConfigAndClient(configPath)
			if err != nil {
				return err
			}

			results, err := slackcli.Search(cmd.Context(), client, query, searchType, limit)
			if err != nil {
				return err
			}

			if useJSON {
				return json.NewEncoder(os.Stdout).Encode(results)
			}

			if len(results) == 0 {
				fmt.Fprintln(os.Stderr, "No results found.")
				return nil
			}

			for _, r := range results {
				if r.Type == "message" {
					fmt.Printf("[MESSAGE] %s #%s %q\n", r.Timestamp, r.Channel, truncate(r.Text, 60))
				} else {
					fmt.Printf("[FILE]    %s (%d bytes)\n", r.Title, r.Size)
				}
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&query, "query", "q", "", "search query")
	cmd.Flags().StringVar(&searchType, "type", "all", "search scope: messages, files, all")
	cmd.Flags().IntVarP(&limit, "limit", "n", 20, "max results")
	cmd.Flags().BoolVar(&useJSON, "json", false, "JSON output")
	configFlag(cmd, &configPath)
	cmd.MarkFlagRequired("query")

	return cmd
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
