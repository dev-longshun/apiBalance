// Package forecast 根据余额快照与上游自报的每日消耗，估算各站点的消耗速度和还能撑多久，
// 并决定何时发「考虑充值 / 消耗突增 / 紧急」提醒和每日日报。
package forecast

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"upstream-balance/internal/model"
	"upstream-balance/internal/store"
)

// Beijing 固定 UTC+8，容器里不一定带时区数据。
var Beijing = time.FixedZone("Asia/Shanghai", 8*3600)

const (
	snapshotKeep  = 35 * 24 * time.Hour
	hourWindow    = 65 * time.Minute // 「最近 1 小时」取这段时间内的快照（留 5 分钟抖动）
	hourMinSpan   = 45 * time.Minute // 快照跨度不到这么长，速度不可信
	hourMaxStale  = 45 * time.Minute // 最新一条快照太旧就不算「最近」
	dailyDays     = 7
	dailyMinSpan  = 20 * time.Hour // 上游不报每日消耗时，靠快照估日均至少要这么长
	minRate       = 0.01
	surgeMinPerHr = 1.0 // 每小时不到 $1 不算突增，免得半夜零星请求被放大
)

// Config 是提醒阈值，存在 settings 表里，缺省用下面的默认值。
type Config struct {
	DaysWarn    float64 // 按日均剩余不到几天 → 考虑充值
	HoursWarn   float64 // 按最近 1 小时速度剩余不到几小时 → 消耗突增
	HoursUrgent float64 // 同上，更紧急
	SurgeRatio  float64 // 最近 1 小时速度达到日均小时速度的几倍算突增
	ReportHour  int     // 每日日报的北京时间整点
}

var defaultConfig = Config{DaysWarn: 3, HoursWarn: 6, HoursUrgent: 2, SurgeRatio: 3, ReportHour: 10}

// Forecast 是一个站点此刻的消耗估算；指针为 nil 表示数据不够，还在积累。
type Forecast struct {
	Balance   float64
	HourRate  *float64 // $/小时
	DailyAvg  *float64 // $/天，最近 7 天
	Yesterday *float64
	HoursLeft *float64 // 按最近 1 小时速度
	DaysLeft  *float64 // 按日均
	Surge     bool
	SurgeX    float64 // 最近 1 小时速度是平时的几倍
}

// HoursToEmpty 取两种估法里更早用完的那个（小时）。
func (f Forecast) HoursToEmpty() *float64 {
	var best *float64
	if f.HoursLeft != nil {
		best = f.HoursLeft
	}
	if f.DaysLeft != nil {
		h := *f.DaysLeft * 24
		if best == nil || h < *best {
			best = &h
		}
	}
	return best
}

type Service struct {
	usage    *store.UsageStore
	settings *store.SettingStore
	now      func() time.Time

	mu        sync.Mutex
	lastPrune time.Time
}

func New(usage *store.UsageStore, settings *store.SettingStore) *Service {
	return &Service{usage: usage, settings: settings, now: time.Now}
}

func (s *Service) Config() Config {
	c := defaultConfig
	num := func(key string, dst *float64) {
		if v, _ := s.settings.Get(key); v != "" {
			if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
				*dst = f
			}
		}
	}
	num("forecast_days_warn", &c.DaysWarn)
	num("forecast_hours_warn", &c.HoursWarn)
	num("forecast_hours_urgent", &c.HoursUrgent)
	num("forecast_surge_ratio", &c.SurgeRatio)
	if v, _ := s.settings.Get("daily_report_hour"); v != "" {
		if h, err := strconv.Atoi(v); err == nil && h >= 0 && h < 24 {
			c.ReportHour = h
		}
	}
	return c
}

// Record 把一次成功查询的余额和上游每日消耗存下来。查询失败的结果直接忽略。
func (s *Service) Record(siteID string, r *model.CheckResult) {
	if r == nil || r.Error != "" {
		return
	}
	now := s.now()
	var total *float64
	if v, ok := r.Extra["total_cost"]; ok {
		total = &v
	}
	_ = s.usage.AddSnapshot(siteID, now.Unix(), r.Balance, total)
	_ = s.usage.UpsertDaily(siteID, r.Daily)

	s.mu.Lock()
	prune := now.Sub(s.lastPrune) > time.Hour
	if prune {
		s.lastPrune = now
	}
	s.mu.Unlock()
	if prune {
		_ = s.usage.PruneSnapshots(now.Add(-snapshotKeep).Unix())
	}
}

