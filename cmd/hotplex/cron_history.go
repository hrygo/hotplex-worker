package main

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	croncli "github.com/hrygo/hotplex/internal/cli/cron"
	"github.com/hrygo/hotplex/internal/eventstore"
)

func newCronHistoryCmd() *cobra.Command {
	var (
		configPath string
		jsonOutput bool
	)
	cmd := &cobra.Command{
		Use:   "history <id|name>",
		Short: "Show execution history for a cron job",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withStoreAndEvents(context.Background(), configPath, func(store croncli.Store, evStore *eventstore.SQLiteStore) error {
				stats, err := croncli.QueryHistory(context.Background(), store, evStore, args[0])
				if err != nil {
					return err
				}

				if jsonOutput {
					return printJSON(stats)
				}

				fmt.Printf("Total turns: %d  Success: %d  Failed: %d\n",
					stats.TotalTurns, stats.SuccessTurns, stats.FailedTurns)
				fmt.Printf("Total duration: %s  Total cost: %s\n",
					croncli.FormatDurationMs(stats.TotalDurMs), croncli.FormatCost(stats.TotalCostUSD))
				fmt.Println()

				if len(stats.Turns) == 0 {
					return nil
				}

				tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
				_, _ = fmt.Fprintln(tw, "#\tSTATUS\tDURATION\tCOST\tMODEL\tTIME")
				for i, t := range stats.Turns {
					status := "ok"
					if !t.Success {
						status = "FAIL"
					}
					_, _ = fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\n",
						i+1, status,
						croncli.FormatDurationMs(t.DurationMs),
						croncli.FormatCost(t.CostUSD),
						t.Model,
						croncli.FormatTimeMs(t.CreatedAt),
					)
				}
				return tw.Flush()
			})
		},
	}
	configFlag(cmd, &configPath)
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	return cmd
}
