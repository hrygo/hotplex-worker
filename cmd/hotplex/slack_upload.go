package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	slackcli "github.com/hrygo/hotplex/internal/cli/slack"
)

func newSlackUploadFileCmd() *cobra.Command {
	var file, title, comment, channel, threadTS, configPath string
	var maxSize int64
	var useJSON bool

	cmd := &cobra.Command{
		Use:   "upload-file",
		Short: "Upload a file to Slack",
		Long:  `Upload a file (any type) to a Slack channel or DM. Supports files up to 50MB by default.`,
		Example: `  hotplex slack upload-file --file ./podcast.mp3 --title "Podcast" --channel D0AQJ5CLZN0
  hotplex slack upload-file -f report.pdf --comment "Q4 report"`,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, client, err := slackcli.LoadConfigAndClient(configPath)
			if err != nil {
				return err
			}

			ch, err := slackcli.ResolveChannel(channel)
			if err != nil {
				return err
			}
			ts := slackcli.ResolveThreadTS(threadTS)

			if maxSize == 0 {
				maxSize = 50 * 1024 * 1024
			}

			result, err := slackcli.UploadFile(cmd.Context(), client, &slackcli.UploadParams{
				FilePath: file,
				Title:    title,
				Comment:  comment,
				Channel:  ch,
				ThreadTS: ts,
				MaxSize:  maxSize,
			})
			if err != nil {
				return err
			}

			if useJSON {
				return json.NewEncoder(os.Stdout).Encode(result)
			}
			fmt.Printf("ok  file_id=%s  title=%q  size=%d  channel=%s\n",
				result.FileID, result.Title, result.Size, result.Channel)
			return nil
		},
	}

	cmd.Flags().StringVarP(&file, "file", "f", "", "local file path")
	cmd.Flags().StringVar(&title, "title", "", "file title (defaults to filename)")
	cmd.Flags().StringVar(&comment, "comment", "", "file description")
	cmd.Flags().StringVar(&channel, "channel", "", "target channel/DM")
	cmd.Flags().StringVar(&threadTS, "thread-ts", "", "thread timestamp for reply")
	cmd.Flags().Int64Var(&maxSize, "max-size", 0, "max file size in bytes (default 50MB)")
	cmd.Flags().BoolVar(&useJSON, "json", false, "JSON output")
	configFlag(cmd, &configPath)
	cmd.MarkFlagRequired("file")

	return cmd
}
