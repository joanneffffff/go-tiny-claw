// cmd/claw/main.go
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/joanneffffff/go-tiny-claw/internal/engine"
	"github.com/joanneffffff/go-tiny-claw/internal/provider"
	"github.com/joanneffffff/go-tiny-claw/internal/schema"
	"github.com/joanneffffff/go-tiny-claw/internal/tools"
)

func main() {
	// 通过命令行参数接收用户的 prompt
	promptPtr := flag.String("prompt", "", "要交给 Agent 执行的任务描述")
	flag.Parse()

	if *promptPtr == "" {
		fmt.Println("用法: go run cmd/claw/main.go -prompt \"你的任务指令\"")
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

	// 挂载基础工具
	registry := tools.NewRegistry()
	registry.Register(tools.NewReadFileTool(workDir))
	registry.Register(tools.NewWriteFileTool(workDir))
	registry.Register(tools.NewBashTool(workDir))

	// 实例化引擎并开启计划模式 (PlanMode=true)
	eng := engine.NewAgentEngine(llmProvider, registry, false).WithPlanMode(true)
	reporter := engine.NewTerminalReporter()

	// 使用固定的 SessionID，以便在多次运行之间共享"短期工作记忆"
	// (在真实的 CLI 中，如果进程重启，Session 的内存历史其实是丢失的。
	// 但这正是我们要演示的重点：即便短期内存丢失，只要 TODO.md 还在，任务就能继续！)
	sessionID := "task_plan_mode_01"
	sess := engine.GlobalSessionMgr.GetOrCreate(sessionID, workDir)

	log.Printf("\n>>> 🚀 收到指令: %s\n", *promptPtr)

	// 将用户的 Prompt 压入 Session
	sess.Append(schema.Message{Role: schema.RoleUser, Content: *promptPtr})

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

	// 唤醒引擎执行
	err := eng.Run(ctx, sess, reporter)
	if err != nil {
		log.Fatalf("引擎运行崩溃: %v", err)
	}
}
