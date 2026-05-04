package main

import (
	"github.com/spf13/cobra"
)

func newSlackCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "slack",
		Short: "Slack messaging operations",
		Long: `Send messages, upload files, and interact with Slack workspaces.
Uses the same configuration as the gateway (~/.hotplex/.env).`,
	}
	cmd.AddCommand(
		newSlackSendMessageCmd(),
		newSlackUpdateMessageCmd(),
		newSlackScheduleMessageCmd(),
		newSlackUploadFileCmd(),
		newSlackDownloadFileCmd(),
		newSlackDeleteFileCmd(),
		newSlackListChannelsCmd(),
		newSlackBookmarkCmd(),
		newSlackReactCmd(),
	)
	return cmd
}
