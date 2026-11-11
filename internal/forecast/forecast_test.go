package forecast

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"upstream-balance/internal/model"
	"upstream-balance/internal/store"
)

func f64(v float64) *float64 { return &v }

var now = time.Date(2026, 11, 11, 14, 0, 0, 0, Beijing)

func snapAt(minAgo int, balance float64, total *float64) store.Snapshot {
	return store.Snapshot{At: now.Add(-time.Duration(minAgo) * time.Minute).Unix(), Balance: balance, TotalCost: total}
}

func TestConsumedIgnoresTopUpAndPrefersLargerCost(t *testing.T) {
	// 100 → 90 → 原地充值到 190 → 185：余额下降共 15
	snaps := []store.Snapshot{snapAt(30, 100, nil), snapAt(20, 90, nil), snapAt(10, 190, nil), snapAt(0, 185, nil)}
	if got := consumed(snaps); got != 15 {
		t.Fatalf("drop = %v, want 15", got)
	}
	// 上游累计扣费增量 18 比余额下降 15 大（同一区间内边花边充）→ 取 18
	snaps[0].TotalCost, snaps[3].TotalCost = f64(10), f64(28)
	if got := consumed(snaps); got != 18 {
		t.Fatalf("cost = %v, want 18", got)
	}
	// 上游报的扣费一直是 0（号商-kirogpt）→ 用余额下降
	snaps[0].TotalCost, snaps[3].TotalCost = f64(0), f64(0)
	if got := consumed(snaps); got != 15 {
		t.Fatalf("zero cost = %v, want 15", got)
	}
}

func TestHourRate(t *testing.T) {
	// 10 分钟一条，一小时从 100 降到 88 → $12/小时
	var snaps []store.Snapshot
	for i := 0; i <= 6; i++ {
		snaps = append(snaps, snapAt(60-i*10, 100-float64(i)*2, nil))
	}
	r := hourRate(now, snaps)
	if r == nil || *r < 11.99 || *r > 12.01 {
		t.Fatalf("rate = %v, want 12", r)
	}
	// 30 分钟一条（公司那套的巡检间隔）也能算
	r = hourRate(now, []store.Snapshot{snapAt(60, 100, nil), snapAt(30, 97, nil), snapAt(0, 94, nil)})
	if r == nil || *r != 6 {
		t.Fatalf("30min rate = %v, want 6", r)
	}
	// 刚上线只有 20 分钟数据 → 积累中
	if r := hourRate(now, []store.Snapshot{snapAt(20, 100, nil), snapAt(0, 99, nil)}); r != nil {
		t.Fatalf("short span rate = %v, want nil", *r)
	}
	// 最新快照是 2 小时前 → 不算「最近」
	if r := hourRate(now, []store.Snapshot{snapAt(180, 100, nil), snapAt(120, 90, nil)}); r != nil {
		t.Fatalf("stale rate = %v, want nil", *r)
	}
}

func TestDailyFromUpstream(t *testing.T) {
	daily := []model.DailyCost{
		{Date: "2026-11-01", Cost: 999}, // 超出 7 天，不计
		{Date: "2026-11-04", Cost: 10},
		{Date: "2026-11-05", Cost: 20},
		// 11-06 没记录 → 按 0
		{Date: "2026-11-07", Cost: 30},
		{Date: "2026-11-08", Cost: 40},
		{Date: "2026-11-09", Cost: 50},
		{Date: "2026-11-10", Cost: 60},
		{Date: "2026-11-11", Cost: 5}, // 今天，不完整，不计
	}
	avg, y := dailyFromUpstream(now, daily)
	if avg == nil || *avg != 30 || *y != 60 { // (10+20+0+30+40+50+60)/7 = 30
		t.Fatalf("avg = %v, yesterday = %v", avg, y)
	}
	// 站点才用了 2 天：按 2 天平均
	avg, _ = dailyFromUpstream(now, []model.DailyCost{{Date: "2026-11-09", Cost: 10}, {Date: "2026-11-10", Cost: 30}})
	if avg == nil || *avg != 20 {
		t.Fatalf("short history avg = %v, want 20", avg)
	}
}

