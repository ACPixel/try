package main

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type sqlDB = sql.DB

func initDB(dbPath string) (*sqlDB, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, err
	}
	if err := migrateDB(db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func migrateDB(db *sqlDB) error {
	version, err := schemaVersion(db)
	if err != nil {
		return err
	}
	if version > currentSchemaVersion {
		return fmt.Errorf("database schema version %d is newer than this try version supports (%d)", version, currentSchemaVersion)
	}
	if version < 1 {
		return migrateToV1(db)
	}
	return nil
}

func schemaVersion(db *sqlDB) (int, error) {
	exists, err := tableExists(db, "schema_migrations")
	if err != nil || !exists {
		return 0, err
	}

	var version int
	err = db.QueryRow(`SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1`).Scan(&version)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return version, err
}

func tableExists(db *sqlDB, table string) (bool, error) {
	var name string
	err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&name)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return err == nil, err
}

func migrateToV1(db *sqlDB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}

	statements := []string{
		`CREATE TABLE IF NOT EXISTS folders (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			path TEXT NOT NULL UNIQUE,
			name TEXT NOT NULL,
			date TEXT NOT NULL,
			created_at TEXT NOT NULL,
			times_opened INTEGER DEFAULT 1,
			last_opened TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			applied_at TEXT NOT NULL
		);`,
	}
	for _, statement := range statements {
		if _, err := tx.Exec(statement); err != nil {
			_ = tx.Rollback()
			return err
		}
	}

	if _, err := tx.Exec(`INSERT OR REPLACE INTO schema_migrations (version, applied_at) VALUES (?, ?)`, currentSchemaVersion, time.Now().Format(time.RFC3339)); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}
