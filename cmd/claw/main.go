package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/joanneffffff/go-tiny-claw/internal/engine"
	"github.com/joanneffffff/go-tiny-claw/internal/provider"
	"github.com/joanneffffff/go-tiny-claw/internal/schema"
	"github.com/joanneffffff/go-tiny-claw/internal/tools"
)

// MockProvider simulates an LLM that supports two-stage ReAct
type MockProvider struct {
	callCount int
}

func (p *MockProvider) Generate(ctx context.Context, messages []schema.Message, availableTools []schema.ToolDefinition) (*schema.Message, error) {
	p.callCount++

	log.Printf("[MockProvider] Turn %d: generating response (tools=%v)...", p.callCount, availableTools != nil)

	// If no tools provided, this is a thinking phase
	if availableTools == nil {
		return &schema.Message{
			Role:     schema.RoleAssistant,
			Reasoning: "我需要先检查当前目录的文件结构，了解项目布局。",
		}, nil
	}

	// With tools provided, this is an action phase
	// First call: return a tool call to bash
	if p.callCount == 2 {
		args, _ := json.Marshal(map[string]string{"command": "ls -la"})
		return &schema.Message{
			Role:    schema.RoleAssistant,
			Content: "我将执行 ls -la 来查看目录结构。",
			ToolCalls: []schema.ToolCall{
				{ID: "call_1", Name: "bash", Arguments: args},
			},
		}, nil
	}

	// Second call: return final response
	return &schema.Message{
		Role:       schema.RoleAssistant,
		Content:    "我已经检查了目录结构，现在为你生成 README.md 大纲：\n\n# 项目结构\n- cmd/claw/\n- internal/engine/\n- internal/provider/",
		ToolCalls: []schema.ToolCall{},
	}, nil
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.SetOutput(os.Stdout)

	fmt.Println("🚀 欢迎来到 go-tiny-claw 引擎启动序列")

	// 1. 初始化模型 Provider (大脑)
	var llmProvider provider.LLMProvider = &MockProvider{}

	// 2. 初始化 Tool Registry (手脚)
	registry := tools.NewRegistry()
	registry.Register(&tools.BashTool{})

	// 3. 组装并启动核心 Engine (操作系统心脏)
	workDir := "/app"
	agentEngine := engine.NewAgentEngine(llmProvider, registry, workDir)

	// 开启两阶段 ReAct 循环
	agentEngine.EnableThinking = true

	fmt.Println("开始执行任务...")
	err := agentEngine.Run(context.Background(), "帮我检查一下当前目录下的文件并输出一个 README.md 大纲")
	if err != nil {
		log.Fatalf("引擎运行崩溃: %v", err)
	}

	log.Println("架构蓝图搭建完毕，等待各核心模块注入！")
}
