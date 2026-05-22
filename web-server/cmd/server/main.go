package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"web-server/config"
	"web-server/internal/handler"
)

func main() {
	// 加载配置
	cfg := config.Load()

	// 创建处理器
	h := handler.New()

	// 注册路由
	mux := http.NewServeMux()
	mux.HandleFunc("/", h.Hello)
	mux.HandleFunc("/health", h.Health)

	// 创建服务器
	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.Port),
		Handler: mux,
	}

	// 启动服务器 (goroutine)
	go func() {
		log.Printf("Server starting on port %d...", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed: %v", err)
		}
	}()

	// 等待中断信号
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")

	// 优雅关闭 (5秒超时)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("Server shutdown error: %v", err)
	}

	log.Println("Server stopped")
}
