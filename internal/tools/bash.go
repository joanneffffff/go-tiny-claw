package tools

import (
	"context"
	"os/exec"

	"github.com/joanneffffff/go-tiny-claw/internal/schema"
)

type BashTool struct{}

func (b *BashTool) Execute(ctx context.Context, args map[string]string) (*schema.ToolResult, error) {
	cmd := args["command"]
	result, err := exec.Command("sh", "-c", cmd).CombinedOutput()
	output := string(result)
	if err != nil {
		return &schema.ToolResult{
			ToolCallID: "",
			Output:     output,
			IsError:    true,
		}, nil // return nil error since we handle it in IsError
	}
	return &schema.ToolResult{
		ToolCallID: "",
		Output:     output,
		IsError:    false,
	}, nil
}

func (b *BashTool) Definition() schema.ToolDefinition {
	return schema.ToolDefinition{
		Name:        "bash",
		Description: "Execute a bash command in the workspace",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"command": map[string]string{"type": "string"},
			},
			"required": []string{"command"},
		},
	}
}