package notification

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Notifier defines the interface for sending notifications
type Notifier interface {
	Send(msg, msgType string) error
}

// Service handles sending notifications to multiple services
type Service struct {
	notifiers []Notifier
}

var notificationClient = &http.Client{Timeout: 15 * time.Second}

// New creates a new notification service
func New(discordWebhook, telegramToken, telegramChatID string) *Service {
	s := &Service{
		notifiers: make([]Notifier, 0),
	}

	if discordWebhook != "" {
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
	jsonBody, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encoding Discord notification: %w", err)
	}

	request, err := http.NewRequest(http.MethodPost, d.WebhookURL, bytes.NewBuffer(jsonBody))
	if err != nil {
		return fmt.Errorf("creating Discord request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	resp, err := notificationClient.Do(request)
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
	endpoint := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", t.BotToken)
	form := url.Values{
		"chat_id": {t.ChatID},
		"text":    {msg},
	}
	request, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("creating Telegram request: %w", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := notificationClient.Do(request)
	if err != nil {
		return fmt.Errorf("telegram request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("telegram returned status %d", resp.StatusCode)
	}

	return nil
}
