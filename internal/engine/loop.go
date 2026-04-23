package engine

import (
	"context"
	"fmt"
	"log"

	"github.com/joanneffffff/go-tiny-claw/internal/provider"
	"github.com/joanneffffff/go-tiny-claw/internal/schema"
	"github.com/joanneffffff/go-tiny-claw/internal/tools"
)

// AgentEngine 是微型 OS 的核心驱动
type AgentEngine struct {
	provider provider.LLMProvider
	registry tools.Registry

	// WorkDir (工作区): 借鉴 OpenClaw 的理念，Agent 必须有一个明确的物理边界
	WorkDir string

	// EnableThinking 开启两阶段 ReAct 循环（先推理再行动）
	EnableThinking bool
}

func NewAgentEngine(p provider.LLMProvider, r tools.Registry, workDir string) *AgentEngine {
	return &AgentEngine{
		provider: p,
		registry: r,
		WorkDir:  workDir,
	}
}

// Run 启动 Agent 的生命周期
func (e *AgentEngine) Run(ctx context.Context, userPrompt string) error {
	log.Printf("[Engine] 引擎启动，锁定工作区: %s\n", e.WorkDir)

	// 1. 初始化会话的 Context (上下文内存)
	contextHistory := []schema.Message{
		{
			Role:    schema.RoleSystem,
			Content: "You are go-tiny-claw, an expert coding assistant. You have full access to tools in the workspace.",
		},
		{
			Role:    schema.RoleUser,
			Content: userPrompt,
		},
	}

	turnCount := 0

	// 2. The Main Loop
	for {
		turnCount++
		log.Printf("========== [Turn %d] 开始 ==========\n", turnCount)

		availableTools := e.registry.GetAvailableTools()

		// ===== 两阶段 ReAct 循环 =====
		if e.EnableThinking {
			// 阶段 1: 思考 (Thinking) - 不带工具，让模型输出推理过程
			log.Println("[Engine] 阶段一：正在思考 (Thinking)...")
			thinkingMsg, err := e.provider.Generate(ctx, contextHistory, nil)
			if err != nil {
				return fmt.Errorf("模型思考失败: %w", err)
			}

			// 将思考过程追加到上下文
			contextHistory = append(contextHistory, *thinkingMsg)

			if thinkingMsg.Reasoning != "" {
				fmt.Printf("💭 模型思考: %s\n", thinkingMsg.Reasoning)
			}

			// 阶段 2: 行动 (Action) - 携带工具定义，让模型决定是否调用工具
			log.Println("[Engine] 阶段二：请求行动 (Action)...")
			actionMsg, err := e.provider.Generate(ctx, contextHistory, availableTools)
			if err != nil {
				return fmt.Errorf("模型行动请求失败: %w", err)
			}

			contextHistory = append(contextHistory, *actionMsg)

			if actionMsg.Content != "" {
				fmt.Printf("🤖 模型: %s\n", actionMsg.Content)
			}

			// 如果没有工具调用，任务完成
			if len(actionMsg.ToolCalls) == 0 {
				log.Println("[Engine] 任务完成，退出循环。")
				break
			}

			// 执行工具
			e.executeToolCalls(ctx, actionMsg.ToolCalls, &contextHistory)

		} else {
			// ===== 标准 ReAct 循环 (单阶段) =====
			log.Println("[Engine] 正在思考 (Reasoning)...")
			responseMsg, err := e.provider.Generate(ctx, contextHistory, availableTools)
			if err != nil {
				return fmt.Errorf("模型生成失败: %w", err)
			}

			contextHistory = append(contextHistory, *responseMsg)

			if responseMsg.Content != "" {
				fmt.Printf("🤖 模型: %s\n", responseMsg.Content)
			}

			if len(responseMsg.ToolCalls) == 0 {
				log.Println("[Engine] 任务完成，退出循环。")
				break
			}

			e.executeToolCalls(ctx, responseMsg.ToolCalls, &contextHistory)
		}
	}

	return nil
}

// executeToolCalls 执行工具调用并将结果追加到上下文
func (e *AgentEngine) executeToolCalls(ctx context.Context, toolCalls []schema.ToolCall, contextHistory *[]schema.Message) {
	log.Printf("[Engine] 模型请求调用 %d 个工具...\n", len(toolCalls))

	for _, toolCall := range toolCalls {
		log.Printf("  -> 🛠️ 执行工具: %s, 参数: %s\n", toolCall.Name, string(toolCall.Arguments))

		result := e.registry.Execute(ctx, toolCall)

		if result.IsError {
			log.Printf("  -> ❌ 工具执行报错: %s\n", result.Output)
		} else {
			log.Printf("  -> ✅ 工具执行成功 (返回 %d 字节)\n", len(result.Output))
		}

		observationMsg := schema.Message{
			Role:       schema.RoleUser,
			Content:    result.Output,
			ToolCallID: toolCall.ID,
		}
		*contextHistory = append(*contextHistory, observationMsg)
	}
}
