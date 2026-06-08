package notifier

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type Telegram struct {
	botToken string
	chatID   string
}

func New(botToken, chatID string) *Telegram {
	return &Telegram{
		botToken: botToken,
		chatID:   chatID,
	}
}

func (t *Telegram) IsConfigured() bool {
	return t.botToken != "" && t.chatID != ""
}

// Send sends a Markdown message via the Telegram Bot API. It retries once on failure.
func (t *Telegram) Send(text string) error {
	err := t.send(text)
	if err != nil {
		// Retry once.
		time.Sleep(2 * time.Second)
		err = t.send(text)
	}
	return err
}

func (t *Telegram) send(text string) error {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", t.botToken)

	payload := map[string]string{
		"chat_id":    t.chatID,
		"text":       text,
		"parse_mode": "Markdown",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("telegram request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("telegram API returned status %d", resp.StatusCode)
	}

	return nil
}

func (t *Telegram) SendAlert(siteName string, balance float64, threshold float64, checkTime string) error {
	text := fmt.Sprintf(
		"⚠️ 余额不足提醒\n\n站点: %s\n当前余额: $%.2f\n触发阈值: $%.2f\n查询时间: %s\n\n请及时充值，避免服务中断。",
		siteName, balance, threshold, checkTime,
	)
	return t.Send(text)
}

func (t *Telegram) SendTestMessage() error {
	return t.Send("✅ Quota Sentinel 测试消息发送成功！")
}
