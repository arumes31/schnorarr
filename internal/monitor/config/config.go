package config

import (
	"encoding/json"
	"log"
	"os"
	"strconv"
)

const ConfigPath = "/config/config.json"

// Config represents the application configuration
type Config struct {
	DiscordWebhook string `json:"discord_webhook"`
	TelegramToken  string `json:"telegram_token"`
	TelegramChatID string `json:"telegram_chat_id"`

	// Scheduler
	SchedulerEnabled bool   `json:"scheduler_enabled"`
	QuietStart       string `json:"quiet_start"`  // HH:MM
	QuietEnd         string `json:"quiet_end"`    // HH:MM
	QuietLimit       int    `json:"quiet_limit"`  // Mbps
	NormalLimit      int    `json:"normal_limit"` // Mbps (Restore to this)

	// Sync
	// BwlimitMbps is the global bandwidth limit in Mbps. nil = unset
	// (fall back to BWLIMIT_MBPS env); a explicit 0 means unlimited and
	// wins over the env var.
	BwlimitMbps *int `json:"bwlimit_mbps,omitempty"`
}

// Load reads configuration from file and falls back to environment variables
func Load() *Config {
	cfg := &Config{}

	// Try to load from file
	file, err := os.ReadFile(ConfigPath)
	if err == nil {
		if err := json.Unmarshal(file, cfg); err != nil {
			log.Printf("Failed to unmarshal config: %v", err)
		}
	}

	// Fallback to Env if empty
	if cfg.DiscordWebhook == "" {
		cfg.DiscordWebhook = os.Getenv("DISCORD_WEBHOOK_URL")
	}
	if cfg.TelegramToken == "" {
		cfg.TelegramToken = os.Getenv("TELEGRAM_BOT_TOKEN")
	}
	if cfg.TelegramChatID == "" {
		cfg.TelegramChatID = os.Getenv("TELEGRAM_CHAT_ID")
	}
	if cfg.BwlimitMbps == nil {
		if bwStr := os.Getenv("BWLIMIT_MBPS"); bwStr != "" {
			if bw, err := strconv.Atoi(bwStr); err == nil && bw >= 0 {
				cfg.BwlimitMbps = &bw
			}
		}
	}

	return cfg
}

// Save writes configuration to file
func (c *Config) Save() error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(ConfigPath, data, 0644)
}
