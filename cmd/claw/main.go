package main

import (
	"context"
	"fmt"
	"log"

	"github.com/joanneffffff/go-tiny-claw/internal/engine"
	"github.com/joanneffffff/go-tiny-claw/internal/provider"
	"github.com/joanneffffff/go-tiny-claw/internal/schema"
	"github.com/joanneffffff/go-tiny-claw/internal/tools"
)

// MockProvider is a mock LLM provider for testing
type MockProvider struct{}

func (p *MockProvider) Generate(ctx context.Context, messages []schema.Message, availableTools []schema.ToolDefinition) (*schema.Message, error) {
	return &schema.Message{
		Role:       schema.RoleAssistant,
		Content:    "This is a mock response from MockProvider",
		ToolCalls:  []schema.ToolCall{}, // No tool calls in mock
	}, nil
}

func main() {
	fmt.Println("🚀 欢迎来到 go-tiny-claw 引擎启动序列")

	// 1. 初始化模型 Provider (大脑)
	var llmProvider provider.LLMProvider = &MockProvider{}

	// 2. 初始化 Tool Registry (手脚)
	registry := tools.NewRegistry()
	// registry.Register(tools.NewBashTool())

	// 3. 组装并启动核心 Engine (操作系统心脏)
	// 设置工作目录
	workDir := "/app"
	agentEngine := engine.NewAgentEngine(llmProvider, registry, workDir)

	fmt.Println("开始执行任务...")
	err := agentEngine.Run(context.Background(), "帮我检查一下当前目录下的文件并输出一个 README.md 大纲")
	if err != nil {
		log.Fatalf("引擎运行崩溃: %v", err)
	}

	log.Println("架构蓝图搭建完毕，等待各核心模块注入！")
}