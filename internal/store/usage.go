package store

import (
	"database/sql"

	"upstream-balance/internal/model"
)

// Snapshot 是某一刻查到的余额；TotalCost 是上游累计实际扣费，上游不给时为 nil。
type Snapshot struct {
	At        int64
	Balance   float64
	TotalCost *float64
}

// AlertState 记录某站点某类预测提醒上次发到了哪一级。
type AlertState struct {
	Level  int
	SentAt int64
}

type UsageStore struct {
	db *DB
}

func NewUsageStore(db *DB) *UsageStore {
	return &UsageStore{db: db}
}

func (s *UsageStore) AddSnapshot(siteID string, at int64, balance float64, totalCost *float64) error {
	var tc interface{}
	if totalCost != nil {
		tc = *totalCost
	}
	_, err := s.db.Exec(`INSERT INTO balance_snapshots (site_id, at, balance, total_cost) VALUES (?, ?, ?, ?)`,
		siteID, at, balance, tc)
	return err
}

// Snapshots 返回 since 之后的快照，按时间由早到晚。
func (s *UsageStore) Snapshots(siteID string, since int64) ([]Snapshot, error) {
	rows, err := s.db.Query(`SELECT at, balance, total_cost FROM balance_snapshots
		WHERE site_id = ? AND at >= ? ORDER BY at`, siteID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Snapshot
	for rows.Next() {
		var snap Snapshot
		var tc sql.NullFloat64
		if err := rows.Scan(&snap.At, &snap.Balance, &tc); err != nil {
			return nil, err
		}
		if tc.Valid {
			v := tc.Float64
			snap.TotalCost = &v
		}
		out = append(out, snap)
	}
	return out, rows.Err()
}

func (s *UsageStore) PruneSnapshots(before int64) error {
	_, err := s.db.Exec(`DELETE FROM balance_snapshots WHERE at < ?`, before)
	return err
}

func (s *UsageStore) UpsertDaily(siteID string, days []model.DailyCost) error {
	if len(days) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(`INSERT INTO daily_usage (site_id, date, cost) VALUES (?, ?, ?)
		ON CONFLICT(site_id, date) DO UPDATE SET cost = excluded.cost`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, d := range days {
		if _, err := stmt.Exec(siteID, d.Date, d.Cost); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Daily 返回 fromDate（含）之后上游自报的每日消耗，按日期升序。
func (s *UsageStore) Daily(siteID, fromDate string) ([]model.DailyCost, error) {
	rows, err := s.db.Query(`SELECT date, cost FROM daily_usage WHERE site_id = ? AND date >= ? ORDER BY date`,
		siteID, fromDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.DailyCost
	for rows.Next() {
		var d model.DailyCost
		if err := rows.Scan(&d.Date, &d.Cost); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// HasDaily 表示这个站点的上游曾经报过每日消耗（报过就以上游口径为准）。
func (s *UsageStore) HasDaily(siteID string) bool {
	var n int
	s.db.QueryRow(`SELECT count(*) FROM daily_usage WHERE site_id = ?`, siteID).Scan(&n)
	return n > 0
}

func (s *UsageStore) AlertState(siteID, kind string) (AlertState, bool) {
	var st AlertState
	err := s.db.QueryRow(`SELECT level, sent_at FROM forecast_alerts WHERE site_id = ? AND kind = ?`,
		siteID, kind).Scan(&st.Level, &st.SentAt)
	return st, err == nil
}

func (s *UsageStore) SetAlertState(siteID, kind string, level int, sentAt int64) error {
	_, err := s.db.Exec(`INSERT INTO forecast_alerts (site_id, kind, level, sent_at) VALUES (?, ?, ?, ?)
		ON CONFLICT(site_id, kind) DO UPDATE SET level = excluded.level, sent_at = excluded.sent_at`,
		siteID, kind, level, sentAt)
	return err
}

func (s *UsageStore) ClearAlertState(siteID, kind string) error {
	_, err := s.db.Exec(`DELETE FROM forecast_alerts WHERE site_id = ? AND kind = ?`, siteID, kind)
	return err
}
