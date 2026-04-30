# go-tiny-claw

A tiny claw machine built with Go, integrating LLM providers (Claude, Zhipu) with Feishu (Lark) bot.

## Project Structure

```
go-tiny-claw/
├── cmd/
│   └── claw/
│       └── main.go          # 程序入口
├── internal/
│   ├── engine/              # MainLoop 核心实现
│   ├── provider/            # 大模型接口抽象与具体厂商 SDK 实现
│   ├── context/             # Token 监控、Prompt 动态组装
│   ├── tools/               # 工具注册表、Middleware、基础极简工具
│   ├── memory/              # 基于文件系统的记忆状态存取
│   └── feishu/              # 飞书机器人交互回调
├── go.mod
└── README.md
```

*** 工具输出卸载（Tool Call Offloading）***：工业级 Harness 的主流做法是在工具执行层实现输出卸载策略——当文件或命令输出超过阈值（通常为数千至数万字符）时，Harness 自动将完整内容写入磁盘临时目录，并向模型返回一段“头部预览 + 尾部预览 + 文件路径引用”的摘要消息，例如：“文件过长（共 5000 行，已卸载至 <path>）。以下为首尾预览，如需完整内容请调用 read_file('<path>')。” 通过这种方式，既保留了模型的决策依据，又倒逼其按需局部读取。

*** 结合全局 Context Compaction ***：即使我们在单工具内通过卸载策略放宽了读取限制，在引擎的全局层面，工业级 Harness 依然在 Main Loop 中设有上下文窗口监控机制。当 Token 使用量接近模型上下文窗口的预设阈值（通常为 75%~98%）时，Harness 会触发 Compaction——对历史会话进行压缩（策略有多种，比如智能摘要等)，保留架构决策、未解决的 Bug 等高价值信息，裁剪冗余工具输出，使 Agent 得以在不丢失关键上下文的前提下继续长时运行。关于这道全局级别的终极防 OOM（内存溢出）防线，我们将在专栏的 第 12 讲 为你揭秘。

---

## 安全边界设计

### 工作目录边界（workDir）

`ReadFileTool` 在创建时接收 `workDir` 参数，并将工具锁定在该目录下操作：

```go
readFileTool := tools.NewReadFileTool(workDir)  // e.g. workDir = "/app"
registry.Register(readFileTool)
```

这是**物理边界**：工具执行时，路径会通过 `filepath.Join(workDir, input.Path)` 拼接，并通过 `strings.HasPrefix` 校验是否在 `workDir` 子目录下。`../../etc/passwd` 这类路径穿越攻击会被拦截并返回错误。

### Self-Correction 盲目重试的隐患

当工具返回错误时，模型会尝试修改参数重试（Self-Correction）。但完全依赖模型盲目重试存在以下隐患：

| 隐患 | 描述 |
|------|------|
| **无限循环** | 模型反复用错误参数重试，持续消耗 Token 和 API 费用 |
| **有害操作放大** | 如 `ExecTool` 执行危险命令，"自纠错"名义下探索不同命令 |
| **Token 爆炸** | 错误信息堆积导致 Context 被污染 |
| **置信度陷阱** | 模型越自信越倾向于"再试一次"而非"这个方向错了" |

### 多层防御设计

**第一层：不可重试错误分类**

```go
type NonRetryableError struct{ Msg string }  // 参数格式错、路径穿越、权限不足
type RetryableError struct{ Msg string }    // 网络超时、临时不可用

// 不可重试错误直接终止重试链
if _, ok := err.(NonRetryableError); ok {
    return fmt.Sprintf("[不可重试] %v", err)
}
```

**第二层：Registry 熔断器**

```go
errorCount map[string]int   // 每个工具的连续错误计数
maxErrors := 3              // 超过则熔断，拒绝再调用

if r.errorCount[call.Name] >= r.maxErrors {
    return "Tool '%s' 已连续%d次错误，请停止重试并向用户报告问题"
}
```

**第三层：参数白名单校验**

```go
// 路径穿越检测
if strings.Contains(input.Path, "..") {
    return "", errors.New("路径穿越检测，拒绝重试")
}
// 拼接后二次校验是否在 workDir 内
fullPath := filepath.Join(t.workDir, input.Path)
if !strings.HasPrefix(fullPath, t.workDir) {
    return "", errors.New("路径越界，拒绝重试")
}
```

**第四层：全局重试预算**

```go
type AgentEngine struct {
    maxRetries int  // 整个会话最大重试次数
}

func (e *AgentEngine) Run(...) error {
    retryBudget := e.maxRetries
    for {
        resp, err := e.provider.Generate(ctx, history, tools)
        if err != nil {
            retryBudget--
            if retryBudget <= 0 {
                return fmt.Errorf("重试预算耗尽")
            }
        }
    }
}
```

**第五层：高危操作干运行（Canary）**

```go
// 高危操作不直接执行，而是返回确认请求
type ConfirmRequiredError struct{ Message string }

if tool.dangerLevel == "critical" {
    return "", ConfirmRequiredError{
        Message: "危险操作，请再次确认: " + cmd,
    }
}
```

### 核心设计理念

| 原则 | 说明 |
|------|------|
| **不信任模型盲目重试** | 框架提供有边界的探索空间，防止无限循环 |
| **错误分类** | `NonRetryableError` 直接终止重试链，`RetryableError` 才允许重试 |
| **熔断器** | 连续 N 次错误后熔断，阻止失控重试 |
| **路径安全** | `workDir` 锁定 + 路径穿越检测，双重校验 |
| **重试预算** | 全局 `retryBudget` 限制总重试次数 |
| **高危 Canary** | 危险操作需要二次确认，不直接执行 |