package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/joanneffffff/go-tiny-claw/internal/schema"
)

// Tool 定义了可执行工具的接口
type Tool interface {
	// Execute runs the tool with given arguments
	Execute(ctx context.Context, args map[string]string) (*schema.ToolResult, error)

	// Definition returns the tool definition for LLM
	Definition() schema.ToolDefinition
}

// Registry 定义了工具的注册与分发执行接口
type Registry interface {
	// GetAvailableTools 返回当前系统挂载的所有可用工具的 Schema
	GetAvailableTools() []schema.ToolDefinition

	// Execute 实际执行模型请求的工具，并返回结果
	Execute(ctx context.Context, call schema.ToolCall) schema.ToolResult

	// Register 注册一个工具到注册表
	Register(tool Tool)
}

// registry 是工具注册表的默认实现
type registry struct {
	tools map[string]Tool
}

// NewRegistry creates a new tool registry
func NewRegistry() Registry {
	return &registry{
		tools: make(map[string]Tool),
	}
}

// Register adds a tool to the registry
func (r *registry) Register(tool Tool) {
	r.tools[tool.Definition().Name] = tool
}

// GetAvailableTools returns all registered tools as ToolDefinitions
func (r *registry) GetAvailableTools() []schema.ToolDefinition {
	defs := make([]schema.ToolDefinition, 0, len(r.tools))
	for _, tool := range r.tools {
		defs = append(defs, tool.Definition())
	}
	return defs
}

// Execute finds and runs a tool by name
func (r *registry) Execute(ctx context.Context, call schema.ToolCall) schema.ToolResult {
	tool, ok := r.tools[call.Name]
	if !ok {
		return schema.ToolResult{
			ToolCallID: call.ID,
			Output:     fmt.Sprintf("tool not found: %s", call.Name),
			IsError:    true,
		}
	}

	// Parse arguments from JSON
	var args map[string]string
	if call.Arguments != nil {
		if err := json.Unmarshal(call.Arguments, &args); err != nil {
			return schema.ToolResult{
				ToolCallID: call.ID,
				Output:     fmt.Sprintf("failed to parse arguments: %v", err),
				IsError:    true,
			}
		}
	}

	result, err := tool.Execute(ctx, args)
	if err != nil {
		return schema.ToolResult{
			ToolCallID: call.ID,
			Output:     err.Error(),
			IsError:    true,
		}
	}

	return schema.ToolResult{
		ToolCallID: call.ID,
		Output:     result.Output,
		IsError:    result.IsError,
	}
}
