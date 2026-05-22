# Web Server

一个极简的 Go HTTP Web 服务器，支持优雅关闭。

## 功能特性

- ✅ HTTP 服务器基础功能
- ✅ JSON 响应格式
- ✅ 健康检查端点
- ✅ 环境变量配置
- ✅ 优雅关闭支持

## 项目结构

```
web-server/
├── cmd/
│   └── server/
│       └── main.go          # 主程序入口
├── config/
│   └── config.go            # 配置管理
├── internal/
│   └── handler/
│       └── handler.go       # HTTP 处理器
├── go.mod
└── README.md
```

## 快速开始

### 运行服务器

```bash
cd web-server
go run cmd/server/main.go
```

### 自定义端口

```bash
PORT=3000 go run cmd/server/main.go
```

## API 端点

### GET /
返回欢迎消息

**响应示例:**
```json
{
  "message": "Hello, World!"
}
```

### GET /health
健康检查端点

**响应示例:**
```json
{
  "status": "ok"
}
```

## 配置

| 环境变量 | 说明 | 默认值 |
|---------|------|--------|
| PORT    | 服务器端口 | 8080 |

## 优雅关闭

服务器支持优雅关闭，接收到 SIGINT 或 SIGTERM 信号后会等待 5 秒让现有请求完成。

## 构建与部署

### 构建

```bash
go build -o bin/server cmd/server/main.go
```

### 运行二进制文件

```bash
./bin/server
```

## 许可证

MIT
