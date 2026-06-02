// cmd/claw/feishu_main.go
// 飞书模式入口：启动飞书 HTTP 机器人 + HITL 中间件
// 运行方式: go run cmd/claw/feishu_main.go
package main

import (
	"context"
	"io"
	"log"
	"net/http"
	"os"

	larkevent "github.com/larksuite/oapi-sdk-go/v3/event"

	"github.com/joanneffffff/go-tiny-claw/internal/engine"
	"github.com/joanneffffff/go-tiny-claw/internal/feishu"
	"github.com/joanneffffff/go-tiny-claw/internal/provider"
	"github.com/joanneffffff/go-tiny-claw/internal/schema"
	"github.com/joanneffffff/go-tiny-claw/internal/tools"
)

func main() {
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		log.Fatal("请先导出 ANTHROPIC_API_KEY 环境变量")
	}
	if os.Getenv("FEISHU_APP_ID") == "" || os.Getenv("FEISHU_APP_SECRET") == "" {
		log.Fatal("请先导出 FEISHU_APP_ID 和 FEISHU_APP_SECRET 环境变量")
	}

	workDir, _ := os.Getwd()

	model := os.Getenv("ANTHROPIC_MODEL")
	if model == "" {
		model = "glm-5.1"
	}

	llmProvider := provider.NewCustomClaudeProvider(model)

	registry := tools.NewRegistry()
	registry.Register(tools.NewReadFileTool(workDir))
	registry.Register(tools.NewWriteFileTool(workDir))
	registry.Register(tools.NewBashTool(workDir))

	eng := engine.NewAgentEngine(llmProvider, registry, false).WithPlanMode(false)

	// 为飞书 bot 绑定一个 session
	sessionID := "feishu_hitl_001"
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

	// 注册飞书事件处理路由
	eventDispatcher := bot.GetEventDispatcher()
	http.HandleFunc("/webhook/event", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		req := &larkevent.EventReq{
			Header: r.Header,
			Body:   body,
		}
		resp := eventDispatcher.Handle(context.Background(), req)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(resp.Body))
	})

	port := ":48080"
	log.Printf("🚀 go-tiny-claw 飞书服务端已启动，正在监听 %s 端口\n", port)
	log.Println("📋 已挂载 HITL 中间件，高危操作需人工审批")
	log.Println("💬 飞书对话中发送 approve/reject 命令进行审批")

	err := http.ListenAndServe(port, nil)
	if err != nil {
		log.Fatalf("服务器启动失败: %v", err)
	}
}