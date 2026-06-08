package engine

import (
	"context"
	"fmt"
	"log"
	"strings"
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

	// PlanMode 开启计划模式（状态外部化、断点续传）
	PlanMode bool

	// MaxConcurrency 全局最大并发数控制（Semaphore）
	// 限制同时运行的工具数量，防止资源耗尽
	MaxConcurrency int

	// compactor 上下文压缩器，防止大模型 OOM
	compactor *ctxpkg.Compactor

	// recovery 自愈管理器，在工具执行失败时注入救援指南
	recovery *ctxpkg.RecoveryManager

	// injector 提醒注入器，用于死循环探测和干预
	injector *ReminderInjector
}

func NewAgentEngine(p provider.LLMProvider, r tools.Registry, enableThinking bool) *AgentEngine {
	return &AgentEngine{
		provider:       p,
		registry:       r,
		EnableThinking: enableThinking,
		PlanMode:       false, // 默认关闭计划模式
		MaxConcurrency: 5,     // 默认最大并发数为 5
		// 【初始化压缩器】：水位线阈值 3000 字符，保护最近 6 条消息
		compactor: ctxpkg.NewCompactor(3000, 6),
		// 【初始化自愈管理器】：在工具失败时注入救援指南
		recovery: ctxpkg.NewRecoveryManager(),
		// 【初始化提醒注入器】：用于死循环探测和干预
		injector: NewReminderInjector(),
	}
}

