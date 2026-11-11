package model

import "time"

type Site struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	BaseURL      string `json:"base_url"`
	PortalURL    string `json:"portal_url"` // 充值/控制台链接；空则 Bot 回退用 base_url
	APIKey       string `json:"api_key,omitempty"`
	APIKeyMasked string `json:"api_key_masked,omitempty"`
	// AccessToken 是 NewAPI「个人设置 → 系统访问令牌」，配合 UserID 直接查 /api/user/self，
	// 不需要保存面板密码，也不会被登录验证码 / 2FA 挡住。
	AccessToken       string    `json:"access_token,omitempty"`
	AccessTokenMasked string    `json:"access_token_masked,omitempty"`
	Username          string    `json:"username,omitempty"`
	Password          string    `json:"password,omitempty"`
	UserID            int       `json:"user_id"`
	AuthType          string    `json:"auth_type"`
	Balance           float64   `json:"balance"`
	BalanceUnit       string    `json:"balance_unit"`
	DetectedType      string    `json:"detected_type"`
	LastCheckAt       string    `json:"last_check_at"`
	LastError         string    `json:"last_error"`
	Status            string    `json:"status"`
	Thresholds        []float64 `json:"thresholds"`
	CreatedAt         string    `json:"created_at"`
	UpdatedAt         string    `json:"updated_at"`
}

// LinkURL returns the URL used for "open / top-up" buttons.
// Prefer portal_url; fall back to base_url so existing sites work without re-edit.
func (s *Site) LinkURL() string {
	if s.PortalURL != "" {
		return s.PortalURL
	}
	return s.BaseURL
}

func (s *Site) MaskSecrets() {
	if len(s.APIKey) > 6 {
		s.APIKeyMasked = s.APIKey[:6] + "***"
	} else if s.APIKey != "" {
		s.APIKeyMasked = "***"
	}
	if len(s.AccessToken) > 6 {
		s.AccessTokenMasked = s.AccessToken[:6] + "***"
	} else if s.AccessToken != "" {
		s.AccessTokenMasked = "***"
	}
	s.APIKey = ""
	s.AccessToken = ""
	s.Password = ""
}

type Threshold struct {
	ID        int64   `json:"id"`
	SiteID    string  `json:"site_id"`
	Amount    float64 `json:"amount"`
	Triggered bool    `json:"triggered"`
}

type CheckResult struct {
	Balance      float64            `json:"balance"`
	Unit         string             `json:"unit"`
	DetectedType string             `json:"detected_type"`
	Extra        map[string]float64 `json:"extra,omitempty"`
	// Daily 是上游自报的按天消耗（sub2api 的 daily_usage），用来算日均；上游不给就为空。
	Daily []DailyCost `json:"daily,omitempty"`
	Error string      `json:"error,omitempty"`
}

// DailyCost 是某一天（上游口径的日期 YYYY-MM-DD）实际扣掉的余额。
type DailyCost struct {
	Date string  `json:"date"`
	Cost float64 `json:"cost"`
}

type AlertSent struct {
	Threshold   float64 `json:"threshold"`
	MessageSent bool    `json:"message_sent"`
}

type SiteCreateRequest struct {
	Name        string    `json:"name" binding:"required"`
	BaseURL     string    `json:"base_url" binding:"required"`
	PortalURL   string    `json:"portal_url"`
	APIKey      string    `json:"api_key"`
	AccessToken string    `json:"access_token"`
	Username    string    `json:"username"`
	Password    string    `json:"password"`
	UserID      int       `json:"user_id"`
	AuthType    string    `json:"auth_type"`
	Thresholds  []float64 `json:"thresholds"`
}

type SiteUpdateRequest struct {
	Name        *string    `json:"name"`
	BaseURL     *string    `json:"base_url"`
	PortalURL   *string    `json:"portal_url"`
	APIKey      *string    `json:"api_key"`
	AccessToken *string    `json:"access_token"`
	Username    *string    `json:"username"`
	Password    *string    `json:"password"`
	UserID      *int       `json:"user_id"`
	AuthType    *string    `json:"auth_type"`
	Thresholds  *[]float64 `json:"thresholds"`
}

type SettingsResponse struct {
	IntervalMinutes  int    `json:"interval_minutes"`
	TelegramBotToken string `json:"telegram_bot_token"`
	TelegramChatID   string `json:"telegram_chat_id"`
	EmailEnabled     bool   `json:"email_enabled"`
	SMTPHost         string `json:"smtp_host"`
	SMTPPort         int    `json:"smtp_port"`
	SMTPSecurity     string `json:"smtp_security"`
	SMTPUser         string `json:"smtp_user"`
	SMTPPassword     string `json:"smtp_password"` // "***configured***" when set, never the real value
	SMTPFromName     string `json:"smtp_from_name"`
	EmailTo          string `json:"email_to"`
}

type SettingsUpdateRequest struct {
	IntervalMinutes  *int    `json:"interval_minutes"`
	TelegramBotToken *string `json:"telegram_bot_token"`
	TelegramChatID   *string `json:"telegram_chat_id"`
	EmailEnabled     *bool   `json:"email_enabled"`
	SMTPHost         *string `json:"smtp_host"`
	SMTPPort         *int    `json:"smtp_port"`
	SMTPSecurity     *string `json:"smtp_security"`
	SMTPUser         *string `json:"smtp_user"`
	SMTPPassword     *string `json:"smtp_password"`
	SMTPFromName     *string `json:"smtp_from_name"`
	EmailTo          *string `json:"email_to"`
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
	EmailConfigured    bool   `json:"email_configured"`
	Version            string `json:"version"`
}

func Now() string {
	return time.Now().UTC().Format(time.RFC3339)
}
