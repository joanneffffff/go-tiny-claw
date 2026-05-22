package handler

import (
	"encoding/json"
	"net/http"
)

// Handler HTTP 处理器集合
type Handler struct{}

// New 创建新的处理器
func New() *Handler {
	return &Handler{}
}

// Hello 处理根路径请求
func (h *Handler) Hello(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"message": "Hello, World!",
	})
}

// Health 健康检查端点
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "ok",
	})
}
