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
│   ├── context/             # Token 监控、Prompt 动态组装、Compactor
│   ├── tools/               # 工具注册表、Middleware、基础极简工具
│   ├── memory/              # 基于文件系统的记忆状态存取
│   └── feishu/              # 飞书机器人交互回调
├── go.mod
├── go.sum
├── Dockerfile
├── .env                 # 环境变量（不提交到 git）
└── README.md
```

## 快速启动（Docker）

### 1. 构建镜像

```bash
docker build -t go-tiny-claw .
```

### 2. 配置环境变量

创建 `.env` 文件：

```bash
# LLM 配置
ANTHROPIC_API_KEY=your_api_key
ANTHROPIC_BASE_URL=https://api.sfkey.cn/
ANTHROPIC_MODEL=glm-5.1
ENABLE_THINKING=false

# 飞书机器人配置
FEISHU_APP_ID=cli_xxxxxxxxxxxxx
FEISHU_APP_SECRET=xxxxxxxxxxxxxxxx
```

### 3. 运行

```bash
# WebSocket 模式（推荐，无需公网地址）
docker run -d --env-file .env --name go-tiny-claw go-tiny-claw

# 查看日志
docker logs -f go-tiny-claw
```

> **注意**：项目使用 WebSocket 模式连接飞书，无需公网 IP 或端口转发，适合内网环境运行。

### 4. 本地开发（持久化容器）

创建一个持久化的开发容器，代码挂载本地目录，Docker Desktop 启动时自动运行：

```bash
# Windows (Git Bash) 需要加 MSYS_NO_PATHCONV=1
MSYS_NO_PATHCONV=1 docker run -d --name go-tiny-claw \
  -v "D:/projects/go-tiny-claw:/app" \
  -w /app \
  --restart=always \
  golang:1.26-alpine \
  tail -f /dev/null

# 复制环境变量到容器
docker cp .env go-tiny-claw:/app/.env
```

以后直接使用：

```bash
# 进入容器
docker exec -it go-tiny-claw sh

# 运行 Agent（普通模式）
docker exec go-tiny-claw go run ./cmd/claw/ -prompt "你的任务"

# 运行 Agent（计划模式）
docker exec go-tiny-claw go run ./cmd/claw/ -prompt "你的任务"

# 编译
docker exec go-tiny-claw go build -o claw ./cmd/claw/
```

---

## 核心功能

### 1. Session 管理（多用户隔离）

每个用户/群聊拥有独立的 Session，历史记录互不干扰：

```go
// 获取或创建 Session
sessionA := engine.GlobalSessionMgr.GetOrCreate("chat_front_001", "/tmp/project_front")
sessionB := engine.GlobalSessionMgr.GetOrCreate("chat_back_002", "/tmp/project_back")

// Session 隔离：Session A 看不到 Session B 的历史
```

**Working Memory 截断**：只保留最近 N 条消息，防止上下文爆炸：

```go
workingMemory := session.GetWorkingMemory(20)  // 只取最近 20 条
```

### 2. Compactor（上下文压缩，防 OOM）

当上下文长度超过阈值时，自动压缩早期历史：

```go
compactor := ctxpkg.NewCompactor(3000, 6)  // 阈值 3000 字符，保护最近 6 条

compactedContext := compactor.Compact(contextHistory)
```

**双重防线策略**：

| 防线 | 范围 | 策略 |
|------|------|------|
| 第一道 | 远期历史 | 完全掩码：`...[早期内容已清理]...` |
| 第二道 | 短期保护区 | 掐头去尾：保留首尾各 500 字符 |

**关键设计**：压缩只影响"发给大模型的临时上下文"，Session 中仍保存全量数据。

### 3. Plan Mode（计划模式，断点续传）

开启计划模式后，Agent 会将任务规划和进度持久化到文件：

```go
eng := engine.NewAgentEngine(provider, registry, false).WithPlanMode(true)
```

**工作流程**：

1. **STEP 1: 环境嗅探** - 检查 `PLAN.md` 和 `TODO.md` 是否存在
2. **分支 A（全新任务）** - 创建规划文档
3. **分支 B（断点续传）** - 读取 TODO.md，找到第一个未完成的任务继续执行
4. **STEP 2: 单步执行 + 实时打勾** - 每完成一步立即更新 TODO.md

**示例**：

```bash
# 第一次运行（创建项目）
docker exec go-tiny-claw go run ./cmd/claw/ -prompt "搭建一个 Web Server"

# 中途停止后，再次运行（断点续传）
docker exec go-tiny-claw go run ./cmd/claw/ -prompt "搭建一个 Web Server"
# Agent 会检测到 TODO.md 存在，从上次中断的地方继续
```

### 4. Thinking Phase（慢思考模式）

开启后，Agent 会先"思考"再行动：

```go
eng := engine.NewAgentEngine(provider, registry, true)  // 第三个参数开启思考模式
```

**两阶段 ReAct 循环**：

1. **Phase 1: Thinking** - 剥夺工具，强制模型输出推理过程
2. **Phase 2: Action** - 恢复工具，模型根据推理结果执行

### 5. 并发控制

- **只读工具**：并发执行
- **涉写工具**：串行执行（加锁）
- **全局并发数**：Semaphore 控制

```go
eng := engine.NewAgentEngine(provider, registry, false).WithMaxConcurrency(10)
```

### 6. RecoveryManager（自愈机制）

当工具执行失败时，RecoveryManager 会分析错误特征并注入救援指南，引导模型自愈：

```python
# 伪代码示意
工具执行失败 → RecoveryManager 分析错误特征 → 注入救援指南 → 返回给模型 → 模型自愈重试
```

**示例流程**：

```
第一轮：edit_file 调用失败
  错误: "在文件中未找到 old_text"
  
  ↓ RecoveryManager 注入救援指南
  
  返回给模型:
  "在文件中未找到 old_text
  
   [系统救援指南]: 你提供的 old_text 与文件当前内容不一致。
   请先使用 read_file 重新读取文件，获取准确内容后再编辑。"

