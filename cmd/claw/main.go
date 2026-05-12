// cmd/claw/main.go
package main

import (
	"context"
	"log"
	"os"

	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	"github.com/larksuite/oapi-sdk-go/v3/ws"

	"github.com/joanneffffff/go-tiny-claw/internal/engine"
	"github.com/joanneffffff/go-tiny-claw/internal/feishu"
	"github.com/joanneffffff/go-tiny-claw/internal/provider"
	"github.com/joanneffffff/go-tiny-claw/internal/tools"
)

func main() {
	// 1. 初始化引擎依赖
	workDir, _ := os.Getwd()

	// 检查必要的环境变量
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		log.Fatal("请先导出 ANTHROPIC_API_KEY 环境变量")
	}
	if os.Getenv("FEISHU_APP_ID") == "" || os.Getenv("FEISHU_APP_SECRET") == "" {
		log.Fatal("请先导出 FEISHU_APP_ID 和 FEISHU_APP_SECRET 环境变量")
	}

	model := os.Getenv("ANTHROPIC_MODEL")
	if model == "" {
		model = "MiniMax-M2.7"
	}
	llmProvider := provider.NewCustomClaudeProvider(model)

	registry := tools.NewRegistry()
	registry.Register(tools.NewReadFileTool(workDir))
	registry.Register(tools.NewWriteFileTool(workDir))
	registry.Register(tools.NewBashTool(workDir))

	// 开启慢思考
	eng := engine.NewAgentEngine(llmProvider, registry, workDir, true)

	// 2. 初始化飞书 Bot 调度器
	bot := feishu.NewFeishuBot(eng)

	// 3. 使用 WebSocket 模式（不需要公网地址）
	d := dispatcher.NewEventDispatcher("", ""). // WebSocket 模式不需要 token/key
		OnP2MessageReceiveV1(bot.HandleMessage())

	wsClient := ws.NewClient(
		os.Getenv("FEISHU_APP_ID"),
		os.Getenv("FEISHU_APP_SECRET"),
		ws.WithEventHandler(d),
		ws.WithLogLevel(larkcore.LogLevelInfo),
		ws.WithAutoReconnect(true),
	)

	log.Printf("🚀 go-tiny-claw 飞书服务端已启动（WebSocket 模式，无需公网地址）\n")

	// Start 会阻塞，自动重连
	if err := wsClient.Start(context.Background()); err != nil {
		log.Fatalf("WebSocket 客户端错误: %v", err)
	}
}
