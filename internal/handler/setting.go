package handler

import (
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"upstream-balance/internal/model"
	"upstream-balance/internal/notifier"
	"upstream-balance/internal/store"
)

type SettingHandler struct {
	settings  *store.SettingStore
	startTime time.Time
	sites     *store.SiteStore
}

func NewSettingHandler(settings *store.SettingStore, sites *store.SiteStore) *SettingHandler {
	return &SettingHandler{
		settings:  settings,
		startTime: time.Now(),
		sites:     sites,
	}
}

func (h *SettingHandler) GetSettings(c *gin.Context) {
	all, err := h.settings.GetAll()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	intervalMinutes := 30
	if v, ok := all["interval_minutes"]; ok && v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			intervalMinutes = n
		}
	}

	botToken := ""
	if v, ok := all["telegram_bot_token"]; ok && v != "" {
		botToken = "***configured***"
	}

	chatID := ""
	if v, ok := all["telegram_chat_id"]; ok {
		chatID = v
	}

	emailCfg := emailConfigFrom(func(k string) string { return all[k] })
	smtpPassword := ""
	if emailCfg.Password != "" {
		smtpPassword = "***configured***"
	}

	resp := model.SettingsResponse{
		IntervalMinutes:  intervalMinutes,
		TelegramBotToken: botToken,
		TelegramChatID:   chatID,
		EmailEnabled:     all["email_enabled"] == "true",
		SMTPHost:         emailCfg.Host,
		SMTPPort:         emailCfg.Port,
		SMTPSecurity:     emailCfg.Security,
		SMTPUser:         emailCfg.User,
		SMTPPassword:     smtpPassword,
		SMTPFromName:     emailCfg.FromName,
		EmailTo:          emailCfg.To,
	}

	c.JSON(http.StatusOK, resp)
}

func (h *SettingHandler) UpdateSettings(c *gin.Context) {
	var req model.SettingsUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Validate email fields up front so a bad value doesn't leave a half-saved form.
	emailUpdates, err := h.validateEmailUpdate(&req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.IntervalMinutes != nil {
		if *req.IntervalMinutes < 5 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "interval_minutes must be >= 5"})
			return
		}
		if err := h.settings.Set("interval_minutes", strconv.Itoa(*req.IntervalMinutes)); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}

	if req.TelegramBotToken != nil {
		if err := h.settings.Set("telegram_bot_token", *req.TelegramBotToken); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}

	if req.TelegramChatID != nil {
		if err := h.settings.Set("telegram_chat_id", *req.TelegramChatID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}

	for k, v := range emailUpdates {
		if err := h.settings.Set(k, v); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}

	// Return updated settings.
	h.GetSettings(c)
}

func isBareAddress(s string) bool {
	a, err := mail.ParseAddress(s)
	return err == nil && a.Address == s
}

// validateEmailUpdate normalizes the email fields of req into settings key/values.
func (h *SettingHandler) validateEmailUpdate(req *model.SettingsUpdateRequest) (map[string]string, error) {
	updates := map[string]string{}

	if req.SMTPHost != nil {
		updates["smtp_host"] = strings.TrimSpace(*req.SMTPHost)
	}
	if req.SMTPPort != nil {
		if *req.SMTPPort < 1 || *req.SMTPPort > 65535 {
			return nil, errors.New("SMTP 端口必须在 1-65535 之间")
		}
		updates["smtp_port"] = strconv.Itoa(*req.SMTPPort)
	}
	if req.SMTPSecurity != nil {
		sec := strings.ToLower(strings.TrimSpace(*req.SMTPSecurity))
		if sec != "ssl" && sec != "starttls" {
			return nil, errors.New("加密方式只能是 ssl 或 starttls")
		}
		updates["smtp_security"] = sec
	}
	if req.SMTPUser != nil {
		user := strings.TrimSpace(*req.SMTPUser)
		if user != "" && !isBareAddress(user) {
			return nil, fmt.Errorf("发件邮箱格式不对：%s", user)
		}
		updates["smtp_user"] = user
	}
	if req.SMTPPassword != nil {
		updates["smtp_password"] = strings.TrimSpace(*req.SMTPPassword)
	}
	if req.SMTPFromName != nil {
		updates["smtp_from_name"] = strings.TrimSpace(*req.SMTPFromName)
	}
	if req.EmailTo != nil {
		rcpts := notifier.ParseRecipients(*req.EmailTo)
		for _, r := range rcpts {
			if !isBareAddress(r) {
				return nil, fmt.Errorf("收件邮箱格式不对：%s", r)
			}
		}
		updates["email_to"] = strings.Join(rcpts, ",")
	}
	if req.EmailEnabled != nil {
		updates["email_enabled"] = strconv.FormatBool(*req.EmailEnabled)
		if *req.EmailEnabled {
			merged := emailConfigFrom(func(k string) string {
				if v, ok := updates[k]; ok {
					return v
				}
				v, _ := h.settings.Get(k)
				return v
			})
			if !notifier.NewEmail(merged).IsConfigured() {
				return nil, errors.New("启用邮件预警前，请填完 SMTP 服务器、发件邮箱、授权码和收件邮箱")
			}
		}
	}
	return updates, nil
}

