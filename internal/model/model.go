package model

import "time"

type Site struct {
	ID           string      `json:"id"`
	Name         string      `json:"name"`
	BaseURL      string      `json:"base_url"`
	APIKey       string      `json:"api_key,omitempty"`
	APIKeyMasked string      `json:"api_key_masked,omitempty"`
	AuthType     string      `json:"auth_type"`
	Balance      float64     `json:"balance"`
	BalanceUnit  string      `json:"balance_unit"`
	DetectedType string      `json:"detected_type"`
	LastCheckAt  string      `json:"last_check_at"`
	LastError    string      `json:"last_error"`
	Status       string      `json:"status"`
	Thresholds   []float64   `json:"thresholds"`
	CreatedAt    string      `json:"created_at"`
	UpdatedAt    string      `json:"updated_at"`
}

func (s *Site) MaskKey() {
	if len(s.APIKey) > 6 {
		s.APIKeyMasked = s.APIKey[:6] + "***"
	} else if s.APIKey != "" {
		s.APIKeyMasked = "***"
	}
	s.APIKey = ""
}

type Threshold struct {
	ID        int64   `json:"id"`
	SiteID    string  `json:"site_id"`
	Amount    float64 `json:"amount"`
	Triggered bool    `json:"triggered"`
}

type CheckResult struct {
	Balance      float64           `json:"balance"`
	Unit         string            `json:"unit"`
	DetectedType string            `json:"detected_type"`
	Extra        map[string]float64 `json:"extra,omitempty"`
	Error        string            `json:"error,omitempty"`
}

type AlertSent struct {
	Threshold   float64 `json:"threshold"`
	MessageSent bool    `json:"message_sent"`
}

type SiteCreateRequest struct {
	Name       string    `json:"name" binding:"required"`
	BaseURL    string    `json:"base_url" binding:"required"`
	APIKey     string    `json:"api_key" binding:"required"`
	AuthType   string    `json:"auth_type"`
	Thresholds []float64 `json:"thresholds"`
}

type SiteUpdateRequest struct {
	Name       *string    `json:"name"`
	BaseURL    *string    `json:"base_url"`
	APIKey     *string    `json:"api_key"`
	AuthType   *string    `json:"auth_type"`
	Thresholds *[]float64 `json:"thresholds"`
}

type SettingsResponse struct {
	IntervalMinutes  int    `json:"interval_minutes"`
	TelegramBotToken string `json:"telegram_bot_token"`
	TelegramChatID   string `json:"telegram_chat_id"`
}

type SettingsUpdateRequest struct {
	IntervalMinutes  *int    `json:"interval_minutes"`
	TelegramBotToken *string `json:"telegram_bot_token"`
	TelegramChatID   *string `json:"telegram_chat_id"`
}

type StatusResponse struct {
	UptimeSeconds      int64  `json:"uptime_seconds"`
	SiteCount          int    `json:"site_count"`
	SitesOK            int    `json:"sites_ok"`
	SitesLow           int    `json:"sites_low"`
	SitesError         int    `json:"sites_error"`
	LastPollAt         string `json:"last_poll_at"`
	NextPollAt         string `json:"next_poll_at"`
	TelegramConfigured bool   `json:"telegram_configured"`
	Version            string `json:"version"`
}

func Now() string {
	return time.Now().UTC().Format(time.RFC3339)
}
