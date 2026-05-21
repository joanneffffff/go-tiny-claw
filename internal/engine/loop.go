package engine

import (
	"context"
	"fmt"
	"log"
	"sync"

	ctxpkg "github.com/joanneffffff/go-tiny-claw/internal/context"
	"github.com/joanneffffff/go-tiny-claw/internal/provider"
	"github.com/joanneffffff/go-tiny-claw/internal/schema"
	"github.com/joanneffffff/go-tiny-claw/internal/tools"
)

// AgentEngine 是微型 OS 的核心驱动
type AgentEngine struct {
	provider provider.LLMProvider
	registry tools.Registry

	// EnableThinking 开启两阶段 ReAct 循环（先推理再行动）
	EnableThinking bool

	// MaxConcurrency 全局最大并发数控制（Semaphore）
	// 限制同时运行的工具数量，防止资源耗尽
	MaxConcurrency int

	// compactor 上下文压缩器，防止大模型 OOM
	compactor *ctxpkg.Compactor
}

func NewAgentEngine(p provider.LLMProvider, r tools.Registry, enableThinking bool) *AgentEngine {
	return &AgentEngine{
		provider:       p,
		registry:       r,
		EnableThinking: enableThinking,
		MaxConcurrency: 5, // 默认最大并发数为 5
		// 【初始化压缩器】：水位线阈值 3000 字符，保护最近 6 条消息
		compactor: ctxpkg.NewCompactor(3000, 6),
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
// 【核心改造】: 移除 userPrompt 参数，改为接收一个具体的 Session 实例
// reporter 参数允许引擎向不同的展现层（终端、飞书、钉钉等）输出信息
func (e *AgentEngine) Run(ctx context.Context, session *Session, reporter Reporter) error {
	log.Printf("[Engine] 唤醒会话 [%s]，锁定工作区: %s\n", session.ID, session.WorkDir)
	log.Printf("[Engine] 慢思考模式 (Thinking Phase): %v\n", e.EnableThinking)
	log.Printf("[Engine] 最大并发数 (MaxConcurrency): %d\n", e.MaxConcurrency)

	// 根据当前 Session 的工作区，动态组装最新的 System Prompt
	composer := ctxpkg.NewPromptComposer(session.WorkDir)
	systemMsg := composer.Build()

	for {
		// 获取当前挂载的所有工具定义
		availableTools := e.registry.GetAvailableTools()

		// 1. 从 Session 提取出近期的 Working Memory (例如最近 20 条，给压缩器留下充足的判断空间)
		workingMemory := session.GetWorkingMemory(20)

		var contextHistory []schema.Message
		contextHistory = append(contextHistory, systemMsg)
		contextHistory = append(contextHistory, workingMemory...)

		// 2. 【核心注入点】: 在向 Provider 发起推理前，过一遍内存压缩器！
		// 无论你带出了多少上下文，如果字符总数超标，早期日志将被掩码化，超大日志将被掐头去尾
		compactedContext := e.compactor.Compact(contextHistory)

		// ====================================================================
		// Phase 1: 慢思考阶段 (Thinking) - 剥夺工具，强制规划
		// ====================================================================
		if e.EnableThinking {
			log.Println("[Engine][Phase 1] 剥夺工具访问权，强制进入慢思考与规划阶段...")

			// 【触发 Reporter】: 开始慢思考
			if reporter != nil {
				reporter.OnThinking(ctx)
			}

			// 核心机制：传入的 availableTools 为 nil！
			// 大模型看不到任何 JSON Schema，被迫只能输出纯文本的思考过程。
			thinkResp, err := e.provider.Generate(ctx, compactedContext, nil)
			if err != nil {
				return fmt.Errorf("Thinking 阶段生成失败: %w", err)
			}

			// 如果模型输出了思考过程，我们将其作为 Assistant 消息追加到上下文中
			if thinkResp.Content != "" {
				log.Printf("🧠 [内部思考 Trace]: %s\n", thinkResp.Content)
				// 将思考过程持久化到 Session 中！
				session.Append(*thinkResp)
				// 把它追加到当前这一轮的临时上下文中，供 Action 阶段使用
				compactedContext = append(compactedContext, *thinkResp)
			}
		}

		// ====================================================================
		// Phase 2: 行动阶段 (Action) - 恢复工具，顺着规划执行
		// ====================================================================
		log.Println("[Engine][Phase 2] 恢复工具挂载，等待模型采取行动...")

		// 此时的 compactedContext 中已经包含了上一阶段模型自己的 Thinking Trace。
		// 模型会顺着自己的逻辑，结合恢复的 availableTools 发起精准的工具调用。
		log.Println("[Engine] 正在调用 LLM API...")
		actionResp, err := e.provider.Generate(ctx, compactedContext, availableTools)
		if err != nil {
			return fmt.Errorf("Action 阶段生成失败: %w", err)
		}
		log.Println("[Engine] LLM API 响应完成")

		// 【驾驭精髓】：注意，写入 Session（硬盘/全量内存）的永远是全量的真实响应，不受 Compact 影响！
		// Compact 只作用于本轮发给大模型的那个临时 Context。
		session.Append(*actionResp)
		compactedContext = append(compactedContext, *actionResp)

		// 【触发 Reporter】: 输出阶段性总结或最终回复
		if actionResp.Content != "" && reporter != nil {
			reporter.OnMessage(ctx, actionResp.Content)
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

				// 【触发 Reporter】: 报告即将在底层执行的工具
				if reporter != nil {
					reporter.OnToolCall(ctx, c.Name, string(c.Arguments))
				}

				log.Printf("  -> [ReadOnly] 🛠️ 触发并发执行: %s, 参数: %s\n", c.Name, string(c.Arguments))

				result := e.registry.Execute(ctx, c)

				if result.IsError {
					log.Printf("  -> [ReadOnly] ❌ 工具执行报错: %s\n", result.Output)
				} else {
					log.Printf("  -> [ReadOnly] ✅ 工具执行成功 (返回 %d 字节)\n", len(result.Output))
				}

				// 【触发 Reporter】: 汇报工具物理执行的结果
				// 为了防止大文件读取导致飞书消息过长被截断，我们仅汇报工具执行状态
				// 注意：传递给大模型的 observationMsgs 依然是完整数据，只是人类看到的 Reporter 是缩略版
				if reporter != nil {
					displayOutput := result.Output
					if len(displayOutput) > 200 {
						displayOutput = displayOutput[:200] + "... (已截断)"
					}
					reporter.OnToolResult(ctx, c.Name, displayOutput, result.IsError)
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

				// 【触发 Reporter】: 报告即将在底层执行的工具
				if reporter != nil {
					reporter.OnToolCall(ctx, c.Name, string(c.Arguments))
				}

				log.Printf("  -> [Write] 🔒 触发串行执行: %s, 参数: %s\n", c.Name, string(c.Arguments))

				result := e.registry.Execute(ctx, c)

				if result.IsError {
					log.Printf("  -> [Write] ❌ 工具执行报错: %s\n", result.Output)
				} else {
					log.Printf("  -> [Write] ✅ 工具执行成功 (返回 %d 字节)\n", len(result.Output))
				}

				// 【触发 Reporter】: 汇报工具物理执行的结果
				if reporter != nil {
					displayOutput := result.Output
					if len(displayOutput) > 200 {
						displayOutput = displayOutput[:200] + "... (已截断)"
					}
					reporter.OnToolResult(ctx, c.Name, displayOutput, result.IsError)
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

		// 8. 将所有的工具执行结果（Observation）持久化到 Session 中，开启下一轮的复盘与推理
		session.Append(observationMsgs...)
	}

	return nil
}
