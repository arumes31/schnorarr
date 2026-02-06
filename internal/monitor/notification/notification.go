package notification

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
)

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
	if msgType == "ERROR" {
		emoji = "🔴"
	} else if msgType == "SUCCESS" {
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

	resp, err := http.Post(d.WebhookURL, "application/json", bytes.NewBuffer(jsonBody))
	if err != nil {
		return fmt.Errorf("discord request failed: %w", err)
	}
	defer resp.Body.Close()

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
	resp, err := http.PostForm(url, map[string][]string{
		"chat_id": {t.ChatID},
		"text":    {msg},
	})
	if err != nil {
		return fmt.Errorf("telegram request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("telegram returned status %d", resp.StatusCode)
	}

	return nil
}
