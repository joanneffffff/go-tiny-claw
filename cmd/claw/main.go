// cmd/claw/main.go
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"

	"github.com/joanneffffff/go-tiny-claw/internal/engine"
	"github.com/joanneffffff/go-tiny-claw/internal/feishu"
	"github.com/joanneffffff/go-tiny-claw/internal/provider"
	"github.com/joanneffffff/go-tiny-claw/internal/schema"
	"github.com/joanneffffff/go-tiny-claw/internal/tools"
)

// loadEnvFromFile 从 .env 文件加载环境变量
func loadEnvFromFile(path string) {
	file, err := os.Open(path)
	if err != nil {
		return // 文件不存在，静默忽略
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// 跳过空行和注释
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// 解析 KEY=VALUE
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			// 只有当环境变量不存在时才设置
			if os.Getenv(key) == "" {
				os.Setenv(key, value)
			}
		}
	}
}

func init() {
	// 自动从工作目录加载 .env 文件
	if workDir, err := os.Getwd(); err == nil {
		loadEnvFromFile(workDir + "/.env")
	}
}

func main() {
	// 通过命令行参数选择模式
	mode := flag.String("mode", "cli", "运行模式: cli (命令行) 或 feishu (飞书机器人)")
	promptPtr := flag.String("prompt", "", "要交给 Agent 执行的任务描述 (cli 模式)")
	flag.Parse()

	if *mode == "cli" {
		runCLIMode(*promptPtr)
	} else if *mode == "feishu" {
		runFeishuMode()
	} else {
		fmt.Println("用法: go run cmd/claw/main.go -mode [cli|feishu]")
		fmt.Println("  -mode cli     : 命令行模式 (需要 -prompt)")
		fmt.Println("  -mode feishu  : 飞书 WebSocket 机器人模式")
		os.Exit(1)
	}
}

// runCLIMode CLI 模式
func runCLIMode(prompt string) {
	if prompt == "" {
		fmt.Println("用法: go run cmd/claw/main.go -mode cli -prompt \"你的任务指令\"")
		os.Exit(1)
	}

	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		log.Fatal("请先导出 ANTHROPIC_API_KEY 环境变量")
	}

	model := os.Getenv("ANTHROPIC_MODEL")
	if model == "" {
		model = "glm-5.1"
	}

	workDir, _ := os.Getwd()
	llmProvider := provider.NewCustomClaudeProvider(model)

	registry := tools.NewRegistry()
	registry.Register(tools.NewReadFileTool(workDir))
	registry.Register(tools.NewWriteFileTool(workDir))
	registry.Register(tools.NewBashTool(workDir))

	eng := engine.NewAgentEngine(llmProvider, registry, false).WithPlanMode(true)
	reporter := engine.NewTerminalReporter()

	sessionID := "task_plan_mode_01"
	sess := engine.GlobalSessionMgr.GetOrCreate(sessionID, workDir)

	log.Printf("\n>>> 🚀 收到指令: %s\n", prompt)

	sess.Append(schema.Message{Role: schema.RoleUser, Content: prompt})

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

	err := eng.Run(ctx, sess, reporter)
	if err != nil {
		log.Fatalf("引擎运行崩溃: %v", err)
	}
}

// runFeishuMode 飞书 WebSocket 模式
func runFeishuMode() {
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		log.Fatal("请先导出 ANTHROPIC_API_KEY 环境变量")
	}
	if os.Getenv("FEISHU_APP_ID") == "" || os.Getenv("FEISHU_APP_SECRET") == "" {
		log.Fatal("请先导出 FEISHU_APP_ID 和 FEISHU_APP_SECRET 环境变量")
	}

	appID := os.Getenv("FEISHU_APP_ID")
	appSecret := os.Getenv("FEISHU_APP_SECRET")

	model := os.Getenv("ANTHROPIC_MODEL")
	if model == "" {
		model = "glm-5.1"
	}

	workDir, _ := os.Getwd()
	llmProvider := provider.NewCustomClaudeProvider(model)

	registry := tools.NewRegistry()
	registry.Register(tools.NewReadFileTool(workDir))
	registry.Register(tools.NewWriteFileTool(workDir))
	registry.Register(tools.NewBashTool(workDir))

	eng := engine.NewAgentEngine(llmProvider, registry, false).WithPlanMode(false)

	// 为飞书 bot 绑定一个 session
	sessionID := "feishu_websocket_001"
	sess := engine.GlobalSessionMgr.GetOrCreate(sessionID, workDir)

	// 创建飞书机器人
	bot := feishu.NewFeishuBot(eng, sess)

	// 【核心注入】注册 HITL 安全拦截 Middleware
	registry.Use(func(ctx context.Context, call schema.ToolCall) (bool, string) {
		argsStr := string(call.Arguments)

		// 检查是否命中高危特征库
		if feishu.IsDangerousCommand(call.Name, argsStr) {
			log.Printf("[HITL] ⚠️ 检测到高危操作: %s, 参数: %s\n", call.Name, argsStr)
			log.Printf("[HITL] 已发送审批请求到飞书，等待人类决策...\n")

			// 挂起当前协程，发送消息给飞书，等待人类审批
			allowed, reason := feishu.GlobalApprovalMgr.WaitForApproval(
				call.ID, call.Name, argsStr, bot.Reporter(),
			)

			if !allowed {
				log.Printf("[HITL] 🚫 操作被拒绝: %s\n", reason)
				return false, reason
			}
			log.Printf("[HITL] ✅ 操作被批准\n")
			return true, ""
		}

		// 非高危操作，直接放行
		return true, ""
	})

	// 创建 WebSocket 客户端
	wsClient := larkws.NewClient(appID, appSecret,
		larkws.WithEventHandler(bot.GetEventDispatcher()),
		larkws.WithAutoReconnect(true),
		larkws.WithLogLevel(1), // Info level
	)

	// 设置生命周期回调
	wsClient.SetOnReady(func() {
		log.Println("🟢 飞书 WebSocket 连接已建立，机器人已上线！")
	})

	wsClient.SetOnDisconnected(func() {
		log.Println("🔴 飞书 WebSocket 连接已断开")
	})

	wsClient.SetOnReconnecting(func() {
		log.Println("🟡 飞书 WebSocket 正在重连...")
	})

	wsClient.SetOnReconnected(func() {
		log.Println("🟢 飞书 WebSocket 已重连成功")
	})

	wsClient.SetOnError(func(err error) {
		log.Printf("🔴 飞书 WebSocket 错误: %v\n", err)
	})

	log.Println("🚀 go-tiny-claw 飞书机器人启动中...")
	log.Println("📋 已挂载 HITL 中间件，高危操作需人工审批")
	log.Println("💬 飞书对话中发送 approve/reject 命令进行审批")
	log.Println("🔌 正在连接飞书 WebSocket...")

	// 启动 WebSocket 连接
	ctx := context.Background()
	go func() {
		if err := wsClient.Start(ctx); err != nil {
			log.Fatalf("WebSocket 启动失败: %v", err)
		}
	}()

	// 等待退出信号
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Println("🛑 正在关闭飞书机器人...")
	wsClient.Close()
	log.Println("👋 已退出")
}