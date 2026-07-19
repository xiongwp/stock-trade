package main

import (
	"encoding/json"
	"fmt"
	"os"
)

// Config 保存 Alpaca 接入所需的密钥与设置。
// 优先从环境变量读取，其次读取本地 config.json（已 gitignore）。
type Config struct {
	KeyID     string `json:"keyId"`
	SecretKey string `json:"secretKey"`
	Feed      string `json:"feed"` // iex（免费）或 sip（需订阅），默认 iex
}

func loadConfig() (Config, error) {
	c := Config{
		KeyID:     os.Getenv("APCA_API_KEY_ID"),
		SecretKey: os.Getenv("APCA_API_SECRET_KEY"),
		Feed:      os.Getenv("APCA_FEED"),
	}

	// 环境变量缺失时，尝试 config.json 补齐。
	if c.KeyID == "" || c.SecretKey == "" {
		if data, err := os.ReadFile("config.json"); err == nil {
			var fromFile Config
			if err := json.Unmarshal(data, &fromFile); err != nil {
				return c, fmt.Errorf("解析 config.json 失败: %w", err)
			}
			if c.KeyID == "" {
				c.KeyID = fromFile.KeyID
			}
			if c.SecretKey == "" {
				c.SecretKey = fromFile.SecretKey
			}
			if c.Feed == "" {
				c.Feed = fromFile.Feed
			}
		}
	}

	if c.Feed == "" {
		c.Feed = "iex"
	}
	if c.KeyID == "" || c.SecretKey == "" {
		return c, fmt.Errorf("缺少 Alpaca 密钥：请设置环境变量 APCA_API_KEY_ID / APCA_API_SECRET_KEY，或创建 config.json")
	}
	return c, nil
}
