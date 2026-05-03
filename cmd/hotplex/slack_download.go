package main

import (
	"fmt"

	"github.com/spf13/cobra"

	slackcli "github.com/hrygo/hotplex/internal/cli/slack"
)

func newSlackDownloadFileCmd() *cobra.Command {
	var fileID, output, configPath string

	cmd := &cobra.Command{
		Use:   "download-file",
		Short: "Download a file from Slack",
		Long:  `Download a file from Slack by its file ID and save to a local path.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, client, err := slackcli.LoadConfigAndClient(configPath)
			if err != nil {
				return err
			}

			if err := slackcli.DownloadFile(cmd.Context(), client, fileID, output); err != nil {
				return err
			}

			fmt.Printf("ok  file_id=%s  output=%s\n", fileID, output)
			return nil
		},
	}

	cmd.Flags().StringVar(&fileID, "file-id", "", "Slack file ID")
	cmd.Flags().StringVarP(&output, "output", "o", "", "local save path")
	configFlag(cmd, &configPath)
	cmd.MarkFlagRequired("file-id")
	cmd.MarkFlagRequired("output")

	return cmd
}