func emailConfigFrom(get func(string) string) notifier.EmailConfig {
	port, _ := strconv.Atoi(get("smtp_port"))
	if port <= 0 {
		port = 465
	}
	security := get("smtp_security")
	if security == "" {
		security = "ssl"
	}
	return notifier.EmailConfig{
		Host:     get("smtp_host"),
		Port:     port,
		Security: security,
		User:     get("smtp_user"),
		Password: get("smtp_password"),
		FromName: get("smtp_from_name"),
		To:       get("email_to"),
	}
}

// EmailConfigFromSettings reads the SMTP setup from the settings table (ignores email_enabled).
func EmailConfigFromSettings(settings *store.SettingStore) notifier.EmailConfig {
	return emailConfigFrom(func(k string) string {
		v, _ := settings.Get(k)
		return v
	})
}

// EmailEnabled reports whether email alerts are switched on.
func EmailEnabled(settings *store.SettingStore) bool {
	v, _ := settings.Get("email_enabled")
	return v == "true"
}

func (h *SettingHandler) TestTelegram(c *gin.Context) {
	botToken, err := h.settings.Get("telegram_bot_token")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	chatID, err := h.settings.Get("telegram_chat_id")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	tg := notifier.New(botToken, chatID)
	if !tg.IsConfigured() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "telegram bot_token and chat_id must be configured"})
		return
	}

	if err := tg.SendTestMessage(); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "test message sent"})
}

func (h *SettingHandler) TestEmail(c *gin.Context) {
	mailer := notifier.NewEmail(EmailConfigFromSettings(h.settings))
	if !mailer.IsConfigured() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请先填写并保存 SMTP 服务器、发件邮箱、授权码和收件邮箱"})
		return
	}

	if err := mailer.SendTestMessage(); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "test email sent"})
}

func (h *SettingHandler) GetStatus(c *gin.Context) {
	sites, err := h.sites.List()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var sitesOK, sitesLow, sitesError int
	for _, site := range sites {
		switch site.Status {
		case "ok":
			sitesOK++
		case "low":
			sitesLow++
		case "error":
			sitesError++
		}
	}

	intervalStr, _ := h.settings.Get("interval_minutes")
	intervalMinutes := 30
	if intervalStr != "" {
		if n, err := strconv.Atoi(intervalStr); err == nil && n >= 5 {
			intervalMinutes = n
		}
	}

	botToken, _ := h.settings.Get("telegram_bot_token")
	chatID, _ := h.settings.Get("telegram_chat_id")

	now := time.Now().UTC()
	lastPoll := ""
	nextPoll := now.Add(time.Duration(intervalMinutes) * time.Minute).Format(time.RFC3339)

	// Find the most recent last_check_at across all sites as a proxy for last poll.
	for _, site := range sites {
		if site.LastCheckAt != "" && site.LastCheckAt > lastPoll {
			lastPoll = site.LastCheckAt
		}
	}

	resp := model.StatusResponse{
		UptimeSeconds:      int64(time.Since(h.startTime).Seconds()),
		SiteCount:          len(sites),
		SitesOK:            sitesOK,
		SitesLow:           sitesLow,
		SitesError:         sitesError,
		LastPollAt:         lastPoll,
		NextPollAt:         nextPoll,
		TelegramConfigured: botToken != "" && chatID != "",
		EmailConfigured:    EmailEnabled(h.settings) && notifier.NewEmail(EmailConfigFromSettings(h.settings)).IsConfigured(),
		Version:            "1.0.0",
	}

	c.JSON(http.StatusOK, resp)
}
