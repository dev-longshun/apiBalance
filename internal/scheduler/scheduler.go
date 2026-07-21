package scheduler

import (
	"context"
	"log"
	"strconv"
	"sync"
	"time"

	"upstream-balance/internal/checker"
	"upstream-balance/internal/model"
	"upstream-balance/internal/notifier"
	"upstream-balance/internal/store"
)

type Scheduler struct {
	checker    *checker.Checker
	sites      *store.SiteStore
	thresholds *store.ThresholdStore
	settings   *store.SettingStore
	notifyFn   func() *notifier.Telegram

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
	notifyFn func() *notifier.Telegram,
) *Scheduler {
	return &Scheduler{
		checker:    chk,
		sites:      sites,
		thresholds: thresholds,
		settings:   settings,
		notifyFn:   notifyFn,
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
				if !t.Triggered {
					s.thresholds.SetTriggered(t.ID, true)
					if tg != nil && tg.IsConfigured() {
						checkTime := model.Now()
						if err := tg.SendAlert(site.Name, result.Balance, t.Amount, checkTime, site.LinkURL()); err != nil {
							log.Printf("[scheduler] failed to send alert for site %s: %v", site.Name, err)
						}
					}
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
	}
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
