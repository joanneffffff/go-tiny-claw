// internal/feishu/approval.go
package feishu

import (
	"context"
	"fmt"
	"log"
	"sync"

	"github.com/joanneffffff/go-tiny-claw/internal/schema"
)

// ApprovalRequest 代表一个待审批的任务
type ApprovalRequest struct {
	TaskID      string        // 唯一标识
	ToolName    string        // 待审批的工具名
	Arguments   string        // 工具参数
	Description string        // 操作描述（供人类理解）
	Ctx         context.Context // 上下文

	// 用于唤醒挂起协程的通道
	responseChan chan *ApprovalResponse
}

// ApprovalResponse 代表人类的审批结果
type ApprovalResponse struct {
	Approved bool   // 是否批准
	Reason   string // 批准/拒绝的理由
}

// ApprovalManager 管理所有待审批的请求
type ApprovalManager struct {
	mu       sync.Mutex
	pending  map[string]*ApprovalRequest // taskID -> request
	taskID   int64                       // 自增 ID 生成器
	reporter *FeishuReporter              // 用于发送审批请求
}

// GlobalApprovalMgr 是全局审批管理器
var GlobalApprovalMgr = NewApprovalManager()

func NewApprovalManager() *ApprovalManager {
	return &ApprovalManager{
		pending: make(map[string]*ApprovalRequest),
	}
}

// SetReporter 设置审批报告器
func (am *ApprovalManager) SetReporter(r *FeishuReporter) {
	am.mu.Lock()
	defer am.mu.Unlock()
	am.reporter = r
}

// RequestApproval 发起审批请求，阻塞等待人类响应
func (am *ApprovalManager) RequestApproval(ctx context.Context, toolName, args, description string) (bool, string, error) {
	// 1. 生成唯一 TaskID
	am.mu.Lock()
	am.taskID++
	taskID := fmt.Sprintf("task_%d", am.taskID)

	// 2. 创建审批请求
	req := &ApprovalRequest{
		TaskID:      taskID,
		ToolName:    toolName,
		Arguments:   args,
		Description: description,
		Ctx:         ctx,
		responseChan: make(chan *ApprovalResponse, 1),
	}

	// 3. 加入待审批队列
	am.pending[taskID] = req
	am.mu.Unlock()

	log.Printf("[Approval] 发起审批请求: %s, 工具: %s\n", taskID, toolName)

	// 4. 发送审批请求到飞书（如果 reporter 已设置）
	if am.reporter != nil {
		am.reporter.sendApprovalRequest(taskID, toolName, args, description)
	}

	// 5. 阻塞等待人类响应
	select {
	case <-ctx.Done():
		// 超时或取消，清理请求
		am.mu.Lock()
		delete(am.pending, taskID)
		am.mu.Unlock()
		return false, "", ctx.Err()

	case resp := <-req.responseChan:
		// 收到人类响应，清理请求
		am.mu.Lock()
		delete(am.pending, taskID)
		am.mu.Unlock()
		return resp.Approved, resp.Reason, nil
	}
}

// ResolveApproval 处理人类的审批响应（由飞书消息回调调用）
func (am *ApprovalManager) ResolveApproval(taskID string, approved bool, reason string) {
	am.mu.Lock()
	req, exists := am.pending[taskID]
	am.mu.Unlock()

	if !exists {
		log.Printf("[Approval] ⚠️ 未找到待审批任务: %s\n", taskID)
		return
	}

	// 发送响应，唤醒挂起的协程
	req.responseChan <- &ApprovalResponse{
		Approved: approved,
		Reason:   reason,
	}
}

// sendApprovalRequest 发送审批请求卡片到飞书
func (r *FeishuReporter) sendApprovalRequest(taskID, toolName, args, description string) {
	msg := fmt.Sprintf(
		"🔴 **审批请求**\n\n"+
			"任务ID: `%s`\n"+
			"工具: `%s`\n"+
			"参数: `%s`\n\n"+
			"操作描述: %s\n\n"+
			"请回复:\n"+
			"- `approve %s` 批准执行\n"+
			"- `reject %s` 拒绝执行",
		taskID, toolName, args, description, taskID, taskID,
	)
	r.sendMsg(msg)
}

// HITLMiddleware 创建人工审批中间件
// 用法: registry.Use(feishu.HITLMiddleware())
func HITLMiddleware() func(ctx context.Context, call schema.ToolCall) (bool, string) {
	// 高危工具清单（需要人工审批）
	dangerousTools := map[string]bool{
		"write_file": true,
		"edit_file":  true,
		"bash":       true,
	}

	return func(ctx context.Context, call schema.ToolCall) (bool, string) {
		// 1. 检查是否为高危工具
		if !dangerousTools[call.Name] {
			// 非高危工具，直接放行
			return true, ""
		}

		// 2. 发起人工审批请求
		description := fmt.Sprintf("即将执行 %s 操作", call.Name)
		approved, reason, err := GlobalApprovalMgr.RequestApproval(
			ctx,
			call.Name,
			string(call.Arguments),
			description,
		)

		if err != nil {
			return false, fmt.Sprintf("审批请求失败: %v", err)
		}

		if approved {
			return true, ""
		}

		return false, fmt.Sprintf("人工审批未通过: %s", reason)
	}
}
