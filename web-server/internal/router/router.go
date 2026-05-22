package router

import (
	"net/http"

	"github.com/example/go-tiny-web/internal/handler"
)

// NewRouter 创建并配置路由
func NewRouter() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", handler.HealthHandler)
	mux.HandleFunc("/hello", handler.HelloHandler)
	return mux
}
