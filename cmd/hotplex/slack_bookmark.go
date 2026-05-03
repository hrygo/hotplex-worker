package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	slackcli "github.com/hrygo/hotplex/internal/cli/slack"
)

func newSlackBookmarkCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bookmark",
		Short: "Manage channel bookmarks",
		Long:  `Add, list, and remove bookmarks in Slack channels.`,
	}
	cmd.AddCommand(
		newSlackBookmarkAddCmd(),
		newSlackBookmarkListCmd(),
		newSlackBookmarkRemoveCmd(),
	)
	return cmd
}

func newSlackBookmarkAddCmd() *cobra.Command {
	var channel, title, url, emoji, configPath string
	var useJSON bool

	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add a bookmark",
		Long:  `Add a bookmark to a Slack channel.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, client, err := slackcli.LoadConfigAndClient(configPath)
			if err != nil {
				return err
			}

			result, err := slackcli.AddBookmark(cmd.Context(), client, channel, title, url, emoji)
			if err != nil {
				return err
			}

			if useJSON {
				return json.NewEncoder(os.Stdout).Encode(result)
			}
			fmt.Printf("ok  bookmark_id=%s  title=%q\n", result.ID, result.Title)
			return nil
		},
	}

	cmd.Flags().StringVar(&channel, "channel", "", "channel ID")
	cmd.Flags().StringVar(&title, "title", "", "bookmark title")
	cmd.Flags().StringVar(&url, "url", "", "bookmark URL")
	cmd.Flags().StringVar(&emoji, "emoji", "", "bookmark icon emoji")
	cmd.Flags().BoolVar(&useJSON, "json", false, "JSON output")
	configFlag(cmd, &configPath)
	_ = cmd.MarkFlagRequired("channel")
	_ = cmd.MarkFlagRequired("title")

	return cmd
}

func newSlackBookmarkListCmd() *cobra.Command {
	var channel, configPath string
	var useJSON bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List bookmarks",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, client, err := slackcli.LoadConfigAndClient(configPath)
			if err != nil {
				return err
			}

			bookmarks, err := slackcli.ListBookmarks(cmd.Context(), client, channel)
			if err != nil {
				return err
			}

			if useJSON {
				return json.NewEncoder(os.Stdout).Encode(bookmarks)
			}

			if len(bookmarks) == 0 {
				fmt.Fprintln(os.Stderr, "No bookmarks found.")
				return nil
			}

			fmt.Printf("%-20s %-30s %s\n", "ID", "TITLE", "URL")
			for _, bm := range bookmarks {
				fmt.Printf("%-20s %-30q %s\n", bm.ID, bm.Title, bm.URL)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&channel, "channel", "", "channel ID")
	cmd.Flags().BoolVar(&useJSON, "json", false, "JSON output")
	configFlag(cmd, &configPath)
	_ = cmd.MarkFlagRequired("channel")

	return cmd
}

func newSlackBookmarkRemoveCmd() *cobra.Command {
	var channel, bookmarkID, configPath string

	cmd := &cobra.Command{
		Use:   "remove",
		Short: "Remove a bookmark",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, client, err := slackcli.LoadConfigAndClient(configPath)
			if err != nil {
				return err
			}

			if err := slackcli.RemoveBookmark(cmd.Context(), client, channel, bookmarkID); err != nil {
				return err
			}

			fmt.Printf("ok  removed bookmark %s from %s\n", bookmarkID, channel)
			return nil
		},
	}

	cmd.Flags().StringVar(&channel, "channel", "", "channel ID")
	cmd.Flags().StringVar(&bookmarkID, "bookmark-id", "", "bookmark ID")
	configFlag(cmd, &configPath)
	_ = cmd.MarkFlagRequired("channel")
	_ = cmd.MarkFlagRequired("bookmark-id")

	return cmd
}
