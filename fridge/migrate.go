package fridge

import (
	"database/sql"
	"fmt"
)

type migration struct {
	ID   int
	Name string
	Up   func(*sql.DB) error
}

func allMigrations() []migration {
	return []migration{
		{1, "initial_fridge_schema", migrate001InitialFridgeSchema},
		{2, "users_sessions_invites", migrate002UsersSessionsInvites},
		{3, "user_admin_and_invite_meta", migrate003UserAdminAndInviteMeta},
		{4, "login_attempts", migrate004LoginAttempts},
	}
}

// Migrate applies numbered schema migrations. A database created by the
// pre-migration InitDB (fridge table, no schema_migrations) is stamped as
// already having migration 1 so existing data is kept.
func Migrate(db *sql.DB) error {
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			id INTEGER PRIMARY KEY,
			name TEXT NOT NULL,
			applied_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	applied, err := appliedMigrationIDs(db)
	if err != nil {
		return err
	}

	if len(applied) == 0 {
		exists, err := tableExists(db, "fridge")
		if err != nil {
			return err
		}
		if exists {
			if err := recordMigration(db, 1, "initial_fridge_schema"); err != nil {
				return err
			}
			applied[1] = true
		}
	}

	for _, m := range allMigrations() {
		if applied[m.ID] {
			continue
		}
		if err := m.Up(db); err != nil {
			return fmt.Errorf("migration %d (%s): %w", m.ID, m.Name, err)
		}
		if err := recordMigration(db, m.ID, m.Name); err != nil {
			return err
		}
	}
	return nil
}

func appliedMigrationIDs(db *sql.DB) (map[int]bool, error) {
	rows, err := db.Query(`SELECT id FROM schema_migrations`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	applied := map[int]bool{}
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		applied[id] = true
	}
	return applied, rows.Err()
}

func recordMigration(db *sql.DB, id int, name string) error {
	_, err := db.Exec(`INSERT INTO schema_migrations (id, name) VALUES (?, ?)`, id, name)
	return err
}

func tableExists(db *sql.DB, name string) (bool, error) {
	var n int
	err := db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`,
		name,
	).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func migrate001InitialFridgeSchema(db *sql.DB) error {
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS fridge (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			quantity NUMBER NOT NULL,
			expiration_date DATETIME,
			last_changed DATETIME DEFAULT CURRENT_TIMESTAMP
		)`); err != nil {
		return err
	}
	_, err := db.Exec(`
		CREATE TRIGGER IF NOT EXISTS update_last_changed
		AFTER UPDATE ON fridge
		FOR EACH ROW
		BEGIN
			UPDATE fridge SET last_changed = CURRENT_TIMESTAMP WHERE id = OLD.id;
		END`)
	return err
}

func migrate002UsersSessionsInvites(db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			email TEXT NOT NULL COLLATE NOCASE UNIQUE,
			password_hash TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
		)`,
		`CREATE TABLE sessions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			token_hash TEXT NOT NULL UNIQUE,
			expires_at TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
		)`,
		`CREATE TABLE invites (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			token_hash TEXT NOT NULL UNIQUE,
			expires_at TEXT NOT NULL,
			used_at TEXT,
			created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
		)`,
		`CREATE INDEX idx_sessions_expires_at ON sessions (expires_at)`,
		`CREATE INDEX idx_invites_expires_at ON invites (expires_at)`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

func migrate003UserAdminAndInviteMeta(db *sql.DB) error {
	stmts := []string{
		`ALTER TABLE users ADD COLUMN is_admin INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE invites ADD COLUMN created_by INTEGER REFERENCES users(id)`,
		`ALTER TABLE invites ADD COLUMN grants_admin INTEGER NOT NULL DEFAULT 0`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

func migrate004LoginAttempts(db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE login_attempts (
			ip TEXT NOT NULL,
			at TEXT NOT NULL
		)`,
		`CREATE INDEX idx_login_attempts_ip_at ON login_attempts (ip, at)`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}
