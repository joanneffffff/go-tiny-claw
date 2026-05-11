package engine

import (
	"context"
	"fmt"
	"log"
	"sync"

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

	// MaxConcurrency 全局最大并发数控制（Semaphore）
	// 限制同时运行的工具数量，防止资源耗尽
	MaxConcurrency int
}

func NewAgentEngine(p provider.LLMProvider, r tools.Registry, workDir string, enableThinking bool) *AgentEngine {
	return &AgentEngine{
		provider:       p,
		registry:       r,
		WorkDir:        workDir,
		EnableThinking: enableThinking,
		MaxConcurrency: 5, // 默认最大并发数为 5
	}
}

// WithMaxConcurrency 设置最大并发数（Builder 模式）
func (e *AgentEngine) WithMaxConcurrency(n int) *AgentEngine {
	if n > 0 {
		e.MaxConcurrency = n
	}
	return e
}

// Run 启动 Agent 的生命周期
func (e *AgentEngine) Run(ctx context.Context, userPrompt string) error {
	log.Printf("[Engine] 引擎启动，锁定工作区: %s\n", e.WorkDir)
	log.Printf("[Engine] 慢思考模式 (Thinking Phase): %v\n", e.EnableThinking)
	log.Printf("[Engine] 最大并发数 (MaxConcurrency): %d\n", e.MaxConcurrency)

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

	for {
		turnCount++
		log.Printf("\n========== [Turn %d] 开始 ==========\n", turnCount)

		// 获取当前挂载的所有工具定义
		availableTools := e.registry.GetAvailableTools()

		// ====================================================================
		// Phase 1: 慢思考阶段 (Thinking) - 剥夺工具，强制规划
		// ====================================================================
		if e.EnableThinking {
			log.Println("[Engine][Phase 1] 剥夺工具访问权，强制进入慢思考与规划阶段...")

			// 核心机制：传入的 availableTools 为 nil！
			// 大模型看不到任何 JSON Schema，被迫只能输出纯文本的思考过程。
			thinkResp, err := e.provider.Generate(ctx, contextHistory, nil)
			if err != nil {
				return fmt.Errorf("Thinking 阶段生成失败: %w", err)
			}

			// 如果模型输出了思考过程，我们将其作为 Assistant 消息追加到上下文中
			if thinkResp.Content != "" {
				fmt.Printf("🧠 [内部思考 Trace]: %s\n", thinkResp.Content)
				contextHistory = append(contextHistory, *thinkResp)
			}
		}

		// ====================================================================
		// Phase 2: 行动阶段 (Action) - 恢复工具，顺着规划执行
		// ====================================================================
		log.Println("[Engine][Phase 2] 恢复工具挂载，等待模型采取行动...")

		// 此时的 contextHistory 中已经包含了上一阶段模型自己的 Thinking Trace。
		// 模型会顺着自己的逻辑，结合恢复的 availableTools 发起精准的工具调用。
		actionResp, err := e.provider.Generate(ctx, contextHistory, availableTools)
		if err != nil {
			return fmt.Errorf("Action 阶段生成失败: %w", err)
		}

		contextHistory = append(contextHistory, *actionResp)

		if actionResp.Content != "" {
			fmt.Printf("🤖 [对外回复]: %s\n", actionResp.Content)
		}

		// ====================================================================
		// 退出与执行逻辑
		// ====================================================================
		if len(actionResp.ToolCalls) == 0 {
			log.Println("[Engine] 模型未请求调用工具，任务宣告完成。")
			break
		}

		log.Printf("[Engine] 模型请求调用 %d 个工具...\n", len(actionResp.ToolCalls))

		// ====================================================================
		// 核心改造: 只读并发 + 涉写串行 + 全局并发数控制
		// ====================================================================

		// 1. 分类：将工具调用分为只读组和涉写组
		var readOnlyCalls []schema.ToolCall
		var writeCalls []schema.ToolCall

		for _, call := range actionResp.ToolCalls {
			if e.registry.IsToolReadOnly(call.Name) {
				readOnlyCalls = append(readOnlyCalls, call)
			} else {
				writeCalls = append(writeCalls, call)
			}
		}

		log.Printf("[Engine] 工具分类: 只读=%d, 涉写=%d\n", len(readOnlyCalls), len(writeCalls))

		// 2. 预分配结果切片
		observationMsgs := make([]schema.Message, len(actionResp.ToolCalls))

		// 3. 建立 call.ID -> 索引 的映射，用于结果回填
		callIndexMap := make(map[string]int)
		for i, call := range actionResp.ToolCalls {
			callIndexMap[call.ID] = i
		}

		// 4. 全局并发控制：使用带缓冲的 channel 作为 Semaphore（令牌桶）
		semaphore := make(chan struct{}, e.MaxConcurrency)

		// 5. 声明 WaitGroup 用于阻塞等待所有协程完成
		var wg sync.WaitGroup

		// 6. 涉写工具串行执行的互斥锁
		var writeMutex sync.Mutex

		// ====================================================================
		// 执行只读工具（并发）
		// ====================================================================
		for _, call := range readOnlyCalls {
			wg.Add(1)

			go func(c schema.ToolCall) {
				defer wg.Done()

				// 获取并发令牌（阻塞直到有空闲槽位）
				semaphore <- struct{}{}
				defer func() { <-semaphore }() // 释放令牌

				log.Printf("  -> [ReadOnly] 🛠️ 触发并发执行: %s, 参数: %s\n", c.Name, string(c.Arguments))

				result := e.registry.Execute(ctx, c)

				if result.IsError {
					log.Printf("  -> [ReadOnly] ❌ 工具执行报错: %s\n", result.Output)
				} else {
					log.Printf("  -> [ReadOnly] ✅ 工具执行成功 (返回 %d 字节)\n", len(result.Output))
				}

				// 回填结果到对应索引
				obsMsg := schema.Message{
					Role:       schema.RoleUser,
					Content:    result.Output,
					ToolCallID: c.ID,
				}
				observationMsgs[callIndexMap[c.ID]] = obsMsg

			}(call)
		}

		// ====================================================================
		// 执行涉写工具（串行）
		// ====================================================================
		for _, call := range writeCalls {
			wg.Add(1)

			go func(c schema.ToolCall) {
				defer wg.Done()

				// 获取并发令牌（全局并发数控制）
				semaphore <- struct{}{}
				defer func() { <-semaphore }()

				// 涉写工具串行执行：加锁确保同一时间只有一个涉写工具运行
				writeMutex.Lock()
				defer writeMutex.Unlock()

				log.Printf("  -> [Write] 🔒 触发串行执行: %s, 参数: %s\n", c.Name, string(c.Arguments))

				result := e.registry.Execute(ctx, c)

				if result.IsError {
					log.Printf("  -> [Write] ❌ 工具执行报错: %s\n", result.Output)
				} else {
					log.Printf("  -> [Write] ✅ 工具执行成功 (返回 %d 字节)\n", len(result.Output))
				}

				// 回填结果到对应索引
				obsMsg := schema.Message{
					Role:       schema.RoleUser,
					Content:    result.Output,
					ToolCallID: c.ID,
				}
				observationMsgs[callIndexMap[c.ID]] = obsMsg

			}(call)
		}

		// 7. Join 阻塞等待：主循环挂起，直到所有的并发协程全部执行完毕
		wg.Wait()
		log.Println("[Engine] 所有工具执行完毕，开始聚合观察结果 (Observation)...")

		// 8. 聚合装填：将结果按照原本的顺序，一次性追加到上下文时间线中
		for _, obs := range observationMsgs {
			contextHistory = append(contextHistory, obs)
		}
	}

	return nil
}
