package store

import "upstream-balance/internal/model"

type ThresholdStore struct {
	db *DB
}

func NewThresholdStore(db *DB) *ThresholdStore {
	return &ThresholdStore{db: db}
}

func (s *ThresholdStore) ListBySite(siteID string) ([]model.Threshold, error) {
	rows, err := s.db.Query(
		`SELECT id, site_id, amount, triggered FROM thresholds WHERE site_id = ? ORDER BY amount DESC`, siteID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var thresholds []model.Threshold
	for rows.Next() {
		var t model.Threshold
		var triggered int
		if err := rows.Scan(&t.ID, &t.SiteID, &t.Amount, &triggered); err != nil {
			return nil, err
		}
		t.Triggered = triggered != 0
		thresholds = append(thresholds, t)
	}
	return thresholds, rows.Err()
}

func (s *ThresholdStore) ReplaceBySite(siteID string, amounts []float64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec("DELETE FROM thresholds WHERE site_id = ?", siteID); err != nil {
		return err
	}

	stmt, err := tx.Prepare("INSERT INTO thresholds (site_id, amount, triggered) VALUES (?, ?, 0)")
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, amount := range amounts {
		if _, err := stmt.Exec(siteID, amount); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (s *ThresholdStore) SetTriggered(id int64, triggered bool) error {
	v := 0
	if triggered {
		v = 1
	}
	_, err := s.db.Exec("UPDATE thresholds SET triggered = ? WHERE id = ?", v, id)
	return err
}

func (s *ThresholdStore) GetAmountsBySite(siteID string) ([]float64, error) {
	rows, err := s.db.Query("SELECT amount FROM thresholds WHERE site_id = ? ORDER BY amount DESC", siteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var amounts []float64
	for rows.Next() {
		var a float64
		if err := rows.Scan(&a); err != nil {
			return nil, err
		}
		amounts = append(amounts, a)
	}
	return amounts, rows.Err()
}
