package config

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"strconv"
)

// ConfigPath is a var (not const) so tests can point it at an isolated file.
var ConfigPath = "/config/config.json"

// MaxMbps is the largest limit in Mbps whose bytes/sec value still fits an
// int64 (MbpsToBps multiplies by 125000).
const MaxMbps = math.MaxInt64 / 125000

// MbpsToBps converts megabits/sec to bytes/sec, rejecting negative values
// and values that would overflow int64.
func MbpsToBps(mbps int) (int64, error) {
	if mbps < 0 || mbps > MaxMbps {
		return 0, fmt.Errorf("bandwidth limit %d Mbps out of range (0-%d)", mbps, MaxMbps)
	}
	return int64(mbps) * 125000, nil
}

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
		if chmodErr := os.Chmod(ConfigPath, 0o600); chmodErr != nil {
			log.Printf("Failed to restrict config permissions: %v", chmodErr)
		}
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
			if bw, err := strconv.Atoi(bwStr); err == nil {
				if _, err := MbpsToBps(bw); err == nil {
					cfg.BwlimitMbps = &bw
				} else {
					log.Printf("Ignoring BWLIMIT_MBPS: %v", err)
				}
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
	directory := filepath.Dir(ConfigPath)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}
	// #nosec G302 -- 0700 is the intentionally owner-only directory mode.
	if err := os.Chmod(directory, 0o700); err != nil {
		return fmt.Errorf("restricting config directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".config-*.tmp")
	if err != nil {
		return fmt.Errorf("creating temporary config: %w", err)
	}
	temporaryName := temporary.Name()
	defer func() { _ = os.Remove(temporaryName) }()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("restricting temporary config: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("writing temporary config: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("syncing temporary config: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("closing temporary config: %w", err)
	}
	if err := os.Rename(temporaryName, ConfigPath); err != nil {
		return fmt.Errorf("replacing config: %w", err)
	}
	if err := os.Chmod(ConfigPath, 0o600); err != nil {
		return fmt.Errorf("restricting config: %w", err)
	}
	return nil
}
