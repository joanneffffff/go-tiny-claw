// cmd/claw/main.go
package main

import (
    "context"
    "log"
    "os"

    "github.com/joanneffffff/go-tiny-claw/internal/engine"
    "github.com/joanneffffff/go-tiny-claw/internal/provider"
    "github.com/joanneffffff/go-tiny-claw/internal/schema"
    "github.com/joanneffffff/go-tiny-claw/internal/tools"
)

// 伪造的工具注册表 (用于测试 Provider 的工具提取能力)
type mockRegistry struct{}

func (m *mockRegistry) GetAvailableTools() []schema.ToolDefinition {
    return []schema.ToolDefinition{
        {
            Name:        "get_weather",
            Description: "获取指定城市的当前天气情况。",
            InputSchema: map[string]interface{}{
                "type": "object",
                "properties": map[string]interface{}{
                    "city": map[string]interface{}{
                        "type": "string",
                    },
                },
                "required": []string{"city"},
            },
        },
    }
}

func (m *mockRegistry) Execute(ctx context.Context, call schema.ToolCall) schema.ToolResult {
    log.Printf("  -> [Mock 工具执行] 获取 %s 的天气中...\n", call.Name)
    return schema.ToolResult{
        ToolCallID: call.ID,
        Output:     "API 返回：今天是晴天，气温 25 度。",
        IsError:    false,
    }
}

func (m *mockRegistry) Register(tool tools.Tool) {}

func main() {
    if os.Getenv("ANTHROPIC_API_KEY") == "" {
        log.Fatal("请先导出 ANTHROPIC_API_KEY 环境变量")
    }

    workDir, _ := os.Getwd()

    model := os.Getenv("ANTHROPIC_MODEL")
    if model == "" {
        model = "MiniMax-M2.7"
    }
    llmProvider := provider.NewCustomClaudeProvider(model)
    registry := &mockRegistry{}

    enableThinking := os.Getenv("ENABLE_THINKING") == "true"
    eng := engine.NewAgentEngine(llmProvider, registry, workDir, enableThinking)

    prompt := "我想去北京跑步，帮我查查天气适合吗？"

    log.Printf("[Config] EnableThinking = %v", enableThinking)
    err := eng.Run(context.Background(), prompt)
    if err != nil {
        log.Fatalf("引擎运行崩溃: %v", err)
    }
}