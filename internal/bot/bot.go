package bot

import (
	"context"
	"fmt"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"quota-sentinel/internal/checker"
	"quota-sentinel/internal/store"
)

type Bot struct {
	api        *tgbotapi.BotAPI
	chatID     int64
	checker    *checker.Checker
	sites      *store.SiteStore
	thresholds *store.ThresholdStore
	settings   *store.SettingStore
	startTime  time.Time
	cancel     context.CancelFunc

	// LastPollAt and NextPollAt are set by the scheduler.
	LastPollAt string
	NextPollAt string
}

func New(token string, chatID int64, chk *checker.Checker, sites *store.SiteStore, thresholds *store.ThresholdStore, settings *store.SettingStore) (*Bot, error) {
	api, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, fmt.Errorf("create bot api: %w", err)
	}

	return &Bot{
		api:        api,
		chatID:     chatID,
		checker:    chk,
		sites:      sites,
		thresholds: thresholds,
		settings:   settings,
		startTime:  time.Now(),
	}, nil
}

// Start begins listening for Telegram updates in a goroutine.
func (b *Bot) Start(ctx context.Context) {
	ctx, b.cancel = context.WithCancel(ctx)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 30

	updates := b.api.GetUpdatesChan(u)

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case update, ok := <-updates:
				if !ok {
					return
				}
				if update.Message == nil {
					continue
				}
				// Only respond to the configured chat.
				if update.Message.Chat.ID != b.chatID {
					continue
				}
				b.handleMessage(update.Message)
			}
		}
	}()
}

// Stop stops the bot from listening.
func (b *Bot) Stop() {
	if b.cancel != nil {
		b.cancel()
	}
	b.api.StopReceivingUpdates()
}

func (b *Bot) handleMessage(msg *tgbotapi.Message) {
	if !msg.IsCommand() {
		return
	}

	switch msg.Command() {
	case "balance":
		b.handleBalance(msg)
	case "refresh":
		b.handleRefresh(msg)
	case "status":
		b.handleStatus(msg)
	case "help":
		b.handleHelp(msg)
	}
}

func (b *Bot) handleBalance(msg *tgbotapi.Message) {
	arg := strings.TrimSpace(msg.CommandArguments())

	if arg != "" {
		b.handleBalanceSingle(msg, arg)
		return
	}

	b.sendText(msg.Chat.ID, "⏳ 正在查询所有站点余额...")

	allSites, err := b.sites.List()
	if err != nil {
		b.sendText(msg.Chat.ID, fmt.Sprintf("❌ 获取站点列表失败: %v", err))
		return
	}
	if len(allSites) == 0 {
		b.sendText(msg.Chat.ID, "📭 当前没有配置任何站点")
		return
	}

	results := b.checker.CheckWithConcurrency(allSites, 5)

	var lines []string
	lines = append(lines, "📊 额度总览\n")

	okCount, warnCount, errCount := 0, 0, 0

	for _, site := range allSites {
		cr := results[site.ID]
		if cr == nil {
			lines = append(lines, fmt.Sprintf("🔴 %-16s 查询失败", site.Name))
			errCount++
			continue
		}
		if cr.Error != "" {
			lines = append(lines, fmt.Sprintf("🔴 %-16s 查询失败", site.Name))
			errCount++
			continue
		}

		belowThreshold := false
		thresholds, _ := b.thresholds.GetAmountsBySite(site.ID)
		for _, th := range thresholds {
			if cr.Balance < th {
				belowThreshold = true
				break
			}
		}

		if belowThreshold {
			lines = append(lines, fmt.Sprintf("🟡 %-16s $%.2f  ⚠️ 低于阈值", site.Name, cr.Balance))
			warnCount++
		} else {
			lines = append(lines, fmt.Sprintf("🟢 %-16s $%.2f", site.Name, cr.Balance))
			okCount++
		}
	}

	now := time.Now().Format("2006-01-02 15:04:05")
	lines = append(lines, "")
	lines = append(lines, fmt.Sprintf("总计: %d 正常 / %d 告警 / %d 异常", okCount, warnCount, errCount))
	lines = append(lines, fmt.Sprintf("查询时间: %s", now))

	b.sendText(msg.Chat.ID, strings.Join(lines, "\n"))
}