第二轮：模型根据指南调用 read_file
  获取正确的文件内容

第三轮：模型用正确的 old_text 再次调用 edit_file
  成功！
```

**错误特征匹配表**：

| 工具 | 错误特征 | 救援指南 |
|------|----------|----------|
| `edit_file` | "未找到 old_text" | 先用 `read_file` 重新读取文件 |
| `edit_file` | "匹配到了多处" | 增加 old_text 上下文确保唯一性 |
| `read_file/write_file` | "no such file" | 用 `bash ls/find` 查找正确路径 |
| `read_file/write_file` | "permission denied" | 检查权限或修改其他文件 |
| `bash` | "command not found" | 思考替代命令或安装脚本 |
| `bash` | "timeout" | 常驻服务转入后台执行 |

**关键设计**：错误信息作为 Observation 返回给模型（而非 Go error），让模型理解问题并自我纠正。

**与传统 try-except 的区别**：

| 对比项 | 传统 try-except | RecoveryManager |
|--------|----------------|-----------------|
| 谁决定下一步 | 程序员硬编码处理逻辑 | 模型自己推理决定 |
| 灵活性 | 固定逻辑，只能处理预设情况 | 开放式，模型自由发挥 |
| 适用场景 | 已知的、确定的错误类型 | 不可预测的开放式场景 |

**类比理解**：

```python
# 传统 try-except：程序员决定出错后做什么
try:
    edit_file(old_text="xxx", new_text="yyy")
except NotFoundError:
    read_file("auth.go")  # 程序员硬编码这个动作
    edit_file(old_text="正确的", new_text="yyy")

# RecoveryManager：给模型"提示"，让模型自己决定
result = edit_file(old_text="xxx", new_text="yyy")
if result.is_error:
    # 注入救援指南，返回给模型
    return """
    未找到 old_text
    
    [系统救援指南]: 请先用 read_file 读取文件获取正确内容
    """
    # 模型看到后，自己推理出需要调用 read_file
```

这就是"自愈"的含义——不是程序自动修复，而是模型理解问题后自己修复。

### 7. ReminderInjector（死循环干预）

当模型反复用相同参数调用同一个工具且连续失败时，ReminderInjector 会检测到死循环模式并注入干预消息：

```python
# 伪代码示意
工具失败 → 生成指纹(MD5) → 累计失败次数
              ↓
       连续 ≥ 3 次？ → 注入干预消息 → 强制模型改变策略
```

**工作流程**：

```
第 1 次: read_file("secret_key.txt") 失败 → 计数 = 1，不干预
第 2 次: read_file("secret_key.txt") 失败 → 计数 = 2，不干预
第 3 次: read_file("secret_key.txt") 失败 → 计数 = 3，触发干预！

干预消息:
"[SYSTEM REMINDER 警告] 
你似乎陷入了死循环。你刚刚连续 3 次使用相同参数调用 'read_file' 工具。
请立即停止无效重试！彻底改变你的策略。"

模型响应:
"您说得对，我陷入了死循环。让我改变策略..."
→ 调用 bash ls 查看目录 → 向用户请求帮助
```

**指纹机制**：

| 参数 | 指纹 | 说明 |
|------|------|------|
| `read_file("a.txt")` | `abc123` | 第一个文件 |
| `read_file("b.txt")` | `xyz789` | 不同参数 = 不同指纹 = 独立计数 |
| `read_file("a.txt")` | `abc123` | 相同参数 = 相同指纹 = 累计计数 |

**为什么用指纹而不是工具名？**

```python
# 区分两种情况：
# 1. 相同参数反复尝试 → 死循环 → 触发干预
# 2. 尝试不同文件 → 正常探索 → 不干预

fingerprint = MD5(tool_name + arguments)
```

**关键设计**：

- 指纹 = `MD5(工具名 + 参数)`，精确识别重复行为
- 阈值 = 3 次，平衡"允许尝试"与"防止浪费"
- 干预消息作为 `RoleUser` 返回，保证最高优先级
- 成功后清空计数器，避免误判

---

## 功能组合示例

### 场景 1：多用户聊天机器人

```go
// 每个群聊一个 Session，自动隔离
session := engine.GlobalSessionMgr.GetOrCreate(chatID, workDir)

// 关闭思考模式（快速响应），关闭计划模式（简单问答）
eng := engine.NewAgentEngine(provider, registry, false).WithPlanMode(false)

eng.Run(ctx, session, reporter)
```

### 场景 2：长程任务（断点续传）

```go
// 开启计划模式，任务进度持久化到文件
eng := engine.NewAgentEngine(provider, registry, false).WithPlanMode(true)

// 即使进程重启，只要 TODO.md 还在，任务就能继续
eng.Run(ctx, session, reporter)
```

### 场景 3：复杂推理任务

```go
// 开启思考模式，先规划再行动
eng := engine.NewAgentEngine(provider, registry, true).WithPlanMode(true)

// Thinking + Plan Mode 双重保障
eng.Run(ctx, session, reporter)
```

### 场景 4：大文件处理（防 OOM）

```go
// Compactor 自动压缩超长上下文
// 默认配置：阈值 3000 字符，保护最近 6 条消息

// 读取大文件后，工具返回结果会被自动压缩
// Session 中仍保存完整数据，只是发给模型的上下文被压缩
```

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
