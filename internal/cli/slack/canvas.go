package slackcli

import (
	"context"
	"fmt"

	"github.com/slack-go/slack"
)

type CanvasResult struct {
	CanvasID string `json:"canvas_id"`
	Title    string `json:"title"`
}

func CreateCanvas(ctx context.Context, client *slack.Client, title, content string) (*CanvasResult, error) {
	docContent := slack.DocumentContent{
		Type:     "markdown",
		Markdown: content,
	}

	canvasID, err := client.CreateCanvasContext(ctx, title, docContent)
	if err != nil {
		return nil, fmt.Errorf("create canvas: %w", err)
	}

	return &CanvasResult{
		CanvasID: canvasID,
		Title:    title,
	}, nil
}

func EditCanvas(ctx context.Context, client *slack.Client, canvasID, content, sectionID, operation string) error {
	if operation == "" {
		operation = "replace"
	}

	change := slack.CanvasChange{
		Operation: operation,
		DocumentContent: slack.DocumentContent{
			Type:     "markdown",
			Markdown: content,
		},
	}
	if sectionID != "" {
		change.SectionID = sectionID
	}

	params := slack.EditCanvasParams{
		CanvasID: canvasID,
		Changes:  []slack.CanvasChange{change},
	}

	return client.EditCanvasContext(ctx, params)
}

type CanvasSection struct {
	ID string `json:"id"`
}

func ListCanvasSections(ctx context.Context, client *slack.Client, canvasID string) ([]CanvasSection, error) {
	params := slack.LookupCanvasSectionsParams{
		CanvasID: canvasID,
	}

	sections, err := client.LookupCanvasSectionsContext(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("list canvas sections: %w", err)
	}

	var result []CanvasSection
	for _, s := range sections {
		result = append(result, CanvasSection{ID: s.ID})
	}

	return result, nil
}
