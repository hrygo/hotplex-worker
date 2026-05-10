package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	croncli "github.com/hrygo/hotplex/internal/cli/cron"
	"github.com/hrygo/hotplex/internal/eventstore"
)

func newCronCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cron",
		Short: "Cron job management",
		Long: `Manage cron jobs for the HotPlex gateway.

CRUD operations work directly on the local SQLite database.
Use --config to specify the gateway config file (default: ~/.hotplex/config.yaml).

Schedule format:
  cron:"*/5 * * * *"   Standard cron expression
  every:30m            Interval (1m minimum)
  at:2026-01-01T00:00:00Z  One-shot at ISO-8601 timestamp`,
	}
	cmd.AddCommand(
		newCronListCmd(),
		newCronGetCmd(),
		newCronCreateCmd(),
		newCronDeleteCmd(),
		newCronUpdateCmd(),
		newCronTriggerCmd(),
		newCronHistoryCmd(),
	)
	return cmd
}

// withStore opens the cron store, calls fn, and ensures cleanup.
func withStore(ctx context.Context, configPath string, fn func(croncli.Store) error) error {
	store, _, cleanup, err := croncli.OpenStore(ctx, configPath)
	if err != nil {
		return err
	}
	defer cleanup()
	return fn(store)
}

// withStoreAndEvents opens both stores, calls fn, and ensures cleanup.
func withStoreAndEvents(ctx context.Context, configPath string, fn func(croncli.Store, *eventstore.SQLiteStore) error) error {
	store, evStore, cleanup, err := croncli.OpenStore(ctx, configPath)
	if err != nil {
		return err
	}
	defer cleanup()
	return fn(store, evStore)
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func shortID(id string) string {
	if strings.HasPrefix(id, "cron_") && len(id) > 13 {
		return id[:13] + "..."
	}
	if len(id) > 12 {
		return id[:12] + "..."
	}
	return id
}

func warnIfGatewayNotNotified(err error) {
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "warning: gateway not notified: %v\n", err)
	}
}