func TestComputeSurgeAndLeft(t *testing.T) {
	cfg := defaultConfig
	// 叮当猫：余额 50，日均 38.1 → 约 1.3 天；最近 1 小时 $12 → 约 4.2 小时，是平时的 7.6 倍
	var snaps []store.Snapshot
	for i := 0; i <= 6; i++ {
		snaps = append(snaps, snapAt(60-i*10, 62-float64(i)*2, nil))
	}
	daily := []model.DailyCost{}
	for d := 1; d <= 7; d++ {
		daily = append(daily, model.DailyCost{Date: now.AddDate(0, 0, -d).Format("2006-01-02"), Cost: 38.1})
	}
	f := compute(now, 50, snaps, daily, true, cfg)
	if f.DaysLeft == nil || *f.DaysLeft < 1.3 || *f.DaysLeft > 1.32 {
		t.Fatalf("days left = %v", f.DaysLeft)
	}
	if f.HoursLeft == nil || *f.HoursLeft < 4.1 || *f.HoursLeft > 4.2 || !f.Surge {
		t.Fatalf("hours left = %v surge = %v x=%v", f.HoursLeft, f.Surge, f.SurgeX)
	}
	if !f.Warn(cfg) || !strings.Contains(f.Line(cfg), "突增") {
		t.Fatalf("line = %q", f.Line(cfg))
	}
	// 半夜零星请求：$0.3/小时虽是平时的 3 倍以上，但不到 $1/小时，不算突增
	quiet := []store.Snapshot{snapAt(60, 1000, nil), snapAt(0, 999.7, nil)}
	low := []model.DailyCost{{Date: now.AddDate(0, 0, -1).Format("2006-01-02"), Cost: 2}}
	if f := compute(now, 999.7, quiet, low, true, cfg); f.Surge {
		t.Fatalf("tiny rate flagged as surge: %v", f.SurgeX)
	}
}

func TestDailyTakesLargerOfUpstreamAndWallet(t *testing.T) {
	cfg := defaultConfig
	// 钱包 24 小时降了 $48（每 10 分钟 $0.333），上游只报这把 Key 每天 $10
	var snaps []store.Snapshot
	for i := 0; i <= 144; i++ {
		snaps = append(snaps, snapAt(1440-i*10, 500-float64(i)/3, nil))
	}
	var daily []model.DailyCost
	for d := 1; d <= 7; d++ {
		daily = append(daily, model.DailyCost{Date: now.AddDate(0, 0, -d).Format("2006-01-02"), Cost: 10})
	}
	f := compute(now, 452, snaps, daily, true, cfg)
	if f.DailyAvg == nil || *f.DailyAvg < 47.9 || *f.DailyAvg > 48.1 {
		t.Fatalf("avg = %v, want wallet-based 48", f.DailyAvg)
	}
	// 快照不满 20 小时：先用上游的 $10
	f = compute(now, 452, snaps[130:], daily, true, cfg)
	if f.DailyAvg == nil || *f.DailyAvg != 10 {
		t.Fatalf("avg = %v, want upstream 10", f.DailyAvg)
	}
	// 上游这几天全是 0（钱是同账户别的 Key 花的）→ 当作没数据
	zero := []model.DailyCost{{Date: "2026-10-13", Cost: 24.28}}
	if f := compute(now, 452, snaps[130:], zero, true, cfg); f.DailyAvg != nil {
		t.Fatalf("all-zero upstream should be 积累中, got %v", *f.DailyAvg)
	}
}

func TestDuration(t *testing.T) {
	for hours, want := range map[float64]string{0.2: "约 12 分钟", 4.16: "约 4.2 小时", 31: "约 31.0 小时", 940: "约 39.2 天", 5000: "超过 100 天"} {
		if got := Duration(hours); got != want {
			t.Errorf("Duration(%v) = %q, want %q", hours, got, want)
		}
	}
	if got := Money(1607.654); got != "$1,607.65" {
		t.Errorf("Money = %q", got)
	}
}

// ---- 提醒去重：用真实 SQLite ----

func newService(t *testing.T) (*Service, *store.DB, *time.Time) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	sites := store.NewSiteStore(db)
	if err := sites.Create(&model.Site{ID: "s1", Name: "叮当猫", BaseURL: "https://x", AuthType: "bearer",
		CreatedAt: model.Now(), UpdatedAt: model.Now()}); err != nil {
		t.Fatal(err)
	}
	clock := now
	svc := New(store.NewUsageStore(db), store.NewSettingStore(db))
	svc.now = func() time.Time { return clock }
	return svc, db, &clock
}