func (b *Bot) handleBalanceSingle(msg *tgbotapi.Message, query string) {
	allSites, err := b.sites.List()
	if err != nil {
		b.sendText(msg.Chat.ID, fmt.Sprintf("❌ 获取站点列表失败: %v", err))
		return
	}

	// Fuzzy match: case-insensitive contains.
	var matched []int
	lowerQuery := strings.ToLower(query)
	for i, s := range allSites {
		if strings.Contains(strings.ToLower(s.Name), lowerQuery) {
			matched = append(matched, i)
		}
	}

	if len(matched) == 0 {
		b.sendText(msg.Chat.ID, fmt.Sprintf("未找到匹配「%s」的站点", query))
		return
	}

	if len(matched) > 1 {
		var names []string
		for _, idx := range matched {
			names = append(names, allSites[idx].Name)
		}
		b.sendText(msg.Chat.ID, fmt.Sprintf("匹配到多个站点，请精确指定:\n%s", strings.Join(names, "\n")))
		return
	}

	site := allSites[matched[0]]
	b.sendText(msg.Chat.ID, fmt.Sprintf("⏳ 正在查询站点「%s」余额...", site.Name))

	cr := b.checker.Check(&site)

	if cr.Error != "" {
		b.sendText(msg.Chat.ID, fmt.Sprintf("🔴 站点「%s」查询失败: %s", site.Name, cr.Error))
		return
	}

	thresholds, _ := b.thresholds.GetAmountsBySite(site.ID)
	belowThreshold := false
	var triggeredTh float64
	for _, th := range thresholds {
		if cr.Balance < th {
			belowThreshold = true
			triggeredTh = th
			break
		}
	}

	var lines []string
	lines = append(lines, fmt.Sprintf("📊 站点详情: %s\n", site.Name))
	lines = append(lines, fmt.Sprintf("余额: $%.2f", cr.Balance))
	lines = append(lines, fmt.Sprintf("单位: %s", cr.Unit))
	lines = append(lines, fmt.Sprintf("类型: %s", cr.DetectedType))

	if belowThreshold {
		lines = append(lines, fmt.Sprintf("\n⚠️ 低于阈值 $%.2f", triggeredTh))
	} else {
		lines = append(lines, "\n✅ 余额正常")
	}

	now := time.Now().Format("2006-01-02 15:04:05")
	lines = append(lines, fmt.Sprintf("查询时间: %s", now))

	b.sendText(msg.Chat.ID, strings.Join(lines, "\n"))
}

func (b *Bot) handleRefresh(msg *tgbotapi.Message) {
	b.sendText(msg.Chat.ID, "⏳ 正在刷新所有站点余额...")

	allSites, err := b.sites.List()
	if err != nil {
		b.sendText(msg.Chat.ID, fmt.Sprintf("❌ 获取站点列表失败: %v", err))
		return
	}
	if len(allSites) == 0 {
		b.sendText(msg.Chat.ID, "📭 当前没有配置任何站点")
		return
	}

	results := b.checker.CheckWithConcurrency(allSites, 5)

	okCount, errCount := 0, 0
	for _, site := range allSites {
		cr := results[site.ID]
		if cr == nil || cr.Error != "" {
			errCount++
			status := "error"
			errMsg := "check failed"
			if cr != nil {
				errMsg = cr.Error
			}
			_ = b.sites.UpdateBalance(site.ID, site.Balance, site.BalanceUnit, site.DetectedType, status, errMsg)
			continue
		}
		okCount++
		_ = b.sites.UpdateBalance(site.ID, cr.Balance, cr.Unit, cr.DetectedType, "ok", "")
	}

	now := time.Now().Format("2006-01-02 15:04:05")
	b.sendText(msg.Chat.ID, fmt.Sprintf("✅ 刷新完成\n\n成功: %d\n失败: %d\n时间: %s", okCount, errCount, now))
}

func (b *Bot) handleStatus(msg *tgbotapi.Message) {
	uptime := time.Since(b.startTime)
	days := int(uptime.Hours()) / 24
	hours := int(uptime.Hours()) % 24
	minutes := int(uptime.Minutes()) % 60
	uptimeStr := fmt.Sprintf("%d天 %d小时 %d分", days, hours, minutes)

	allSites, err := b.sites.List()
	if err != nil {
		b.sendText(msg.Chat.ID, fmt.Sprintf("❌ 获取状态失败: %v", err))
		return
	}

	okCount, warnCount, errCount := 0, 0, 0
	for _, site := range allSites {
		switch site.Status {
		case "ok":
			okCount++
		case "low":
			warnCount++
		default:
			errCount++
		}
	}

	var lines []string
	lines = append(lines, "📈 系统状态\n")
	lines = append(lines, fmt.Sprintf("运行时间: %s", uptimeStr))
	lines = append(lines, fmt.Sprintf("站点总数: %d", len(allSites)))
	lines = append(lines, fmt.Sprintf("  🟢 正常: %d", okCount))
	lines = append(lines, fmt.Sprintf("  🟡 告警: %d", warnCount))
	lines = append(lines, fmt.Sprintf("  🔴 异常: %d", errCount))

	if b.LastPollAt != "" {
		lines = append(lines, fmt.Sprintf("\n上次轮询: %s", b.LastPollAt))
	}
	if b.NextPollAt != "" {
		lines = append(lines, fmt.Sprintf("下次轮询: %s", b.NextPollAt))
	}

	b.sendText(msg.Chat.ID, strings.Join(lines, "\n"))
}

func (b *Bot) handleHelp(msg *tgbotapi.Message) {
	text := `🤖 Quota Sentinel Bot

可用命令:
/balance - 查询所有站点余额
/balance <名称> - 查询指定站点余额
/refresh - 刷新所有站点余额
/status - 查看系统运行状态
/help - 显示此帮助信息`

	b.sendText(msg.Chat.ID, text)
}

func (b *Bot) sendText(chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	_, _ = b.api.Send(msg)
}
