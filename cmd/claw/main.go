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
    // 读取环境变量
    if os.Getenv("ANTHROPIC_API_KEY") == "" && os.Getenv("ZHIPU_API_KEY") == "" {
        log.Fatal("请设置 ANTHROPIC_API_KEY 或 ZHIPU_API_KEY 环境变量")
    }

    workDir, _ := os.Getwd()

    // 初始化 Provider
    llmProvider := provider.NewCustomClaudeProvider("MiniMax-M2.7")

    // 初始化真实的 Tool Registry
    registry := tools.NewRegistry()

    // 将真实的 ReadFile 工具挂载到注册表中
    readFileTool := tools.NewReadFileTool(workDir)
    registry.Register(readFileTool)

    // 实例化核心引擎
    enableThinking := os.Getenv("ENABLE_THINKING") == "true"
    eng := engine.NewAgentEngine(llmProvider, registry, workDir, enableThinking)

    // 下发任务
    prompt := "请调用工具读取一下当前工作区目录下 hello.txt 文件的内容，并用一句话向我总结它说了什么。"

    log.Printf("[Config] EnableThinking=%v", enableThinking)
    err := eng.Run(context.Background(), prompt)
    if err != nil {
        log.Fatalf("引擎运行崩溃: %v", err)
    }
}