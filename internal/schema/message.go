package schema

// Message represents a single message in the conversation
type Message struct {
	Role    string `json:"role"`    // "user", "assistant", "system"
	Content string `json:"content"`
}

// ToolCall represents a tool invocation
type ToolCall struct {
	Name      string            `json:"name"`
	Arguments map[string]string `json:"arguments"`
}

// ToolResult represents the result of a tool execution
type ToolResult struct {
	Name    string `json:"name"`
	Output  string `json:"output"`
	Error   string `json:"error,omitempty"`
	Success bool   `json:"success"`
}
