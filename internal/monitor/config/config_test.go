package config

import (
	"math"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// isolateConfigPath points ConfigPath at an empty file inside a temp dir so
// tests never see (or write) the real configuration, and restores it after.
func isolateConfigPath(t *testing.T) {
	t.Helper()
	original := ConfigPath
	ConfigPath = filepath.Join(t.TempDir(), "config.json")
	t.Cleanup(func() { ConfigPath = original })
}

func TestLoad(t *testing.T) {
	isolateConfigPath(t)

	// Set env vars
	_ = os.Setenv("DISCORD_WEBHOOK_URL", "https://discord.webhook.test")
	_ = os.Setenv("TELEGRAM_BOT_TOKEN", "test-token")
	_ = os.Setenv("TELEGRAM_CHAT_ID", "12345")

	cfg := Load()

	if cfg.DiscordWebhook != "https://discord.webhook.test" {
		t.Errorf("Expected Discord webhook from env, got %s", cfg.DiscordWebhook)
	}
	if cfg.TelegramToken != "test-token" {
		t.Errorf("Expected Telegram token from env, got %s", cfg.TelegramToken)
	}
	if cfg.TelegramChatID != "12345" {
		t.Errorf("Expected Telegram chat ID from env, got %s", cfg.TelegramChatID)
	}

	// Clean up
	_ = os.Unsetenv("DISCORD_WEBHOOK_URL")
	_ = os.Unsetenv("TELEGRAM_BOT_TOKEN")
	_ = os.Unsetenv("TELEGRAM_CHAT_ID")
}

func TestLoadBwlimitEnvFallback(t *testing.T) {
	isolateConfigPath(t)

	_ = os.Setenv("BWLIMIT_MBPS", "50")
	defer func() { _ = os.Unsetenv("BWLIMIT_MBPS") }()

	cfg := Load()
	if cfg.BwlimitMbps == nil {
		t.Fatal("Expected BwlimitMbps to be set from env")
	}
	if *cfg.BwlimitMbps != 50 {
		t.Errorf("Expected BwlimitMbps 50 from env, got %d", *cfg.BwlimitMbps)
	}
}

func TestLoadBwlimitUnset(t *testing.T) {
	isolateConfigPath(t)

	_ = os.Unsetenv("BWLIMIT_MBPS")
	cfg := Load()
	// Without a config file or env var, the field stays unset (nil) so that
	// an explicit 0 in the file remains distinguishable from "not configured".
	if cfg.BwlimitMbps != nil {
		t.Errorf("Expected BwlimitMbps to be nil when unset, got %d", *cfg.BwlimitMbps)
	}
}

func TestLoadBwlimitEnvOverflow(t *testing.T) {
	isolateConfigPath(t)

	// Parses as an int on 64-bit but exceeds the int64-safe bytes/sec range.
	_ = os.Setenv("BWLIMIT_MBPS", strconv.Itoa(MaxMbps+1))
	defer func() { _ = os.Unsetenv("BWLIMIT_MBPS") }()

	cfg := Load()
	if cfg.BwlimitMbps != nil {
		t.Errorf("Expected out-of-range BWLIMIT_MBPS to be ignored, got %d", *cfg.BwlimitMbps)
	}
}

func TestMbpsToBps(t *testing.T) {
	for _, tc := range []struct {
		mbps    int
		want    int64
		wantErr bool
	}{
		{0, 0, false},
		{50, 6250000, false},
		{MaxMbps, math.MaxInt64 / 125000 * 125000, false},
		{MaxMbps + 1, 0, true},
		{-1, 0, true},
	} {
		got, err := MbpsToBps(tc.mbps)
		if (err != nil) != tc.wantErr {
			t.Errorf("MbpsToBps(%d) err = %v, wantErr %v", tc.mbps, err, tc.wantErr)
		}
		if err == nil && got != tc.want {
			t.Errorf("MbpsToBps(%d) = %d, want %d", tc.mbps, got, tc.want)
		}
	}
}

func TestSave(t *testing.T) {
	isolateConfigPath(t)

	cfg := &Config{
		DiscordWebhook:   "https://test.webhook",
		SchedulerEnabled: true,
		NormalLimit:      100,
	}

	if err := cfg.Save(); err != nil {
		t.Fatalf("Save to isolated path failed: %v", err)
	}
	if _, err := os.Stat(ConfigPath); err != nil {
		t.Fatalf("Expected config file to exist after Save: %v", err)
	}
}
