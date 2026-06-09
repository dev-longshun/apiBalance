package bot

import (
	"context"
	"fmt"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"upstream-balance/internal/checker"
	"upstream-balance/internal/store"
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

func (b *Bot) Start(ctx context.Context) {
	ctx, b.cancel = context.WithCancel(ctx)

	commands := tgbotapi.NewSetMyCommands(
		tgbotapi.BotCommand{Command: "start", Description: "打开主菜单"},
		tgbotapi.BotCommand{Command: "balance", Description: "查询所有站点余额"},
		tgbotapi.BotCommand{Command: "refresh", Description: "刷新所有站点余额"},
		tgbotapi.BotCommand{Command: "status", Description: "系统运行状态"},
		tgbotapi.BotCommand{Command: "help", Description: "帮助信息"},
	)
	b.api.Request(commands)

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
				if update.CallbackQuery != nil && update.CallbackQuery.Message != nil {
					if update.CallbackQuery.Message.Chat.ID == b.chatID {
						b.handleCallback(update.CallbackQuery)
					}
					continue
				}
				if update.Message == nil {
					continue
				}
				if update.Message.Chat.ID != b.chatID {
					continue
				}
				b.handleMessage(update.Message)
			}
		}
	}()
}

func (b *Bot) Stop() {
	if b.cancel != nil {
		b.cancel()
	}
	b.api.StopReceivingUpdates()
}

func mainMenuKeyboard() tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📊 查询余额", "balance"),
			tgbotapi.NewInlineKeyboardButtonData("🔄 刷新余额", "refresh"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📈 系统状态", "status"),
			tgbotapi.NewInlineKeyboardButtonData("❓ 帮助", "help"),
		),
	)
}

func backMenuKeyboard() tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔄 刷新余额", "refresh"),
			tgbotapi.NewInlineKeyboardButtonData("🔙 主菜单", "start"),
		),
	)
}

// handleCallback handles inline keyboard button presses.
func (b *Bot) handleCallback(cq *tgbotapi.CallbackQuery) {
	callback := tgbotapi.NewCallback(cq.ID, "")
	b.api.Request(callback)

	chatID := cq.Message.Chat.ID
	switch cq.Data {
	case "start":
		b.sendStart(chatID)
	case "balance":
		b.doBalance(chatID)
	case "refresh":
		b.doRefresh(chatID)
	case "status":
		b.doStatus(chatID)
	case "help":
		b.doHelp(chatID)
	}
}

func (b *Bot) handleMessage(msg *tgbotapi.Message) {
	if !msg.IsCommand() {
		return
	}
	chatID := msg.Chat.ID
	switch msg.Command() {
	case "start":
		b.sendStart(chatID)
	case "balance":
		b.doBalance(chatID)
	case "refresh":
		b.doRefresh(chatID)
	case "status":
		b.doStatus(chatID)
	case "help":
		b.doHelp(chatID)
	}
}

func (b *Bot) sendStart(chatID int64) {
	text := "👋 欢迎使用上游渠道额度监控\n\n👇 请选择操作："
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ReplyMarkup = mainMenuKeyboard()
	b.api.Send(msg)
}

func (b *Bot) doBalance(chatID int64) {
	b.sendText(chatID, "⏳ 正在查询所有站点余额...")

	allSites, err := b.sites.List()
	if err != nil {
		b.sendText(chatID, fmt.Sprintf("❌ 获取站点列表失败: %v", err))
		return
	}
	if len(allSites) == 0 {
		b.sendText(chatID, "📭 当前没有配置任何站点")
		return
	}

	results := b.checker.CheckWithConcurrency(allSites, 5)

	var lines []string
	lines = append(lines, "📊 额度总览\n")
	okCount, warnCount, errCount := 0, 0, 0

	for _, site := range allSites {
		cr := results[site.ID]
		if cr == nil || cr.Error != "" {
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
			lines = append(lines, fmt.Sprintf("🟡 %-16s $%.2f  ⚠️", site.Name, cr.Balance))
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

	msg := tgbotapi.NewMessage(chatID, strings.Join(lines, "\n"))
	msg.ReplyMarkup = backMenuKeyboard()
	b.api.Send(msg)
}

func (b *Bot) doRefresh(chatID int64) {
	b.sendText(chatID, "⏳ 正在刷新所有站点余额...")

	allSites, err := b.sites.List()
	if err != nil {
		b.sendText(chatID, fmt.Sprintf("❌ 获取站点列表失败: %v", err))
		return
	}
	if len(allSites) == 0 {
		b.sendText(chatID, "📭 当前没有配置任何站点")
		return
	}

	results := b.checker.CheckWithConcurrency(allSites, 5)

	okCount, errCount := 0, 0
	for _, site := range allSites {
		cr := results[site.ID]
		if cr == nil || cr.Error != "" {
			errCount++
			errMsg := "check failed"
			if cr != nil {
				errMsg = cr.Error
			}
			_ = b.sites.UpdateBalance(site.ID, site.Balance, site.BalanceUnit, site.DetectedType, "error", errMsg)
			continue
		}
		okCount++
		_ = b.sites.UpdateBalance(site.ID, cr.Balance, cr.Unit, cr.DetectedType, "ok", "")
	}

	now := time.Now().Format("2006-01-02 15:04:05")
	text := fmt.Sprintf("✅ 刷新完成\n\n成功: %d\n失败: %d\n时间: %s", okCount, errCount, now)
	reply := tgbotapi.NewMessage(chatID, text)
	reply.ReplyMarkup = backMenuKeyboard()
	b.api.Send(reply)
}

func (b *Bot) doStatus(chatID int64) {
	uptime := time.Since(b.startTime)
	days := int(uptime.Hours()) / 24
	hours := int(uptime.Hours()) % 24
	minutes := int(uptime.Minutes()) % 60

	allSites, err := b.sites.List()
	if err != nil {
		b.sendText(chatID, fmt.Sprintf("❌ 获取状态失败: %v", err))
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
	lines = append(lines, fmt.Sprintf("运行时间: %d天 %d小时 %d分", days, hours, minutes))
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

	reply := tgbotapi.NewMessage(chatID, strings.Join(lines, "\n"))
	reply.ReplyMarkup = backMenuKeyboard()
	b.api.Send(reply)
}

func (b *Bot) doHelp(chatID int64) {
	text := `🤖 UpstreamBalance Bot

可用命令:
/balance - 查询所有站点余额
/balance <名称> - 查询指定站点余额
/refresh - 刷新所有站点余额
/status - 查看系统运行状态
/help - 显示此帮助信息`

	reply := tgbotapi.NewMessage(chatID, text)
	reply.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔙 主菜单", "start"),
		),
	)
	b.api.Send(reply)
}

func (b *Bot) sendText(chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	b.api.Send(msg)
}
