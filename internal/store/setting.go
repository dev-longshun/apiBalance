package store

import "database/sql"

type SettingStore struct {
	db *DB
}

func NewSettingStore(db *DB) *SettingStore {
	return &SettingStore{db: db}
}

var defaults = map[string]string{
	"interval_minutes":   "30",
	"telegram_bot_token": "",
	"telegram_chat_id":   "",
	"admin_username":     "",
	"admin_password":     "",
}

func (s *SettingStore) Get(key string) (string, error) {
	var value string
	err := s.db.QueryRow("SELECT value FROM settings WHERE key = ?", key).Scan(&value)
	if err == sql.ErrNoRows {
		if def, ok := defaults[key]; ok {
			return def, nil
		}
		return "", nil
	}
	return value, err
}

func (s *SettingStore) Set(key, value string) error {
	_, err := s.db.Exec(
		`INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, value,
	)
	return err
}

func (s *SettingStore) GetAll() (map[string]string, error) {
	result := make(map[string]string)
	for k, v := range defaults {
		result[k] = v
	}

	rows, err := s.db.Query("SELECT key, value FROM settings")
	if err != nil {
		return result, err
	}
	defer rows.Close()

	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return result, err
		}
		result[k] = v
	}
	return result, rows.Err()
}
