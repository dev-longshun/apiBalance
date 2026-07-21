package bot

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"upstream-balance/internal/checker"
	"upstream-balance/internal/model"
	"upstream-balance/internal/store"
)

type Bot struct {
	api        *tgbotapi.BotAPI
	chatIDs    map[int64]bool
	checker    *checker.Checker
	sites      *store.SiteStore
	thresholds *store.ThresholdStore
	settings   *store.SettingStore
	startTime  time.Time
	cancel     context.CancelFunc

	LastPollAt string
	NextPollAt string
}

func New(token string, chatIDs []int64, chk *checker.Checker, sites *store.SiteStore, thresholds *store.ThresholdStore, settings *store.SettingStore) (*Bot, error) {
	api, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, fmt.Errorf("create bot api: %w", err)
	}

	idSet := make(map[int64]bool, len(chatIDs))
	for _, id := range chatIDs {
		idSet[id] = true
	}

	return &Bot{
		api:        api,
		chatIDs:    idSet,
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
		tgbotapi.BotCommand{Command: "topup", Description: "打开各站点充值/控制台"},
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
					if b.chatIDs[update.CallbackQuery.Message.Chat.ID] {
						b.handleCallback(update.CallbackQuery)
					}
					continue
				}
				if update.Message == nil {
					continue
				}
				if !b.chatIDs[update.Message.Chat.ID] {
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
			tgbotapi.NewInlineKeyboardButtonData("💳 去充值", "topup"),
			tgbotapi.NewInlineKeyboardButtonData("📈 系统状态", "status"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("❓ 帮助", "help"),
		),
	)
}

func backMenuKeyboard() tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("💳 去充值", "topup"),
			tgbotapi.NewInlineKeyboardButtonData("🔄 刷新余额", "refresh"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔙 主菜单", "start"),
		),
	)
}

// isHTTPURL accepts only absolute http(s) URLs for Telegram url buttons.
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

// siteLinkButtons builds rows of url buttons (max 2 per row) for sites that have a link.
func siteLinkButtons(sites []model.Site, withBack bool) tgbotapi.InlineKeyboardMarkup {
	var rows [][]tgbotapi.InlineKeyboardButton
	var row []tgbotapi.InlineKeyboardButton

	for _, site := range sites {
		link := strings.TrimSpace(site.LinkURL())
		if !isHTTPURL(link) {
			continue
		}
		// Telegram button text limit is 64 chars.
		label := "💳 " + site.Name
		if len([]rune(label)) > 64 {
			runes := []rune(site.Name)
			if len(runes) > 60 {
				runes = runes[:60]
			}
			label = "💳 " + string(runes)
		}
		row = append(row, tgbotapi.NewInlineKeyboardButtonURL(label, link))
		if len(row) == 2 {
			rows = append(rows, row)
			row = nil
		}
	}
	if len(row) > 0 {
		rows = append(rows, row)
	}
	if withBack {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔙 主菜单", "start"),
		))
	}
	if len(rows) == 0 {
		return tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("🔙 主菜单", "start"),
			),
		)
	}
	return tgbotapi.InlineKeyboardMarkup{InlineKeyboard: rows}
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
	case "topup":
		b.doTopup(chatID)
	case "refresh":
		b.doRefresh(chatID)
	case "status":
		b.doStatus(chatID)
	case "help":
		b.doHelp(chatID)
	}
}

func (b *Bot) handleMessage(msg *tgbotapi.Message) {
	if msg.IsCommand() {
		chatID := msg.Chat.ID
		switch msg.Command() {
		case "start":
			b.sendStart(chatID)
		case "balance":
			b.doBalance(chatID)
		case "topup":
			b.doTopup(chatID)
		case "refresh":
			b.doRefresh(chatID)
		case "status":
			b.doStatus(chatID)
		case "help":
			b.doHelp(chatID)
		}
		return
	}

	if b.isMentioned(msg) {
		b.sendStart(msg.Chat.ID)
	}
}

func (b *Bot) isMentioned(msg *tgbotapi.Message) bool {
	botName := "@" + b.api.Self.UserName
	for _, entity := range msg.Entities {
		if entity.Type == "mention" {
			mention := msg.Text[entity.Offset : entity.Offset+entity.Length]
			if strings.EqualFold(mention, botName) {
				return true
			}
		}
	}
	return false
}

