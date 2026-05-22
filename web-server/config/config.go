package config

import (
	"os"
	"strconv"
)

// Config 应用配置
type Config struct {
	Port int
}

// Load 从环境变量加载配置
func Load() *Config {
	cfg := &Config{
		Port: 8080, // 默认端口
	}

	if port := os.Getenv("PORT"); port != "" {
		if p, err := strconv.Atoi(port); err == nil {
			cfg.Port = p
		}
	}

	return cfg
}
