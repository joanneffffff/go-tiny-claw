// cmd/claw/main.go
package main

import (
	"context"
	"log"
	"os"

	"github.com/joanneffffff/go-tiny-claw/internal/engine"
	"github.com/joanneffffff/go-tiny-claw/internal/provider"
	"github.com/joanneffffff/go-tiny-claw/internal/tools"
)

func main() {
	// 检查必要的环境变量
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		log.Fatal("请先导出 ANTHROPIC_API_KEY 环境变量")
	}

	workDir, _ := os.Getwd()

	model := os.Getenv("ANTHROPIC_MODEL")
	if model == "" {
		model = "MiniMax-M2.7"
	}
	llmProvider := provider.NewCustomClaudeProvider(model)

	registry := tools.NewRegistry()
	registry.Register(tools.NewReadFileTool(workDir))
	registry.Register(tools.NewWriteFileTool(workDir))
	registry.Register(tools.NewBashTool(workDir))

	// 实例化引擎，开启慢思考
	eng := engine.NewAgentEngine(llmProvider, registry, workDir, true)

	// 注入终端输出器
	reporter := engine.NewTerminalReporter()

	prompt := `
	我需要在当前目录下新建一个 ping.go，提供一个简单的 http ping 接口。
	写完之后，帮我把代码用 git 提交一下。
	`

	err := eng.Run(context.Background(), prompt, reporter)
	if err != nil {
		log.Fatalf("引擎运行崩溃: %v", err)
	}

	// ========== WebSocket 模式（已注释）==========
	// 如需启用飞书机器人，取消以下注释并注释上方的 CLI 模式代码
	//
	// import (
	//     larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	//     "github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	//     "github.com/larksuite/oapi-sdk-go/v3/ws"
	//     "github.com/joanneffffff/go-tiny-claw/internal/feishu"
	// )
	//
	// if os.Getenv("FEISHU_APP_ID") == "" || os.Getenv("FEISHU_APP_SECRET") == "" {
	//     log.Fatal("请先导出 FEISHU_APP_ID 和 FEISHU_APP_SECRET 环境变量")
	// }
	//
	// bot := feishu.NewFeishuBot(eng)
	// d := dispatcher.NewEventDispatcher("", "").
	//     OnP2MessageReceiveV1(bot.HandleMessage())
	//
	// wsClient := ws.NewClient(
	//     os.Getenv("FEISHU_APP_ID"),
	//     os.Getenv("FEISHU_APP_SECRET"),
	//     ws.WithEventHandler(d),
	//     ws.WithLogLevel(larkcore.LogLevelInfo),
	//     ws.WithAutoReconnect(true),
	// )
	//
	// log.Printf("🚀 go-tiny-claw 飞书服务端已启动（WebSocket 模式）\n")
	// if err := wsClient.Start(context.Background()); err != nil {
	//     log.Fatalf("WebSocket 客户端错误: %v", err)
	// }
}
