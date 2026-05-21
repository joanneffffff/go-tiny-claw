// cmd/claw/main.go
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
	llmProvider := provider.NewCustomClaudeProvider(model)

	workDir, _ := os.Getwd()
	registry := tools.NewRegistry()
	registry.Register(tools.NewReadFileTool(workDir))
	registry.Register(tools.NewWriteFileTool(workDir))
	registry.Register(tools.NewBashTool(workDir))

	// 实例化引擎 (关闭思考模式以提速)
	eng := engine.NewAgentEngine(llmProvider, registry, false)
	reporter := engine.NewTerminalReporter()

	sessionID := "test_oom_protection_001"
	sess := engine.GlobalSessionMgr.GetOrCreate(sessionID, workDir)

	// 发起一个会导致读取大文件的恶意任务
	prompt := `
请帮我执行以下三个步骤：
1. 使用 bash 执行 echo "开始排查日志"
2. 使用 read_file 工具读取当前目录下的巨大文件 mock_log.txt
3. 使用 bash 执行 date 命令获取当前时间，并告诉我任务全部完成。
`

	sess.Append(schema.Message{Role: schema.RoleUser, Content: prompt})

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	err := eng.Run(ctx, sess, reporter)
	if err != nil {
		log.Fatalf("引擎运行崩溃: %v", err)
	}
}
