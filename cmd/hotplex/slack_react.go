package main

import (
	"fmt"

	"github.com/spf13/cobra"

	slackcli "github.com/hrygo/hotplex/internal/cli/slack"
)

func newSlackReactCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "react",
		Short: "Add or remove emoji reactions",
		Long:  `Add or remove emoji reactions on Slack messages.`,
	}
	cmd.AddCommand(
		newSlackReactAddCmd(),
		newSlackReactRemoveCmd(),
	)
	return cmd
}

func newSlackReactAddCmd() *cobra.Command {
	var channel, ts, emoji, configPath string

	cmd := &cobra.Command{
		Use:     "add",
		Short:   "Add a reaction",
		Example: `  hotplex slack react add --channel D0AQJ5CLZN0 --ts 1777797319.120439 --emoji white_check_mark`,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, client, err := slackcli.LoadConfigAndClient(configPath)
			if err != nil {
				return err
			}

			if err := slackcli.AddReaction(cmd.Context(), client, channel, ts, emoji); err != nil {
				return err
			}

			fmt.Printf("ok  added :%s: on %s/%s\n", emoji, channel, ts)
			return nil
		},
	}

	cmd.Flags().StringVar(&channel, "channel", "", "channel ID")
	cmd.Flags().StringVar(&ts, "ts", "", "message timestamp")
	cmd.Flags().StringVarP(&emoji, "emoji", "e", "", "emoji name (without colons)")
	configFlag(cmd, &configPath)
	_ = cmd.MarkFlagRequired("channel")
	_ = cmd.MarkFlagRequired("ts")
	_ = cmd.MarkFlagRequired("emoji")

	return cmd
}

func newSlackReactRemoveCmd() *cobra.Command {
	var channel, ts, emoji, configPath string

	cmd := &cobra.Command{
		Use:   "remove",
		Short: "Remove a reaction",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, client, err := slackcli.LoadConfigAndClient(configPath)
			if err != nil {
				return err
			}

			if err := slackcli.RemoveReaction(cmd.Context(), client, channel, ts, emoji); err != nil {
				return err
			}

			fmt.Printf("ok  removed :%s: on %s/%s\n", emoji, channel, ts)
			return nil
		},
	}

	cmd.Flags().StringVar(&channel, "channel", "", "channel ID")
	cmd.Flags().StringVar(&ts, "ts", "", "message timestamp")
	cmd.Flags().StringVarP(&emoji, "emoji", "e", "", "emoji name (without colons)")
	configFlag(cmd, &configPath)
	_ = cmd.MarkFlagRequired("channel")
	_ = cmd.MarkFlagRequired("ts")
	_ = cmd.MarkFlagRequired("emoji")

	return cmd
}
