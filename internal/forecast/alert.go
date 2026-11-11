package forecast

import (
	"fmt"
	"html"
	"strings"

	"upstream-balance/internal/model"
)

const (
	kindHours  = "hours"
	kindDays   = "days"
	quietUntil = 8 // 北京时间这之前不发「考虑充值」这种不急的提醒
)

// Alert 是一条要发出去的预测提醒：HTML 发 Telegram（余额加粗），Text 是同内容的纯文本，发邮件。
type Alert struct {
	Subject string
	Text    string
	HTML    string
}

// Evaluate 在定时巡检后调用：按估算决定这个站点要不要发提醒，并更新去重状态。
//
//   - 按最近 1 小时速度：剩余 < HoursWarn 发一次「消耗突增」，< HoursUrgent 再发一次「紧急」；
//     恢复到 HoursWarn 的 1.5 倍以上（充值了或速度降下来）才清状态，免得在线附近来回刷。
//   - 按日均：剩余 < DailyWarn 小时时发一次「考虑充值」（不急，北京时间 0–8 点不发，等到早上）；
//     之后每天由日报继续列出，不再单独刷；恢复到 DailyWarn 以上才清状态。
//
// 同一次巡检两种都触发时合成一条，以更紧急的为标题。返回的 commit 在发送成功后调用，
// 才记下「已提醒」；发送失败就不调，下一轮巡检会重发。
func (s *Service) Evaluate(site model.Site, f Forecast) (alert *Alert, commit func()) {
	cfg := s.Config()
	now := s.now()

	hourLevel := 0
	if f.HoursLeft != nil {
		switch {
		case *f.HoursLeft < cfg.HoursUrgent:
			hourLevel = 2
		case *f.HoursLeft < cfg.HoursWarn:
			hourLevel = 1
		}
	}
	sendHours := false
	if st, ok := s.usage.AlertState(site.ID, kindHours); hourLevel > 0 {
		sendHours = !ok || hourLevel > st.Level
	} else if ok && (f.HoursLeft == nil || *f.HoursLeft >= cfg.HoursWarn*1.5) {
		_ = s.usage.ClearAlertState(site.ID, kindHours)
	}

	daysLow := f.DaysLeft != nil && *f.DaysLeft*24 < cfg.DailyWarn
	sendDays := false
	if _, ok := s.usage.AlertState(site.ID, kindDays); daysLow {
		sendDays = !ok && now.In(Beijing).Hour() >= quietUntil
	} else if ok {
		_ = s.usage.ClearAlertState(site.ID, kindDays)
	}

	if !sendHours && !sendDays {
		return nil, func() {}
	}
	commit = func() {
		if sendHours {
			_ = s.usage.SetAlertState(site.ID, kindHours, hourLevel, now.Unix())
		}
		if sendDays {
			_ = s.usage.SetAlertState(site.ID, kindDays, 1, now.Unix())
		}
	}
	return s.compose(site, f, cfg, hourLevel, sendHours, sendDays), commit
}

func (s *Service) compose(site model.Site, f Forecast, cfg Config, hourLevel int, sendHours, sendDays bool) *Alert {
	var head, reason, subject string
	if sendHours {
		rate := fmt.Sprintf("%s/小时", Money(*f.HourRate))
		if f.Surge {
			rate += fmt.Sprintf("，是平时的 %.1f 倍", f.SurgeX)
		}
		left := Duration(*f.HoursLeft)
		if hourLevel == 2 {
			head = "🚨 余额快用完了 · " + site.Name
			reason = fmt.Sprintf("按最近 1 小时的速度（%s），%s后用完，请尽快充值。", rate, left)
		} else {
			head = "⚠️ 消耗加快 · " + site.Name
			reason = fmt.Sprintf("按最近 1 小时的速度（%s），%s后用完，建议提前充值。", rate, left)
		}
		subject = fmt.Sprintf("%s %s后用完", site.Name, left)
	} else {
		head = "💡 考虑充值 · " + site.Name
		reason = fmt.Sprintf("按最近 7 天日均 %s，余额%s后用完。", Money(*f.DailyAvg), Duration(*f.DaysLeft*24))
		subject = fmt.Sprintf("%s 余额%s后用完", site.Name, Duration(*f.DaysLeft*24))
	}

	detail := fmt.Sprintf("近 1 小时：%s · 日均：%s · 昨天：%s", optMoney(f.HourRate), optMoney(f.DailyAvg), optMoney(f.Yesterday))
	at := "时间：" + s.now().In(Beijing).Format("2006-01-02 15:04") + "（北京）"
	text := strings.Join([]string{head, "", reason, "", "当前余额：" + Money(f.Balance), detail, at}, "\n")
	esc := html.EscapeString
	htmlText := strings.Join([]string{"<b>" + esc(head) + "</b>", "", "当前余额：<b>" + Money(f.Balance) + "</b>", "",
		esc(reason), "", esc(detail), esc(at)}, "\n")
	return &Alert{Subject: "余额预警：" + subject, Text: text, HTML: htmlText}
}

// ReportDue 表示到了今天发日报的时间、且今天还没发过；调用方发成功后调 MarkReported。
func (s *Service) ReportDue() bool {
	now := s.now().In(Beijing)
	if now.Hour() < s.Config().ReportHour {
		return false
	}
	last, _ := s.settings.Get("daily_report_date")
	return last != now.Format("2006-01-02")
}

func (s *Service) MarkReported() {
	_ = s.settings.Set("daily_report_date", s.now().In(Beijing).Format("2006-01-02"))
}

// DailyReport 生成每日日报（Telegram HTML）：各站点余额、昨天消耗、日均、可撑多久，末尾列出建议充值的站点。
func (s *Service) DailyReport(sites []model.Site) string {
	cfg := s.Config()
	lines := []string{"📋 <b>每日额度日报</b> · " + s.now().In(Beijing).Format("01-02"), ""}
	var low []string
	for _, site := range sites {
		if site.Status == "error" {
			lines = append(lines, SiteBlock("🔴", site.Name, site.Balance, "查询失败，上面是最后一次查到的余额"), "")
			continue
		}
		f := s.For(site.ID, site.Balance)
		icon := f.Icon(cfg, site.Status == "low")
		if icon != "🟢" {
			low = append(low, site.Name)
		}
		lines = append(lines, SiteBlock(icon, site.Name, site.Balance, f.ReportLine(cfg)), "")
	}
	if len(low) > 0 {
		lines = append(lines, "👉 建议充值："+html.EscapeString(strings.Join(low, "、")))
	} else {
		lines = append(lines, fmt.Sprintf("✅ 各站点都能撑 %g 小时以上，暂时不用充值。", cfg.DailyWarn))
	}
	return strings.Join(lines, "\n")
}