func (b *Bot) sendStart(chatID int64) {
	text := "👋 欢迎使用上游渠道额度监控\n\n👇 请选择操作："
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ReplyMarkup = mainMenuKeyboard()
	b.api.Send(msg)
}

func (b *Bot) doTopup(chatID int64) {
	allSites, err := b.sites.List()
	if err != nil {
		b.sendText(chatID, fmt.Sprintf("❌ 获取站点列表失败: %v", err))
		return
	}
	if len(allSites) == 0 {
		b.sendText(chatID, "📭 当前没有配置任何站点")
		return
	}

	linked := 0
	for _, s := range allSites {
		if isHTTPURL(s.LinkURL()) {
			linked++
		}
	}
	if linked == 0 {
		text := "📭 暂无可用链接。\n请在 Web 面板为站点填写「接口地址」或「充值/控制台链接」。"
		msg := tgbotapi.NewMessage(chatID, text)
		msg.ReplyMarkup = backMenuKeyboard()
		b.api.Send(msg)
		return
	}

	text := "💳 点击下方按钮，直接打开对应站点充值/控制台：\n（未单独配置充值链接时，使用接口地址）"
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ReplyMarkup = siteLinkButtons(allSites, true)
	b.api.Send(msg)
}

func (b *Bot) doBalance(chatID int64) {
	// 查询与「刷新」同一套：实时拉余额 + 展示额度总览 + 充值按钮
	b.showBalanceOverview(chatID, "⏳ 正在查询所有站点余额...")
}

func (b *Bot) doRefresh(chatID int64) {
	// 用户期望：点「刷新余额」后仍看到完整额度总览，而不是一句「刷新完成」
	b.showBalanceOverview(chatID, "⏳ 正在刷新所有站点余额...")
}

// showBalanceOverview 实时查询所有站点，写回缓存，并发送带充值按钮的额度总览。
func (b *Bot) showBalanceOverview(chatID int64, progressText string) {
	b.sendText(chatID, progressText)

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
			errMsg := "check failed"
			if cr != nil && cr.Error != "" {
				errMsg = cr.Error
			}
			_ = b.sites.UpdateBalance(site.ID, site.Balance, site.BalanceUnit, site.DetectedType, "error", errMsg)
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

		status := "ok"
		if belowThreshold {
			status = "low"
			lines = append(lines, fmt.Sprintf("🟡 %-16s $%.2f  ⚠️", site.Name, cr.Balance))
			warnCount++
		} else {
			lines = append(lines, fmt.Sprintf("🟢 %-16s $%.2f", site.Name, cr.Balance))
			okCount++
		}
		_ = b.sites.UpdateBalance(site.ID, cr.Balance, cr.Unit, cr.DetectedType, status, "")
	}

	now := time.Now().Format("2006-01-02 15:04:05")
	lines = append(lines, "")
	lines = append(lines, fmt.Sprintf("总计: %d 正常 / %d 告警 / %d 异常", okCount, warnCount, errCount))
	lines = append(lines, fmt.Sprintf("查询时间: %s", now))
	lines = append(lines, "")
	lines = append(lines, "👇 需要充值时，点下方站点按钮直达：")

	kb := siteLinkButtons(allSites, false)
	kb.InlineKeyboard = append(kb.InlineKeyboard,
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔄 刷新余额", "refresh"),
			tgbotapi.NewInlineKeyboardButtonData("🔙 主菜单", "start"),
		),
	)

	msg := tgbotapi.NewMessage(chatID, strings.Join(lines, "\n"))
	msg.ReplyMarkup = kb
	b.api.Send(msg)
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
/balance - 查询所有站点余额（额度总览 + 充值按钮）
/topup - 打开各站点充值/控制台
/refresh - 重新查询并再次展示额度总览
/status - 查看系统运行状态
/help - 显示此帮助信息

提示: 在 Web 面板可为站点单独填写「充值/控制台链接」；留空则使用接口地址。`

	reply := tgbotapi.NewMessage(chatID, text)
	reply.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("💳 去充值", "topup"),
			tgbotapi.NewInlineKeyboardButtonData("🔙 主菜单", "start"),
		),
	)
	b.api.Send(reply)
}

func (b *Bot) sendText(chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	b.api.Send(msg)
}
