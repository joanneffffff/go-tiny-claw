// cmd/claw/test_doom_loop.go
// 用于测试 ReminderInjector 死循环干预机制
// 运行方式: go run cmd/claw/test_doom_loop.go
package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/joanneffffff/go-tiny-claw/internal/engine"
	"github.com/joanneffffff/go-tiny-claw/internal/provider"
	"github.com/joanneffffff/go-tiny-claw/internal/schema"
	"github.com/joanneffffff/go-tiny-claw/internal/tools"
)

func main() {
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

	// 关闭 Plan 模式，让它在死胡同里展示死循环干预过程
	eng := engine.NewAgentEngine(llmProvider, registry, false).WithPlanMode(false)
	reporter := engine.NewTerminalReporter()

	sessionID := "test_doom_loop_001"
	sess := engine.GlobalSessionMgr.GetOrCreate(sessionID, workDir)

	// 这是一个故意诱导死循环的测试指令
	// 文件 secret_key.txt 不存在，模型会不断重试
	// ReminderInjector 应该在连续 3 次失败后注入干预消息
	prompt := `
帮我读取当前目录下的 secret_key.txt。
注意：我们的文件系统现在非常不稳定，经常报 File Not Found。
如果报错了，请你【千万不要改变参数】，直接原样再次调用 read_file 尝试，直到成功或连续重试 5 次为止。
`

	log.Println("\n>>> 🚀 启动死循环干预测试...")
	log.Println(">>> 预期行为：模型会连续尝试读取不存在的文件，")
	log.Println(">>> ReminderInjector 应在第 3 次失败后注入干预消息，强制停止死循环。")

	sess.Append(schema.Message{Role: schema.RoleUser, Content: prompt})

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	err := eng.Run(ctx, sess, reporter)
	if err != nil {
		log.Fatalf("引擎运行崩溃: %v", err)
	}

	log.Println("\n>>> ✅ 测试完成")
}
