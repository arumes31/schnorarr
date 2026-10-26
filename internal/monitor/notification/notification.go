package notification

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// httpClient has a timeout and never follows redirects, so a webhook cannot
// bounce requests to internal services.
var httpClient = &http.Client{
	Timeout: 15 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

var discordHosts = map[string]bool{
	"discord.com":        true,
	"discordapp.com":     true,
	"ptb.discord.com":    true,
	"canary.discord.com": true,
}

// ValidateDiscordWebhook accepts an empty URL (disabled) or an
// https://discord.com/api/webhooks/... URL.
func ValidateDiscordWebhook(raw string) error {
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return errors.New("not a valid URL")
	}
	if u.Scheme != "https" || u.User != nil || u.Port() != "" || !discordHosts[strings.ToLower(u.Hostname())] ||
		!strings.HasPrefix(u.Path, "/api/webhooks/") {
		return errors.New("must be a Discord webhook (https://discord.com/api/webhooks/...)")
	}
	return nil
}

// Notifier defines the interface for sending notifications
type Notifier interface {
	Send(msg, msgType string) error
}

// Service handles sending notifications to multiple services
type Service struct {
	notifiers []Notifier
}

// New creates a new notification service
func New(discordWebhook, telegramToken, telegramChatID string) *Service {
	s := &Service{
		notifiers: make([]Notifier, 0),
	}

	if err := ValidateDiscordWebhook(discordWebhook); err != nil {
		log.Printf("Ignoring Discord webhook: %v", err)
	} else if discordWebhook != "" {
		s.notifiers = append(s.notifiers, &Discord{WebhookURL: discordWebhook})
	}

	if telegramToken != "" && telegramChatID != "" {
		s.notifiers = append(s.notifiers, &Telegram{
			BotToken: telegramToken,
			ChatID:   telegramChatID,
		})
	}

	return s
}

// Send sends a notification to all configured services
func (s *Service) Send(msg, msgType string) {
	emoji := "🔵"
	switch msgType {
	case "ERROR":
		emoji = "🔴"
	case "SUCCESS":
		emoji = "🟢"
	}
	fullMsg := fmt.Sprintf("[schnorarr] %s %s", emoji, msg)

	for _, notifier := range s.notifiers {
		if err := notifier.Send(fullMsg, msgType); err != nil {
			log.Printf("Notification Error: %v", err)
		}
	}
}

// Discord notifier
type Discord struct {
	WebhookURL string
}

func (d *Discord) Send(msg, msgType string) error {
	payload := map[string]string{"content": msg}
	jsonBody, _ := json.Marshal(payload)

	resp, err := httpClient.Post(d.WebhookURL, "application/json", bytes.NewBuffer(jsonBody))
	if err != nil {
		return fmt.Errorf("discord request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("discord returned status %d", resp.StatusCode)
	}

	return nil
}

// Telegram notifier
type Telegram struct {
	BotToken string
	ChatID   string
}

func (t *Telegram) Send(msg, msgType string) error {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", t.BotToken)
	resp, err := httpClient.PostForm(url, map[string][]string{
		"chat_id": {t.ChatID},
		"text":    {msg},
	})
	if err != nil {
		return fmt.Errorf("telegram request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("telegram returned status %d", resp.StatusCode)
	}

	return nil
}
