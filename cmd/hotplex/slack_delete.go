package main

import (
	"fmt"

	"github.com/spf13/cobra"

	slackcli "github.com/hrygo/hotplex/internal/cli/slack"
)

func newSlackDeleteFileCmd() *cobra.Command {
	var fileID, configPath string

	cmd := &cobra.Command{
		Use:   "delete-file",
		Short: "Delete a file from Slack",
		Long:  `Delete a file from Slack by its file ID.`,
		Example: `  hotplex slack delete-file --file-id F0AQJ5CLZN0
  hotplex slack delete-file --file-id F0AQJ5CLZN0 --config ./configs/config.yaml`,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, client, err := slackcli.LoadConfigAndClient(configPath)
			if err != nil {
				return err
			}

			if err := slackcli.DeleteFile(cmd.Context(), client, fileID); err != nil {
				return err
			}

			fmt.Printf("ok  file_id=%s  deleted\n", fileID)
			return nil
		},
	}

	cmd.Flags().StringVar(&fileID, "file-id", "", "Slack file ID")
	configFlag(cmd, &configPath)
	_ = cmd.MarkFlagRequired("file-id")

	return cmd
}
