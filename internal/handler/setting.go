package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"quota-sentinel/internal/model"
	"quota-sentinel/internal/notifier"
	"quota-sentinel/internal/store"
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

	resp := model.SettingsResponse{
		IntervalMinutes:  intervalMinutes,
		TelegramBotToken: botToken,
		TelegramChatID:   chatID,
	}

	c.JSON(http.StatusOK, resp)
}

func (h *SettingHandler) UpdateSettings(c *gin.Context) {
	var req model.SettingsUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
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

	// Return updated settings.
	h.GetSettings(c)
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
		Version:            "1.0.0",
	}

	c.JSON(http.StatusOK, resp)
}