// For 估算一个站点此刻的消耗情况。
func (s *Service) For(siteID string, balance float64) Forecast {
	now := s.now()
	snaps, _ := s.usage.Snapshots(siteID, now.Add(-dailyDays*24*time.Hour).Unix())
	var daily []model.DailyCost
	hasDaily := s.usage.HasDaily(siteID)
	if hasDaily {
		daily, _ = s.usage.Daily(siteID, "")
	}
	return compute(now, balance, snaps, daily, hasDaily, s.Config())
}

func compute(now time.Time, balance float64, snaps []store.Snapshot, daily []model.DailyCost, hasDaily bool, cfg Config) Forecast {
	f := Forecast{Balance: balance}
	f.HourRate = hourRate(now, snaps)

	// 上游 daily_usage 是「这把 Key」的用量，余额却是整个钱包：同账户下别的 Key 在花钱时它偏低，
	// 甚至一直是 0。所以两种估法取大：刚上线时先用上游的，快照满 20 小时后以钱包实际下降为准。
	var upAvg, upY *float64
	if hasDaily {
		upAvg, upY = dailyFromUpstream(now, daily)
		if upAvg != nil && *upAvg == 0 {
			upAvg, upY = nil, nil
		}
	}
	snapAvg, snapY := dailyFromSnapshots(now, snaps)
	f.DailyAvg, f.Yesterday = maxPtr(upAvg, snapAvg), maxPtr(upY, snapY)

	if f.HourRate != nil && *f.HourRate >= minRate {
		h := balance / *f.HourRate
		f.HoursLeft = &h
	}
	if f.DailyAvg != nil && *f.DailyAvg >= minRate {
		d := balance / *f.DailyAvg
		f.DaysLeft = &d
	}
	if f.HourRate != nil && f.DailyAvg != nil && *f.DailyAvg >= minRate {
		f.SurgeX = *f.HourRate / (*f.DailyAvg / 24)
		f.Surge = f.SurgeX >= cfg.SurgeRatio && *f.HourRate >= surgeMinPerHr
	}
	return f
}

// consumed 估算一串快照（由早到晚）之间花掉的钱：余额下降之和（余额上涨视为充值，不算），
// 与上游累计扣费的增量取较大者——有的上游不报扣费（一直是 0），有的期间充过值。
func consumed(snaps []store.Snapshot) float64 {
	if len(snaps) < 2 {
		return 0
	}
	drop := 0.0
	for i := 1; i < len(snaps); i++ {
		if d := snaps[i-1].Balance - snaps[i].Balance; d > 0 {
			drop += d
		}
	}
	first, last := snaps[0], snaps[len(snaps)-1]
	if first.TotalCost != nil && last.TotalCost != nil {
		if c := *last.TotalCost - *first.TotalCost; c > drop {
			return c
		}
	}
	return drop
}

func maxPtr(a, b *float64) *float64 {
	if a == nil || (b != nil && *b > *a) {
		return b
	}
	return a
}

func between(snaps []store.Snapshot, from, to int64) []store.Snapshot {
	var out []store.Snapshot
	for _, s := range snaps {
		if s.At >= from && s.At <= to {
			out = append(out, s)
		}
	}
	return out
}

func hourRate(now time.Time, snaps []store.Snapshot) *float64 {
	w := between(snaps, now.Add(-hourWindow).Unix(), now.Unix())
	if len(w) < 2 {
		return nil
	}
	first, last := w[0], w[len(w)-1]
	span := time.Duration(last.At-first.At) * time.Second
	if span < hourMinSpan || now.Sub(time.Unix(last.At, 0)) > hourMaxStale {
		return nil
	}
	r := consumed(w) / span.Hours()
	return &r
}

func dayStart(t time.Time) time.Time {
	t = t.In(Beijing)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, Beijing)
}

