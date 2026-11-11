package scheduler

import (
	"context"
	"log"
	"strconv"
	"sync"
	"time"

	"upstream-balance/internal/checker"
	"upstream-balance/internal/forecast"
	"upstream-balance/internal/model"
	"upstream-balance/internal/notifier"
	"upstream-balance/internal/store"
)

type Scheduler struct {
	checker    *checker.Checker
	sites      *store.SiteStore
	thresholds *store.ThresholdStore
	settings   *store.SettingStore
	forecast   *forecast.Service
	notifyFn   func() *notifier.Telegram
	emailFn    func() *notifier.Email

	mu         sync.Mutex
	cancel     context.CancelFunc
	LastPollAt string
	NextPollAt string
	running    bool
}

func New(
	chk *checker.Checker,
	sites *store.SiteStore,
	thresholds *store.ThresholdStore,
	settings *store.SettingStore,
	fc *forecast.Service,
	notifyFn func() *notifier.Telegram,
	emailFn func() *notifier.Email,
) *Scheduler {
	return &Scheduler{
		checker:    chk,
		sites:      sites,
		thresholds: thresholds,
		settings:   settings,
		forecast:   fc,
		notifyFn:   notifyFn,
		emailFn:    emailFn,
	}
}

func (s *Scheduler) Start(ctx context.Context) {
	go s.pollAll()

	go func() {
		for {
			interval := s.getInterval()
			s.mu.Lock()
			s.NextPollAt = time.Now().Add(interval).UTC().Format(time.RFC3339)
			s.mu.Unlock()

			ctx2, cancel := context.WithCancel(ctx)
			s.mu.Lock()
			s.cancel = cancel
			s.mu.Unlock()

			select {
			case <-time.After(interval):
				cancel()
				s.pollAll()
			case <-ctx2.Done():
				return
			case <-ctx.Done():
				cancel()
				return
			}
		}
	}()
}

func (s *Scheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
	}
}

func (s *Scheduler) Restart() {
	s.mu.Lock()
	if s.cancel != nil {
		s.cancel()
	}
	s.mu.Unlock()
}

func (s *Scheduler) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

func (s *Scheduler) PollAll() {
	s.pollAll()
}

func (s *Scheduler) pollAll() {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.running = false
		s.LastPollAt = model.Now()
		s.mu.Unlock()
	}()

	sites, err := s.sites.List()
	if err != nil {
		log.Printf("[scheduler] failed to list sites: %v", err)
		return
	}
	if len(sites) == 0 {
		return
	}

	maxConc := 10
	if v, err := s.settings.Get("max_concurrency"); err == nil && v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			maxConc = n
		}
	}

	results := s.checker.CheckWithConcurrency(sites, maxConc)

	tg := s.notifyFn()
	mailer := s.emailFn()

	for i := range sites {
		site := &sites[i]
		result, ok := results[site.ID]
		if !ok {
			continue
		}

		status := "ok"
		if result.Error != "" {
			status = "error"
		}

		if result.Error == "" {
			s.forecast.Record(site.ID, result)
			if err := s.sites.UpdateBalance(site.ID, result.Balance, result.Unit, result.DetectedType, "ok", ""); err != nil {
				log.Printf("[scheduler] failed to update site %s: %v", site.Name, err)
			}
		} else {
			if err := s.sites.UpdateBalance(site.ID, site.Balance, site.BalanceUnit, site.DetectedType, "error", result.Error); err != nil {
				log.Printf("[scheduler] failed to update site %s: %v", site.Name, err)
			}
			continue
		}

		thresholdList, err := s.thresholds.ListBySite(site.ID)
		if err != nil {
			log.Printf("[scheduler] failed to list thresholds for site %s: %v", site.Name, err)
			continue
		}

		hasLow := false
		for _, t := range thresholdList {
			if result.Balance < t.Amount {
				hasLow = true
				if !t.Triggered && s.sendAlert(tg, mailer, site, result.Balance, t.Amount) {
					s.thresholds.SetTriggered(t.ID, true)
				}
			} else {
				if t.Triggered {
					s.thresholds.SetTriggered(t.ID, false)
				}
			}
		}

		if hasLow {
			status = "low"
		}
		if status != "ok" {
			s.sites.UpdateBalance(site.ID, result.Balance, result.Unit, result.DetectedType, status, "")
		}

		if alert, commit := s.forecast.Evaluate(*site, s.forecast.For(site.ID, result.Balance)); alert != nil {
			if s.sendForecastAlert(tg, mailer, site, alert) {
				commit()
			}
		}
	}

	s.maybeDailyReport(tg)
}

