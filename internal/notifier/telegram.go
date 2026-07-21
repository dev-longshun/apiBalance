package notifier

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Telegram struct {
	botToken string
	chatIDs  []string
}

func New(botToken, chatIDs string) *Telegram {
	var ids []string
	for _, id := range strings.Split(chatIDs, ",") {
		id = strings.TrimSpace(id)
		if id != "" {
			ids = append(ids, id)
		}
	}
	return &Telegram{
		botToken: botToken,
		chatIDs:  ids,
	}
}

func (t *Telegram) IsConfigured() bool {
	return t.botToken != "" && len(t.chatIDs) > 0
}

// Send sends a Markdown message to all configured chat IDs. It retries once per chat on failure.
func (t *Telegram) Send(text string) error {
	return t.SendWithKeyboard(text, nil)
}

// SendWithKeyboard sends a message with an optional inline keyboard (reply_markup).
func (t *Telegram) SendWithKeyboard(text string, replyMarkup map[string]interface{}) error {
	var lastErr error
	for _, chatID := range t.chatIDs {
		if err := t.sendTo(chatID, text, replyMarkup); err != nil {
			time.Sleep(2 * time.Second)
			if err = t.sendTo(chatID, text, replyMarkup); err != nil {
				lastErr = err
			}
		}
	}
	return lastErr
}

func (t *Telegram) sendTo(chatID, text string, replyMarkup map[string]interface{}) error {
	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", t.botToken)

	payload := map[string]interface{}{
		"chat_id":    chatID,
		"text":       text,
		"parse_mode": "Markdown",
	}
	if replyMarkup != nil {
		payload["reply_markup"] = replyMarkup
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	resp, err := http.Post(apiURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("telegram request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("telegram API returned status %d", resp.StatusCode)
	}

	return nil
}

func isHTTPURL(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	return u.Host != ""
}

// SendAlert notifies that a site is below threshold.
// linkURL is optional; when set, an inline "去充值" url button is attached.
func (t *Telegram) SendAlert(siteName string, balance float64, threshold float64, checkTime string, linkURL string) error {
	text := fmt.Sprintf(
		"⚠️ 余额不足提醒\n\n站点: %s\n当前余额: $%.2f\n触发阈值: $%.2f\n查询时间: %s\n\n请及时充值，避免服务中断。",
		siteName, balance, threshold, checkTime,
	)

	linkURL = strings.TrimSpace(linkURL)
	if isHTTPURL(linkURL) {
		kb := map[string]interface{}{
			"inline_keyboard": [][]map[string]string{
				{
					{"text": "💳 去充值 · " + siteName, "url": linkURL},
				},
			},
		}
		return t.SendWithKeyboard(text, kb)
	}
	return t.Send(text)
}

func (t *Telegram) SendTestMessage() error {
	return t.Send("✅ UpstreamBalance 测试消息发送成功！")
}
