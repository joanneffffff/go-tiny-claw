package tools

import "github.com/joanneffffff/go-tiny-claw/internal/schema"

// Tool defines the interface for executable tools
type Tool interface {
	// Name returns the tool name
	Name() string

	// Execute runs the tool with given arguments
	Execute(args map[string]string) (*schema.ToolResult, error)

	// Description returns the tool description
	Description() string

	// Definition returns the tool definition for LLM
	Definition() schema.ToolDefinition
}

// Registry manages tool registration and execution
type Registry struct {
	tools map[string]Tool
}

// NewRegistry creates a new tool registry
func NewRegistry() *Registry {
	return &Registry{
		tools: make(map[string]Tool),
	}
}

// Register adds a tool to the registry
func (r *Registry) Register(tool Tool) {
	r.tools[tool.Name()] = tool
}

// Execute finds and runs a tool by name
func (r *Registry) Execute(name string, args map[string]string) (*schema.ToolResult, error) {
	tool, ok := r.tools[name]
	if !ok {
		return nil, &ToolNotFoundError{Name: name}
	}
	return tool.Execute(args)
}

// ListTools returns all registered tools as ToolDefinitions
func (r *Registry) ListTools() []schema.ToolDefinition {
	defs := make([]schema.ToolDefinition, 0, len(r.tools))
	for _, tool := range r.tools {
		defs = append(defs, tool.Definition())
	}
	return defs
}

// ToolNotFoundError is returned when a tool is not found
type ToolNotFoundError struct {
	Name string
}

func (e *ToolNotFoundError) Error() string {
	return "tool not found: " + e.Name
}
