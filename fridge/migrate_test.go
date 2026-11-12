package fridge

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func TestMigrateFreshDB(t *testing.T) {
	db := openTempDB(t)

	for _, table := range []string{"fridge", "schema_migrations", "users", "sessions", "invites"} {
		ok, err := tableExists(db, table)
		if err != nil {
			t.Fatalf("tableExists(%q): %v", table, err)
		}
		if !ok {
			t.Errorf("missing table %q after migrate", table)
		}
	}

	ids, err := appliedMigrationIDs(db)
	if err != nil {
		t.Fatalf("appliedMigrationIDs: %v", err)
	}
	if !ids[1] || !ids[2] {
		t.Fatalf("applied migrations = %v, want 1 and 2", ids)
	}

	// Re-running InitDB/Migrate is a no-op and keeps data.
	f := &Fridge{}
	id, err := f.AddItem(db, "Milk", 1, date(t, "2026-12-01"))
	if err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
	got, err := f.GetItemByID(db, id)
	if err != nil {
		t.Fatalf("GetItemByID after remigrate: %v", err)
	}
	if got.Name != "Milk" {
		t.Errorf("Name = %q, want Milk", got.Name)
	}
}

func TestMigrateLegacyDB(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite3", sqliteDSN(path))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Fatalf("ping: %v", err)
	}

	// Recreate the original InitDB schema with no schema_migrations table.
	if _, err := db.Exec(`
		CREATE TABLE fridge (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			quantity NUMBER NOT NULL,
			expiration_date DATETIME,
			last_changed DATETIME DEFAULT CURRENT_TIMESTAMP
		)`); err != nil {
		t.Fatalf("create legacy fridge: %v", err)
	}
	if _, err := db.Exec(`
		CREATE TRIGGER update_last_changed
		AFTER UPDATE ON fridge
		FOR EACH ROW
		BEGIN
			UPDATE fridge SET last_changed = CURRENT_TIMESTAMP WHERE id = OLD.id;
		END`); err != nil {
		t.Fatalf("create legacy trigger: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO fridge (name, quantity, expiration_date) VALUES (?, ?, ?)`,
		"Eggs", 12, "2026-11-20",
	); err != nil {
		t.Fatalf("insert legacy row: %v", err)
	}

	db.Close()

	migrated, err := InitDB(path)
	if err != nil {
		t.Fatalf("InitDB on legacy file: %v", err)
	}
	t.Cleanup(func() { migrated.Close() })

	var name string
	var qty float64
	if err := migrated.QueryRow(`SELECT name, quantity FROM fridge WHERE name = ?`, "Eggs").
		Scan(&name, &qty); err != nil {
		t.Fatalf("legacy row missing after migrate: %v", err)
	}
	if name != "Eggs" || qty != 12 {
		t.Fatalf("legacy row = %s %v, want Eggs 12", name, qty)
	}

	for _, table := range []string{"users", "sessions", "invites", "schema_migrations"} {
		ok, err := tableExists(migrated, table)
		if err != nil {
			t.Fatalf("tableExists(%q): %v", table, err)
		}
		if !ok {
			t.Errorf("missing table %q after legacy migrate", table)
		}
	}

	ids, err := appliedMigrationIDs(migrated)
	if err != nil {
		t.Fatalf("appliedMigrationIDs: %v", err)
	}
	if !ids[1] || !ids[2] {
		t.Fatalf("applied migrations = %v, want 1 (stamped) and 2", ids)
	}
}