// sendForecastAlert 发消耗预测提醒（Telegram + 邮件）；规则同 sendAlert：有通道但全失败才返回 false。
func (s *Scheduler) sendForecastAlert(tg *notifier.Telegram, mailer *notifier.Email, site *model.Site, alert *forecast.Alert) bool {
	attempted, delivered := false, false
	if tg != nil && tg.IsConfigured() {
		attempted = true
		kb := notifier.SiteButtons([]string{site.Name}, []string{site.LinkURL()})
		if err := tg.SendPlain(alert.Text, kb); err != nil {
			log.Printf("[scheduler] failed to send forecast alert for site %s: %v", site.Name, err)
		} else {
			delivered = true
		}
	}
	if mailer != nil && mailer.IsConfigured() {
		attempted = true
		body := alert.Text
		if link := site.LinkURL(); link != "" {
			body += "\n\n充值 / 控制台：" + link
		}
		if err := mailer.Send(alert.Subject, body); err != nil {
			log.Printf("[scheduler] failed to send forecast email for site %s: %v", site.Name, err)
		} else {
			delivered = true
		}
	}
	if delivered {
		log.Printf("[scheduler] forecast alert sent for site %s: %s", site.Name, alert.Subject)
	}
	return delivered || !attempted
}

// maybeDailyReport 每天到点后的第一次巡检发日报（只发 Telegram）。
func (s *Scheduler) maybeDailyReport(tg *notifier.Telegram) {
	if tg == nil || !tg.IsConfigured() || !s.forecast.ReportDue() {
		return
	}
	sites, err := s.sites.List()
	if err != nil || len(sites) == 0 {
		return
	}
	names := make([]string, len(sites))
	links := make([]string, len(sites))
	for i := range sites {
		names[i], links[i] = sites[i].Name, sites[i].LinkURL()
	}
	if err := tg.SendPlain(s.forecast.DailyReport(sites), notifier.SiteButtons(names, links)); err != nil {
		log.Printf("[scheduler] failed to send daily report: %v", err)
		return
	}
	s.forecast.MarkReported()
	log.Printf("[scheduler] daily report sent")
}

// sendAlert pushes a low-balance alert to every configured channel. It returns false only
// when channels exist but all of them failed, so the alert is retried on the next poll
// instead of being silently lost.
func (s *Scheduler) sendAlert(tg *notifier.Telegram, mailer *notifier.Email, site *model.Site, balance, threshold float64) bool {
	checkTime := model.Now()
	attempted, delivered := false, false

	if tg != nil && tg.IsConfigured() {
		attempted = true
		if err := tg.SendAlert(site.Name, balance, threshold, checkTime, site.LinkURL()); err != nil {
			log.Printf("[scheduler] failed to send alert for site %s: %v", site.Name, err)
		} else {
			delivered = true
		}
	}

	if mailer != nil && mailer.IsConfigured() {
		attempted = true
		if err := mailer.SendAlert(site.Name, balance, threshold, checkTime, site.LinkURL()); err != nil {
			log.Printf("[scheduler] failed to send email alert for site %s: %v", site.Name, err)
		} else {
			delivered = true
			log.Printf("[scheduler] email alert sent for site %s (balance %.2f < %.2f)", site.Name, balance, threshold)
		}
	}

	return delivered || !attempted
}

func (s *Scheduler) getInterval() time.Duration {
	v, err := s.settings.Get("interval_minutes")
	if err != nil || v == "" {
		return 30 * time.Minute
	}
	m, err := strconv.Atoi(v)
	if err != nil || m < 5 {
		return 30 * time.Minute
	}
	return time.Duration(m) * time.Minute
}
