# 极简 Go Web Server 项目规划

## 项目目标
搭建一个极简的 Go 语言 Web Server 项目，具备清晰的项目结构和基础功能。

## 架构设计
采用标准库 `net/http` 实现极简 Web Server，项目结构如下：

```
web-server/
├── cmd/
│   └── server/
│       └── main.go          # 应用入口
├── internal/
│   └── handler/
│       └── handler.go       # HTTP 处理器
├── config/
│   └── config.go            # 配置管理
├── go.mod                   # Go 模块定义
└── README.md                # 项目说明
```

## 技术选型
- **HTTP 框架**: 标准库 `net/http` (极简主义)
- **配置管理**: 环境变量 + 默认值
- **路由**: `http.ServeMux` (标准库)
- **日志**: 标准库 `log`

## 核心功能
1. 健康检查端点 `/health`
2. Hello World 端点 `/`
3. 优雅关闭 (Graceful Shutdown)
4. 可配置端口 (默认 8080)

## 实现原则
- 零外部依赖
- 代码简洁清晰
- 符合 Go 标准项目布局
