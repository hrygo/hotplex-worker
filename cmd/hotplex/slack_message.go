package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	slackcli "github.com/hrygo/hotplex/internal/cli/slack"
)

func newSlackSendMessageCmd() *cobra.Command {
	var text, channel, threadTS, configPath string
	var useJSON bool

	cmd := &cobra.Command{
		Use:   "send-message",
		Short: "Send a text message",
		Long:  `Send a text message to a Slack channel or DM. Supports mrkdwn formatting.`,
		Example: `  hotplex slack send-message --text "Hello" --channel D0AQJ5CLZN0
  hotplex slack send-message -t "Reply" --thread-ts 1777797319.120439`,
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

			result, err := slackcli.SendMessage(cmd.Context(), client, ch, ts, text)
			if err != nil {
				return err
			}

			if useJSON {
				return json.NewEncoder(os.Stdout).Encode(result)
			}
			fmt.Printf("ok  channel=%s  ts=%s\n", result.Channel, result.TS)
			return nil
		},
	}

	cmd.Flags().StringVarP(&text, "text", "t", "", "message text (supports mrkdwn)")
	cmd.Flags().StringVar(&channel, "channel", "", "target channel/DM ID")
	cmd.Flags().StringVar(&threadTS, "thread-ts", "", "thread timestamp for reply")
	cmd.Flags().BoolVar(&useJSON, "json", false, "JSON output")
	configFlag(cmd, &configPath)
	_ = cmd.MarkFlagRequired("text")

	return cmd
}

func newSlackUpdateMessageCmd() *cobra.Command {
	var text, channel, ts, configPath string
	var useJSON bool

	cmd := &cobra.Command{
		Use:   "update-message",
		Short: "Update an existing message",
		Long:  `Update the content of a previously sent message.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, client, err := slackcli.LoadConfigAndClient(configPath)
			if err != nil {
				return err
			}

			result, err := slackcli.UpdateMessage(cmd.Context(), client, channel, ts, text)
			if err != nil {
				return err
			}

			if useJSON {
				return json.NewEncoder(os.Stdout).Encode(result)
			}
			fmt.Printf("ok  channel=%s  ts=%s\n", result.Channel, result.TS)
			return nil
		},
	}

	cmd.Flags().StringVarP(&text, "text", "t", "", "new message text")
	cmd.Flags().StringVar(&channel, "channel", "", "channel ID")
	cmd.Flags().StringVar(&ts, "ts", "", "message timestamp")
	cmd.Flags().BoolVar(&useJSON, "json", false, "JSON output")
	configFlag(cmd, &configPath)
	_ = cmd.MarkFlagRequired("text")
	_ = cmd.MarkFlagRequired("channel")
	_ = cmd.MarkFlagRequired("ts")

	return cmd
}

func newSlackScheduleMessageCmd() *cobra.Command {
	var text, channel, at, configPath string
	var useJSON bool

	cmd := &cobra.Command{
		Use:   "schedule-message",
		Short: "Schedule a message for future delivery",
		Long:  `Schedule a message to be sent at a specified time. Accepts ISO 8601 or Unix timestamp.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, client, err := slackcli.LoadConfigAndClient(configPath)
			if err != nil {
				return err
			}

			ch, err := slackcli.ResolveChannel(channel)
			if err != nil {
				return err
			}

			var postAt int64
			if t, err := time.Parse(time.RFC3339, at); err == nil {
				postAt = t.Unix()
			} else {
				postAt, err = strconv.ParseInt(at, 10, 64)
				if err != nil {
					return fmt.Errorf("--at must be ISO 8601 or Unix timestamp")
				}
			}

			scheduledID, err := slackcli.ScheduleMessage(cmd.Context(), client, ch, postAt, text)
			if err != nil {
				return err
			}

			if useJSON {
				return json.NewEncoder(os.Stdout).Encode(map[string]any{
					"ok":                   true,
					"scheduled_message_id": scheduledID,
					"channel":              ch,
					"post_at":              postAt,
				})
			}
			fmt.Printf("ok  scheduled_id=%s  channel=%s  post_at=%d\n", scheduledID, ch, postAt)
			return nil
		},
	}

	cmd.Flags().StringVarP(&text, "text", "t", "", "message text")
	cmd.Flags().StringVar(&channel, "channel", "", "target channel")
	cmd.Flags().StringVar(&at, "at", "", "send time (ISO 8601 or Unix timestamp)")
	cmd.Flags().BoolVar(&useJSON, "json", false, "JSON output")
	configFlag(cmd, &configPath)
	_ = cmd.MarkFlagRequired("text")
	_ = cmd.MarkFlagRequired("at")

	return cmd
}
