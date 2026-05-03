package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	slackcli "github.com/hrygo/hotplex/internal/cli/slack"
)

func newSlackCanvasCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "canvas",
		Short: "Manage Slack Canvas documents",
		Long:  `Create, edit, and inspect Slack Canvas documents (native Slack document system).`,
	}
	cmd.AddCommand(
		newSlackCanvasCreateCmd(),
		newSlackCanvasEditCmd(),
		newSlackCanvasListSectionsCmd(),
	)
	return cmd
}

func newSlackCanvasCreateCmd() *cobra.Command {
	var title, content, file, configPath string
	var useJSON bool

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new Canvas",
		Long:  `Create a new Canvas document in Slack with Markdown content.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, client, err := slackcli.LoadConfigAndClient(configPath)
			if err != nil {
				return err
			}

			body := content
			if file != "" {
				data, err := os.ReadFile(file)
				if err != nil {
					return fmt.Errorf("read file: %w", err)
				}
				body = string(data)
			}

			if body == "" {
				return fmt.Errorf("--content or --file is required")
			}

			result, err := slackcli.CreateCanvas(cmd.Context(), client, title, body)
			if err != nil {
				return err
			}

			if useJSON {
				return json.NewEncoder(os.Stdout).Encode(result)
			}
			fmt.Printf("ok  canvas_id=%s  title=%q\n", result.CanvasID, result.Title)
			return nil
		},
	}

	cmd.Flags().StringVar(&title, "title", "", "canvas title")
	cmd.Flags().StringVar(&content, "content", "", "initial content (markdown)")
	cmd.Flags().StringVar(&file, "file", "", "read content from file")
	cmd.Flags().BoolVar(&useJSON, "json", false, "JSON output")
	configFlag(cmd, &configPath)
	cmd.MarkFlagRequired("title")

	return cmd
}

func newSlackCanvasEditCmd() *cobra.Command {
	var canvasID, content, file, sectionID, operation, configPath string

	cmd := &cobra.Command{
		Use:   "edit",
		Short: "Edit a Canvas",
		Long:  `Edit content of an existing Canvas document.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, client, err := slackcli.LoadConfigAndClient(configPath)
			if err != nil {
				return err
			}

			body := content
			if file != "" {
				data, err := os.ReadFile(file)
				if err != nil {
					return fmt.Errorf("read file: %w", err)
				}
				body = string(data)
			}

			if body == "" {
				return fmt.Errorf("--content or --file is required")
			}

			if err := slackcli.EditCanvas(cmd.Context(), client, canvasID, body, sectionID, operation); err != nil {
				return err
			}

			fmt.Printf("ok  canvas_id=%s  operation=%s\n", canvasID, operation)
			return nil
		},
	}

	cmd.Flags().StringVar(&canvasID, "canvas-id", "", "canvas ID")
	cmd.Flags().StringVar(&content, "content", "", "new content (markdown)")
	cmd.Flags().StringVar(&file, "file", "", "read content from file")
	cmd.Flags().StringVar(&sectionID, "section-id", "", "edit specific section only")
	cmd.Flags().StringVar(&operation, "operation", "replace", "operation: replace, insert_after, insert_before, delete")
	configFlag(cmd, &configPath)
	cmd.MarkFlagRequired("canvas-id")

	return cmd
}

func newSlackCanvasListSectionsCmd() *cobra.Command {
	var canvasID, configPath string
	var useJSON bool

	cmd := &cobra.Command{
		Use:   "list-sections",
		Short: "List Canvas sections",
		Long:  `List all sections in a Canvas document.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, client, err := slackcli.LoadConfigAndClient(configPath)
			if err != nil {
				return err
			}

			sections, err := slackcli.ListCanvasSections(cmd.Context(), client, canvasID)
			if err != nil {
				return err
			}

			if useJSON {
				return json.NewEncoder(os.Stdout).Encode(sections)
			}

			if len(sections) == 0 {
				fmt.Fprintln(os.Stderr, "No sections found.")
				return nil
			}

			for _, s := range sections {
				fmt.Printf("%s\n", s.ID)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&canvasID, "canvas-id", "", "canvas ID")
	cmd.Flags().BoolVar(&useJSON, "json", false, "JSON output")
	configFlag(cmd, &configPath)
	cmd.MarkFlagRequired("canvas-id")

	return cmd
}
