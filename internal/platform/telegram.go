package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

// TelegramAdapter implements the Telegram platform adapter
type TelegramAdapter struct {
	botToken string
	baseURL  string
	client   *http.Client
	handler  MessageHandler
	offset   int
	stopCh   chan struct{}
}

// TelegramUpdate represents a Telegram update
type TelegramUpdate struct {
	UpdateID int              `json:"update_id"`
	Message  *TelegramMessage `json:"message,omitempty"`
}

// TelegramMessage represents a Telegram message
type TelegramMessage struct {
	MessageID int              `json:"message_id"`
	From      *TelegramUser    `json:"from,omitempty"`
	Chat      *TelegramChat    `json:"chat"`
	Text      string           `json:"text,omitempty"`
	Date      int64            `json:"date"`
}

// TelegramUser represents a Telegram user
type TelegramUser struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name,omitempty"`
	Username  string `json:"username,omitempty"`
}

// TelegramChat represents a Telegram chat
type TelegramChat struct {
	ID    int64  `json:"id"`
	Type  string `json:"type"`
	Title string `json:"title,omitempty"`
}

// TelegramSendMessageRequest represents a send message request
type TelegramSendMessageRequest struct {
	ChatID    int64  `json:"chat_id"`
	Text      string `json:"text"`
	ParseMode string `json:"parse_mode,omitempty"`
}

// NewTelegramAdapter creates a new Telegram adapter
func NewTelegramAdapter(botToken string) *TelegramAdapter {
	return &TelegramAdapter{
		botToken: botToken,
		baseURL:  fmt.Sprintf("https://api.telegram.org/bot%s", botToken),
		client:   &http.Client{Timeout: 30 * time.Second},
		stopCh:   make(chan struct{}),
	}
}

// Name returns the platform name
func (a *TelegramAdapter) Name() string {
	return "telegram"
}

// Start starts the Telegram adapter
func (a *TelegramAdapter) Start(ctx context.Context) error {
	// Verify bot token
	if err := a.verifyBot(); err != nil {
		return fmt.Errorf("failed to verify bot: %w", err)
	}

	log.Println("Telegram adapter started")

	// Start polling
	go a.pollUpdates(ctx)

	return nil
}

// Stop stops the Telegram adapter
func (a *TelegramAdapter) Stop() error {
	close(a.stopCh)
	return nil
}

// SendMessage sends a message to a Telegram chat
func (a *TelegramAdapter) SendMessage(msg *Message) error {
	// Parse chat ID from session ID
	var chatID int64
	if _, err := fmt.Sscanf(msg.SessionID, "telegram:%d", &chatID); err != nil {
		return fmt.Errorf("invalid session ID: %s", msg.SessionID)
	}

	req := TelegramSendMessageRequest{
		ChatID:    chatID,
		Text:      msg.Content,
		ParseMode: "Markdown",
	}

	return a.sendMessage(req)
}

// OnMessage registers a message handler
func (a *TelegramAdapter) OnMessage(handler MessageHandler) {
	a.handler = handler
}

// verifyBot verifies the bot token
func (a *TelegramAdapter) verifyBot() error {
	resp, err := a.client.Get(fmt.Sprintf("%s/getMe", a.baseURL))
	if err != nil {
		return fmt.Errorf("failed to verify bot: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		OK     bool `json:"ok"`
		Result struct {
			Username string `json:"username"`
		} `json:"result"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	if !result.OK {
		return fmt.Errorf("invalid bot token")
	}

	log.Printf("Telegram bot verified: @%s", result.Result.Username)
	return nil
}

// pollUpdates polls for updates
func (a *TelegramAdapter) pollUpdates(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-a.stopCh:
			return
		default:
			updates, err := a.getUpdates()
			if err != nil {
				log.Printf("Failed to get updates: %v", err)
				time.Sleep(time.Second * 5)
				continue
			}

			for _, update := range updates {
				a.processUpdate(update)
			}
		}
	}
}

// getUpdates gets updates from Telegram
func (a *TelegramAdapter) getUpdates() ([]TelegramUpdate, error) {
	url := fmt.Sprintf("%s/getUpdates?offset=%d&timeout=30", a.baseURL, a.offset)

	resp, err := a.client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to get updates: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		OK     bool             `json:"ok"`
		Result []TelegramUpdate `json:"result"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if !result.OK {
		return nil, fmt.Errorf("failed to get updates")
	}

	// Update offset
	for _, update := range result.Result {
		if update.UpdateID >= a.offset {
			a.offset = update.UpdateID + 1
		}
	}

	return result.Result, nil
}

// processUpdate processes an update
func (a *TelegramAdapter) processUpdate(update TelegramUpdate) {
	if update.Message == nil {
		return
	}

	msg := &Message{
		ID:        fmt.Sprintf("%d", update.Message.MessageID),
		SessionID: fmt.Sprintf("telegram:%d", update.Message.Chat.ID),
		Role:      "user",
		Content:   update.Message.Text,
		Platform:  "telegram",
		Timestamp: time.Unix(update.Message.Date, 0),
	}

	if a.handler != nil {
		resp, err := a.handler(msg)
		if err != nil {
			log.Printf("Failed to handle message: %v", err)
			return
		}

		if resp != nil {
			resp.SessionID = msg.SessionID
			if err := a.SendMessage(resp); err != nil {
				log.Printf("Failed to send response: %v", err)
			}
		}
	}
}

// sendMessage sends a message to Telegram
func (a *TelegramAdapter) sendMessage(req TelegramSendMessageRequest) error {
	data, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/sendMessage", a.baseURL)
	resp, err := a.client.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to send message (status %d): %s", resp.StatusCode, string(body))
	}

	return nil
}
