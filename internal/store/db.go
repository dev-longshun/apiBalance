package store

import (
	"database/sql"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

type DB struct {
	*sql.DB
}

func Open(path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}

	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		db.Close()
		return nil, err
	}

	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}

	return &DB{db}, nil
}

func migrate(db *sql.DB) error {
	ddl := `
CREATE TABLE IF NOT EXISTS sites (
    id            TEXT PRIMARY KEY,
    name          TEXT NOT NULL UNIQUE,
    base_url      TEXT NOT NULL,
    api_key       TEXT NOT NULL DEFAULT '',
    username      TEXT NOT NULL DEFAULT '',
    password      TEXT NOT NULL DEFAULT '',
    user_id       INTEGER NOT NULL DEFAULT 0,
    auth_type     TEXT NOT NULL DEFAULT 'bearer',
    balance       REAL NOT NULL DEFAULT 0,
    balance_unit  TEXT NOT NULL DEFAULT '',
    detected_type TEXT NOT NULL DEFAULT '',
    last_check_at TEXT NOT NULL DEFAULT '',
    last_error    TEXT NOT NULL DEFAULT '',
    status        TEXT NOT NULL DEFAULT 'unknown',
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS thresholds (
    id        INTEGER PRIMARY KEY AUTOINCREMENT,
    site_id   TEXT NOT NULL,
    amount    REAL NOT NULL,
    triggered INTEGER NOT NULL DEFAULT 0,
    FOREIGN KEY (site_id) REFERENCES sites(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
`
	if _, err := db.Exec(ddl); err != nil {
		return err
	}

	// Add columns for existing databases that lack them.
	for _, col := range []struct{ name, typ, dflt string }{
		{"username", "TEXT", "''"},
		{"password", "TEXT", "''"},
		{"user_id", "INTEGER", "0"},
	} {
		if !columnExists(db, "sites", col.name) {
			_, err := db.Exec("ALTER TABLE sites ADD COLUMN " + col.name + " " + col.typ + " NOT NULL DEFAULT " + col.dflt)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func columnExists(db *sql.DB, table, column string) bool {
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return false
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var dfltValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dfltValue, &pk); err != nil {
			return false
		}
		if name == column {
			return true
		}
	}
	return false
}
