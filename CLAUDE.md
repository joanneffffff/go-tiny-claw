# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build and Run

```bash
# Build the binary
go build -o main ./cmd/claw/

# Run directly (requires environment variables)
go run ./cmd/claw/

# Run via Docker (WebSocket mode, no public IP needed)
docker build -t go-tiny-claw .
docker run -d --env-file .env --name go-tiny-claw go-tiny-claw
```

Required environment variables:
- `ANTHROPIC_API_KEY` - API key for the LLM provider
- `ANTHROPIC_BASE_URL` - Base URL for the API endpoint
- `ANTHROPIC_MODEL` - Model name (default: glm-5.1)
- `ENABLE_THINKING` - Enable two-phase ReAct loop (default: false)
- `FEISHU_APP_ID` - Feishu bot app ID
- `FEISHU_APP_SECRET` - Feishu bot app secret

## Architecture

This is a minimal AI agent harness ("tiny claw") that implements a ReAct-style agent loop with tool calling capabilities.

### Core Components

**AgentEngine** (`internal/engine/loop.go`) - The main loop that drives the agent:
- Two-phase ReAct loop: optional "thinking" phase (no tools) followed by "action" phase (with tools)
- Maintains conversation history in `[]schema.Message`
- Terminates when the model returns no tool calls

**LLMProvider** (`internal/provider/interface.go`) - Interface abstraction for LLM backends:
- `Generate(ctx, messages, tools)` - Single method contract
- Two implementations: `ClaudeProvider` (Anthropic SDK) and `OpenAIProvider` (Zhipu/OpenAI compatible)

**Registry** (`internal/tools/registry.go`) - Tool registration and execution router:
- `Register(tool)` - Mount tools by name
- `Execute(ctx, call)` - Route tool calls and return results
- Tools implement `BaseTool` interface: `Name()`, `Definition()`, `Execute(ctx, args)`

**Schema** (`internal/schema/message.go`) - Shared data structures:
- `Message` - Conversation turn with role, content, tool calls
- `ToolCall` / `ToolResult` - Tool invocation and response
- `ToolDefinition` - JSON Schema for tool parameters

### Built-in Tools

All tools are sandboxed to `workDir` for security:
- `read_file` - Read file contents (truncated at 8000 bytes)
- `write_file` - Create/overwrite files (auto-creates parent directories)
- `bash` - Execute shell commands (30s timeout, output truncated at 8000 bytes)

### Feishu Integration

The bot uses **WebSocket mode** to connect to Feishu servers:
- No public IP or port forwarding required
- Suitable for running in internal network environments
- Auto-reconnects on connection failure

### Safety Mechanisms

The harness implements multiple safety layers documented in README.md:
- Work directory boundary enforcement (path traversal prevention)
- Output truncation to prevent context overflow
- Timeout controls on bash commands
- Self-correction handling (errors returned as strings, not Go errors)

## Key Design Patterns

**Provider Translation**: Each provider translates between internal `schema.Message` and SDK-specific message formats. This allows swapping LLM backends without changing the engine.

**Tool Sandbox**: Tools receive `workDir` at construction time and must validate paths stay within bounds.

**Error as Observation**: Tool errors are returned as string output (not Go errors) to let the model self-correct rather than crashing the loop.