// WithPlanMode 设置计划模式（Builder 模式）
func (e *AgentEngine) WithPlanMode(enabled bool) *AgentEngine {
	e.PlanMode = enabled
	return e
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
	log.Printf("[Engine] 计划模式 (Plan Mode): %v\n", e.PlanMode)
	log.Printf("[Engine] 最大并发数 (MaxConcurrency): %d\n", e.MaxConcurrency)

	// 根据当前 Session 的工作区，动态组装最新的 System Prompt
	// 【核心重构】：传入 PlanMode 状态，决定是否注入状态外部化指令
	composer := ctxpkg.NewPromptComposer(session.WorkDir, e.PlanMode)
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

		// 用于存储当前回合的思考内容（Thinking Phase 产生）
		var currentTurnThinkingContent string

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

			// 如果模型输出了思考过程，保存到变量中，供 Action 阶段合并
			if thinkResp.Content != "" {
				log.Printf("🧠 [内部思考 Trace]: %s\n", thinkResp.Content)
				currentTurnThinkingContent = thinkResp.Content
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

		// 【关键修复】：合并 Thinking 和 Action 为一条合法的 Assistant 消息
		// 避免 API 报错 "messages must follow role sequence"
		finalAssistantMsg := schema.Message{
			Role:      schema.RoleAssistant,
			Content:   strings.TrimSpace(currentTurnThinkingContent + "\n" + actionResp.Content),
			ToolCalls: actionResp.ToolCalls,
		}
		session.Append(finalAssistantMsg)

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

				// 【核心拦截与注入】：如果工具执行失败，交由 RecoveryManager 注入救援指南
				finalOutput := result.Output
				if result.IsError {
					finalOutput = e.recovery.AnalyzeAndInject(c.Name, result.Output)
					log.Printf("  -> [ReadOnly] ❌ 注入救援指南\n")
				} else {
					log.Printf("  -> [ReadOnly] ✅ 工具执行成功 (返回 %d 字节)\n", len(result.Output))
				}

				// 【触发 Reporter】: 汇报工具物理执行的结果
				if reporter != nil {
					displayOutput := finalOutput
					if len(displayOutput) > 200 {
						displayOutput = displayOutput[:200] + "... (已截断)"
					}
					reporter.OnToolResult(ctx, c.Name, displayOutput, result.IsError)
				}

				// 回填结果到对应索引（使用注入过 Recovery Hint 的最终结果）
				obsMsg := schema.Message{
					Role:       schema.RoleUser,
					Content:    finalOutput,
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

				// 【核心拦截与注入】：如果工具执行失败，交由 RecoveryManager 注入救援指南
				finalOutput := result.Output
				if result.IsError {
					finalOutput = e.recovery.AnalyzeAndInject(c.Name, result.Output)
					log.Printf("  -> [Write] ❌ 注入救援指南\n")
				} else {
					log.Printf("  -> [Write] ✅ 工具执行成功 (返回 %d 字节)\n", len(result.Output))
				}

				// 【触发 Reporter】: 汇报工具物理执行的结果
				if reporter != nil {
					displayOutput := finalOutput
					if len(displayOutput) > 200 {
						displayOutput = displayOutput[:200] + "... (已截断)"
					}
					reporter.OnToolResult(ctx, c.Name, displayOutput, result.IsError)
				}

				// 回填结果到对应索引（使用注入过 Recovery Hint 的最终结果）
				obsMsg := schema.Message{
					Role:       schema.RoleUser,
					Content:    finalOutput,
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

		// 9. 【核心防线】：在准备进入下一轮之前，进行死循环探测！
		//    如果模型反复用相同参数调用同一个工具，injector 会检测到并注入严厉提醒
		//    取第一个工具调用作为探测对象（简化处理）
		if len(actionResp.ToolCalls) > 0 {
			firstCall := actionResp.ToolCalls[0]
			firstResult := observationMsgs[0]

			// 从 observationMsgs 中提取 ToolResult 信息
			var toolResult schema.ToolResult
			toolResult.Output = firstResult.Content
			toolResult.IsError = strings.Contains(firstResult.Content, "[系统救援指南]")

			reminderMsg := e.injector.CheckAndInject(firstCall, toolResult)
			if reminderMsg != nil {
				// 如果触发了干预规则，将这条严厉的提醒作为 User 消息强制追加到 Session 的最末尾
				// 大模型在下一轮被唤醒时，第一眼就会看到这句话，从而打破局部执念
				log.Printf("[Engine] 🚨 检测到死循环模式，注入干预提醒！\n")
				session.Append(*reminderMsg)
			}
		}
	}

	return nil
}

// RunSub 是专为 Subagent 拉起的一次性受限循环。
// 它不依赖外部 Session，打完就跑。
// Reporter：为了让用户在终端看到子智能体的工作轨迹，我们将主线程的 Reporter 透传进来，并打上特殊标记。
func (e *AgentEngine) RunSub(ctx context.Context, taskPrompt string, readOnlyRegistry tools.Registry, reporter any) (string, error) {

	// 【核心优化】：子智能体极其容易偷懒。我们必须在 System Prompt 中严厉警告它必须使用工具！
	contextHistory := []schema.Message{
		{
			Role: schema.RoleSystem,
			Content: `你是一个专门负责深度探索的探路者 (Explorer Subagent)。
你的任务是根据主架构师的指令，在当前工作区内仔细阅读代码、查阅日志，搜集足够的信息。

【核心纪律】
1. 你必须、且只能依靠内置工具（如 bash 的 find/grep，或 read_file）去寻找答案。绝对不允许凭空捏造或猜测！
2. 如果你没有找到确切的答案，你必须继续使用工具深入搜索。
3. 当且仅当你找到了确切的线索后，停止调用工具，直接输出一段纯文本作为你的终极汇报。主架构师会根据你的汇报来做下一步决策。`,
		},
		{
			Role:    schema.RoleUser,
			Content: taskPrompt,
		},
	}

	// 限制子智能体最多只能跑 10 个 Turn，防止它自己卡死
	const maxSubTurns = 10
	turnCount := 0

	for {
		turnCount++
		if turnCount > maxSubTurns {
			return "", fmt.Errorf("子智能体探索过于深入，超过 %d 轮被强制召回，请主 Agent 给它更明确的指令", maxSubTurns)
		}

		// 【驾驭底线】：子智能体仅能获取传入的只读工具注册表
		availableTools := readOnlyRegistry.GetAvailableTools()

		compactedContext := e.compactor.Compact(contextHistory)

		// 子任务要求急速响应，强制关闭主体的慢思考，直接预测行动
		actionResp, err := e.provider.Generate(ctx, compactedContext, availableTools)
		if err != nil {
			return "", fmt.Errorf("子智能体推理失败: %w", err)
		}

		contextHistory = append(contextHistory, *actionResp)

		// 【核心退出条件】：子智能体一旦不调用工具了，说明它做好了总结汇报
		if len(actionResp.ToolCalls) == 0 {
			// 直接将它的这段汇报内容剥离出来返回给上层
			return actionResp.Content, nil
		}

		// 执行只读工具的并发循环
		observationMsgs := make([]schema.Message, len(actionResp.ToolCalls))
		var wg sync.WaitGroup

		for i, toolCall := range actionResp.ToolCalls {
			wg.Add(1)
			go func(idx int, call schema.ToolCall) {
				defer wg.Done()

				// 【可视化的关键】：让终端用户看到 Subagent 正在干嘛
				var r Reporter
				if reporter != nil {
					r = reporter.(Reporter)
					r.OnToolCall(ctx, fmt.Sprintf("[Subagent] %s", call.Name), string(call.Arguments))
				}

				result := readOnlyRegistry.Execute(ctx, call)

				finalOutput := result.Output
				if result.IsError {
					finalOutput = e.recovery.AnalyzeAndInject(call.Name, result.Output)
				}

				if reporter != nil {
					display := finalOutput
					if len(display) > 200 {
						display = display[:200] + "... (已截断)"
					}
					r.OnToolResult(ctx, fmt.Sprintf("[Subagent] %s", call.Name), display, result.IsError)
				}

				observationMsgs[idx] = schema.Message{
					Role:       schema.RoleUser,
					Content:    finalOutput,
					ToolCallID: call.ID,
				}
			}(i, toolCall)
		}

		wg.Wait()
		contextHistory = append(contextHistory, observationMsgs...)
	}
}