// dailyFromUpstream：最近 7 个完整自然日（不含今天）的平均。上游没记录的日子按 0 算，
// 站点历史不足 7 天时按实际天数平均。
func dailyFromUpstream(now time.Time, daily []model.DailyCost) (avg, yesterday *float64) {
	today := dayStart(now)
	todayStr := today.Format("2006-01-02")
	fromStr := today.AddDate(0, 0, -dailyDays).Format("2006-01-02")
	yStr := today.AddDate(0, 0, -1).Format("2006-01-02")

	var earliest string
	sum, y := 0.0, 0.0
	for _, d := range daily {
		if earliest == "" || d.Date < earliest {
			earliest = d.Date
		}
		if d.Date >= fromStr && d.Date < todayStr {
			sum += d.Cost
		}
		if d.Date == yStr {
			y = d.Cost
		}
	}
	days := dailyDays
	if earliest > fromStr {
		first, err := time.ParseInLocation("2006-01-02", earliest, Beijing)
		if err != nil {
			return nil, nil
		}
		days = int(today.Sub(first).Hours()/24 + 0.5)
	}
	if days < 1 {
		return nil, nil
	}
	a := sum / float64(days)
	return &a, &y
}

// dailyFromSnapshots：上游不报每日消耗时，用最近 7 天的余额快照估算。
func dailyFromSnapshots(now time.Time, snaps []store.Snapshot) (avg, yesterday *float64) {
	if len(snaps) >= 2 {
		span := time.Duration(snaps[len(snaps)-1].At-snaps[0].At) * time.Second
		if span >= dailyMinSpan {
			a := consumed(snaps) / span.Hours() * 24
			avg = &a
		}
	}
	today := dayStart(now)
	slack := int64(30 * 60)
	y := between(snaps, today.AddDate(0, 0, -1).Unix()-slack, today.Unix()+slack)
	if len(y) >= 2 && time.Duration(y[len(y)-1].At-y[0].At)*time.Second >= dailyMinSpan {
		v := consumed(y)
		yesterday = &v
	}
	return avg, yesterday
}

// ---- 文案 ----

func Money(v float64) string {
	neg := v < 0
	v = math.Abs(v)
	s := strconv.FormatFloat(v, 'f', 2, 64)
	intPart, frac := s[:len(s)-3], s[len(s)-3:]
	var b strings.Builder
	for i, c := range intPart {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	if neg {
		return "-$" + b.String() + frac
	}
	return "$" + b.String() + frac
}

func optMoney(v *float64) string {
	if v == nil {
		return "积累中"
	}
	return Money(*v)
}

// Duration 把小时数写成「约 X 小时 / 约 X 天」。
func Duration(hours float64) string {
	switch {
	case hours < 1:
		return fmt.Sprintf("约 %.0f 分钟", math.Max(hours*60, 1))
	case hours < 48:
		return fmt.Sprintf("约 %.1f 小时", hours)
	case hours < 24*100:
		return fmt.Sprintf("约 %.1f 天", hours/24)
	default:
		return "超过 100 天"
	}
}

// Warn 表示这个站点按当前估算已经该提醒了。
func (f Forecast) Warn(cfg Config) bool {
	if f.HoursLeft != nil && *f.HoursLeft < cfg.HoursWarn {
		return true
	}
	return f.DaysLeft != nil && *f.DaysLeft < cfg.DaysWarn
}

// Line 是额度总览里每个站点下面那一行：近 1 小时 · 日均 · 可撑多久。
func (f Forecast) Line(cfg Config) string {
	parts := []string{"近 1 小时 " + optMoney(f.HourRate), "日均 " + optMoney(f.DailyAvg)}
	if h := f.HoursToEmpty(); h != nil {
		parts = append(parts, "可撑"+Duration(*h))
	}
	line := strings.Join(parts, " · ")
	if f.Surge {
		line += fmt.Sprintf(" · 突增 %.1f 倍", f.SurgeX)
	}
	if f.Warn(cfg) {
		line += " ⚠️"
	}
	return line
}

// ReportLine 是日报里每个站点下面那一行：昨天 · 日均 · 可撑多久。
func (f Forecast) ReportLine(cfg Config) string {
	parts := []string{"昨天 " + optMoney(f.Yesterday), "日均 " + optMoney(f.DailyAvg)}
	if h := f.HoursToEmpty(); h != nil {
		parts = append(parts, "可撑"+Duration(*h))
	}
	line := strings.Join(parts, " · ")
	if f.Warn(cfg) {
		line += " ⚠️"
	}
	return line
}