func TestEvaluateHoursEscalatesOnceAndResets(t *testing.T) {
	svc, _, clock := newService(t)
	site := model.Site{ID: "s1", Name: "叮当猫"}
	fc := func(hoursLeft float64) Forecast {
		return Forecast{Balance: 50, HourRate: f64(50 / hoursLeft), HoursLeft: f64(hoursLeft)}
	}
	send := func(f Forecast) *Alert {
		a, commit := svc.Evaluate(site, f)
		commit()
		return a
	}
	if a := send(fc(5)); a == nil || !strings.Contains(a.Text, "消耗加快") {
		t.Fatalf("first warn = %+v", a)
	}
	if a := send(fc(4)); a != nil {
		t.Fatalf("repeat warn should be silent: %+v", a)
	}
	if a := send(fc(1.5)); a == nil || !strings.Contains(a.Text, "🚨") {
		t.Fatalf("urgent = %+v", a)
	}
	if a := send(fc(1)); a != nil {
		t.Fatalf("repeat urgent should be silent")
	}
	send(fc(7)) // 6–9 小时之间：不清状态
	if a := send(fc(5)); a != nil {
		t.Fatalf("hysteresis broken: %+v", a)
	}
	send(fc(20)) // 充值后恢复 → 清状态
	*clock = clock.Add(time.Hour)
	if a := send(fc(5)); a == nil {
		t.Fatalf("should warn again after reset")
	}
}

func TestEvaluateUncommittedAlertIsRetried(t *testing.T) {
	svc, _, _ := newService(t)
	site := model.Site{ID: "s1", Name: "叮当猫"}
	f := Forecast{Balance: 50, HourRate: f64(25), HoursLeft: f64(2.5)}
	if a, _ := svc.Evaluate(site, f); a == nil { // 发送失败：不 commit
		t.Fatal("want alert")
	}
	if a, _ := svc.Evaluate(site, f); a == nil {
		t.Fatal("unsent alert must be retried on next poll")
	}
}

func TestEvaluateDaysQuietHoursAndOnce(t *testing.T) {
	svc, _, clock := newService(t)
	site := model.Site{ID: "s1", Name: "叮当猫"}
	f := Forecast{Balance: 50, DailyAvg: f64(38), DaysLeft: f64(1.3)}

	*clock = time.Date(2026, 11, 11, 3, 0, 0, 0, Beijing) // 凌晨 3 点：不发
	if a, _ := svc.Evaluate(site, f); a != nil {
		t.Fatalf("days alert at night: %+v", a)
	}
	*clock = time.Date(2026, 11, 11, 8, 10, 0, 0, Beijing)
	a, commit := svc.Evaluate(site, f)
	if a == nil || !strings.Contains(a.Text, "考虑充值") || !strings.Contains(a.Subject, "叮当猫") {
		t.Fatalf("days alert = %+v", a)
	}
	commit()
	*clock = clock.AddDate(0, 0, 1)
	if a, _ := svc.Evaluate(site, f); a != nil {
		t.Fatalf("days alert repeats daily; daily report should cover it")
	}
}

func TestDailyReportDueAndText(t *testing.T) {
	svc, _, clock := newService(t)
	*clock = time.Date(2026, 11, 11, 9, 50, 0, 0, Beijing)
	if svc.ReportDue() {
		t.Fatal("before 10:00")
	}
	*clock = time.Date(2026, 11, 11, 10, 5, 0, 0, Beijing)
	if !svc.ReportDue() {
		t.Fatal("after 10:00")
	}
	svc.MarkReported()
	if svc.ReportDue() {
		t.Fatal("already sent today")
	}

	usage := svc.usage
	for d := 1; d <= 7; d++ {
		_ = usage.UpsertDaily("s1", []model.DailyCost{{Date: clock.AddDate(0, 0, -d).Format("2006-01-02"), Cost: 38}})
	}
	text := svc.DailyReport([]model.Site{{ID: "s1", Name: "叮当猫", Balance: 50, Status: "ok"}})
	for _, want := range []string{"每日额度日报 · 11-11", "🟡 叮当猫  $50.00", "昨天 $38.00", "日均 $38.00", "建议充值：叮当猫"} {
		if !strings.Contains(text, want) {
			t.Errorf("report missing %q:\n%s", want, text)
		}
	}
}
