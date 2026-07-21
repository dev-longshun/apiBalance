package store

import (
	"database/sql"
	"fmt"

	"upstream-balance/internal/model"
)

type SiteStore struct {
	db *DB
}

func NewSiteStore(db *DB) *SiteStore {
	return &SiteStore{db: db}
}

func (s *SiteStore) Create(site *model.Site) error {
	_, err := s.db.Exec(
		`INSERT INTO sites (id, name, base_url, portal_url, api_key, username, password, user_id, auth_type, balance, balance_unit, detected_type, last_check_at, last_error, status, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		site.ID, site.Name, site.BaseURL, site.PortalURL, site.APIKey,
		site.Username, site.Password, site.UserID,
		site.AuthType,
		site.Balance, site.BalanceUnit, site.DetectedType,
		site.LastCheckAt, site.LastError, site.Status,
		site.CreatedAt, site.UpdatedAt,
	)
	return err
}

func (s *SiteStore) List() ([]model.Site, error) {
	rows, err := s.db.Query(
		`SELECT id, name, base_url, portal_url, api_key, username, password, user_id, auth_type, balance, balance_unit, detected_type, last_check_at, last_error, status, created_at, updated_at FROM sites ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sites []model.Site
	for rows.Next() {
		var site model.Site
		if err := rows.Scan(&site.ID, &site.Name, &site.BaseURL, &site.PortalURL, &site.APIKey,
			&site.Username, &site.Password, &site.UserID,
			&site.AuthType,
			&site.Balance, &site.BalanceUnit, &site.DetectedType,
			&site.LastCheckAt, &site.LastError, &site.Status,
			&site.CreatedAt, &site.UpdatedAt); err != nil {
			return nil, err
		}
		sites = append(sites, site)
	}
	return sites, rows.Err()
}

func (s *SiteStore) Get(id string) (*model.Site, error) {
	var site model.Site
	err := s.db.QueryRow(
		`SELECT id, name, base_url, portal_url, api_key, username, password, user_id, auth_type, balance, balance_unit, detected_type, last_check_at, last_error, status, created_at, updated_at FROM sites WHERE id = ?`, id,
	).Scan(&site.ID, &site.Name, &site.BaseURL, &site.PortalURL, &site.APIKey,
		&site.Username, &site.Password, &site.UserID,
		&site.AuthType,
		&site.Balance, &site.BalanceUnit, &site.DetectedType,
		&site.LastCheckAt, &site.LastError, &site.Status,
		&site.CreatedAt, &site.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &site, nil
}

func (s *SiteStore) Update(id string, updates map[string]interface{}) error {
	if len(updates) == 0 {
		return nil
	}

	query := "UPDATE sites SET "
	args := make([]interface{}, 0, len(updates)+1)
	first := true
	for col, val := range updates {
		if !first {
			query += ", "
		}
		query += fmt.Sprintf("%s = ?", col)
		args = append(args, val)
		first = false
	}
	query += " WHERE id = ?"
	args = append(args, id)

	result, err := s.db.Exec(query, args...)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *SiteStore) Delete(id string) error {
	result, err := s.db.Exec("DELETE FROM sites WHERE id = ?", id)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *SiteStore) UpdateBalance(id string, balance float64, unit, detectedType, status, lastError string) error {
	now := model.Now()
	_, err := s.db.Exec(
		`UPDATE sites SET balance = ?, balance_unit = ?, detected_type = ?, status = ?, last_error = ?, last_check_at = ?, updated_at = ? WHERE id = ?`,
		balance, unit, detectedType, status, lastError, now, now, id,
	)
	return err
}
