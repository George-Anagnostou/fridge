package fridge

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestMigrateFreshDB(t *testing.T) {
	db := openTempDB(t)

	for _, table := range []string{"fridge", "schema_migrations", "users", "sessions", "invites", "login_attempts", "stores", "store_members", "lookup_cache"} {
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
	if !ids[1] || !ids[2] || !ids[3] || !ids[4] || !ids[5] || !ids[6] || !ids[7] {
		t.Fatalf("applied migrations = %v, want 1-7", ids)
	}

	// Re-running InitDB/Migrate is a no-op and keeps data.
	_, storeID := seedUserAndStore(t, db)
	f := &Fridge{}
	id, err := f.AddItem(db, storeID, "Milk", 1, date(t, "2026-12-01"))
	if err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
	got, err := f.GetItemByID(db, storeID, id)
	if err != nil {
		t.Fatalf("GetItemByID after remigrate: %v", err)
	}
	if got.Name != "Milk" {
		t.Errorf("Name = %q, want Milk", got.Name)
	}
}

func TestMigrateLegacyDB(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", sqliteDSN(path))
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
	if !ids[1] || !ids[2] || !ids[3] || !ids[4] || !ids[5] || !ids[6] || !ids[7] {
		t.Fatalf("applied migrations = %v, want 1 (stamped) through 7", ids)
	}
}

func TestMigrateAddsAdminColumns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pre-admin.db")
	db, err := sql.Open("sqlite", sqliteDSN(path))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Fatalf("ping: %v", err)
	}
	if err := migrate001InitialFridgeSchema(db); err != nil {
		t.Fatalf("migrate 1: %v", err)
	}
	if err := migrate002UsersSessionsInvites(db); err != nil {
		t.Fatalf("migrate 2: %v", err)
	}
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			id INTEGER PRIMARY KEY,
			name TEXT NOT NULL,
			applied_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`); err != nil {
		t.Fatalf("schema_migrations: %v", err)
	}
	if err := recordMigration(db, 1, "initial_fridge_schema"); err != nil {
		t.Fatalf("stamp 1: %v", err)
	}
	if err := recordMigration(db, 2, "users_sessions_invites"); err != nil {
		t.Fatalf("stamp 2: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO users (email, password_hash) VALUES (?, ?)`, "old@example.com", "x"); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	db.Close()

	migrated, err := InitDB(path)
	if err != nil {
		t.Fatalf("InitDB on pre-admin file: %v", err)
	}
	t.Cleanup(func() { migrated.Close() })

	var isAdmin int
	if err := migrated.QueryRow(`SELECT is_admin FROM users WHERE email = ?`, "old@example.com").Scan(&isAdmin); err != nil {
		t.Fatalf("is_admin column missing or row lost: %v", err)
	}
	if isAdmin != 0 {
		t.Fatalf("is_admin = %d, want 0 default", isAdmin)
	}

	var grants, createdBy sql.NullInt64
	// New invite columns must exist and accept defaults/NULL.
	if _, err := migrated.Exec(
		`INSERT INTO invites (token_hash, expires_at) VALUES (?, ?)`,
		"testhash", "2026-12-01T00:00:00Z",
	); err != nil {
		t.Fatalf("insert invite after migrate 3: %v", err)
	}
	if err := migrated.QueryRow(`SELECT grants_admin, created_by FROM invites WHERE token_hash = ?`, "testhash").
		Scan(&grants, &createdBy); err != nil {
		t.Fatalf("invite admin columns missing: %v", err)
	}
	if grants.Int64 != 0 || createdBy.Valid {
		t.Fatalf("grants_admin=%v created_by=%v, want 0 and NULL", grants, createdBy)
	}

	ids, err := appliedMigrationIDs(migrated)
	if err != nil {
		t.Fatalf("appliedMigrationIDs: %v", err)
	}
	if !ids[3] {
		t.Fatalf("applied migrations = %v, want 3", ids)
	}
}

func TestMigrateExistingItemsToDefaultStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pre-stores.db")
	db, err := sql.Open("sqlite", sqliteDSN(path))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Fatalf("ping: %v", err)
	}
	if err := migrate001InitialFridgeSchema(db); err != nil {
		t.Fatalf("m1: %v", err)
	}
	if err := migrate002UsersSessionsInvites(db); err != nil {
		t.Fatalf("m2: %v", err)
	}
	if err := migrate003UserAdminAndInviteMeta(db); err != nil {
		t.Fatalf("m3: %v", err)
	}
	if err := migrate004LoginAttempts(db); err != nil {
		t.Fatalf("m4: %v", err)
	}
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			id INTEGER PRIMARY KEY,
			name TEXT NOT NULL,
			applied_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`); err != nil {
		t.Fatalf("schema_migrations: %v", err)
	}
	for id, name := range map[int]string{1: "initial_fridge_schema", 2: "users_sessions_invites", 3: "user_admin_and_invite_meta", 4: "login_attempts"} {
		if err := recordMigration(db, id, name); err != nil {
			t.Fatalf("stamp %d: %v", id, err)
		}
	}
	if _, err := db.Exec(`INSERT INTO users (email, password_hash, is_admin) VALUES (?, ?, 0)`, "user@example.com", "x"); err != nil {
		t.Fatalf("user: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO users (email, password_hash, is_admin) VALUES (?, ?, 1)`, "admin@example.com", "x"); err != nil {
		t.Fatalf("admin: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO fridge (name, quantity, expiration_date) VALUES (?, ?, ?)`, "Milk", 1, "2026-12-01"); err != nil {
		t.Fatalf("item: %v", err)
	}
	db.Close()

	migrated, err := InitDB(path)
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { migrated.Close() })

	var storeID, ownerID int64
	var storeName string
	if err := migrated.QueryRow(`SELECT id, name, owner_id FROM stores`).Scan(&storeID, &storeName, &ownerID); err != nil {
		t.Fatalf("default store: %v", err)
	}
	if storeName != DefaultStoreName {
		t.Fatalf("store name = %q, want %q", storeName, DefaultStoreName)
	}
	var adminID int64
	if err := migrated.QueryRow(`SELECT id FROM users WHERE email = ?`, "admin@example.com").Scan(&adminID); err != nil {
		t.Fatalf("admin id: %v", err)
	}
	if ownerID != adminID {
		t.Fatalf("default store owner = %d, want first admin %d", ownerID, adminID)
	}
	var itemStore int64
	if err := migrated.QueryRow(`SELECT store_id FROM fridge WHERE name = ?`, "Milk").Scan(&itemStore); err != nil {
		t.Fatalf("item store_id: %v", err)
	}
	if itemStore != storeID {
		t.Fatalf("item store_id = %d, want %d", itemStore, storeID)
	}
}

func TestOrphanItemsClaimedOnFirstUser(t *testing.T) {
	path := filepath.Join(t.TempDir(), "orphans.db")
	db, err := sql.Open("sqlite", sqliteDSN(path))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Fatalf("ping: %v", err)
	}
	if _, err := db.Exec(`
		CREATE TABLE fridge (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			quantity NUMBER NOT NULL,
			expiration_date DATETIME,
			last_changed DATETIME DEFAULT CURRENT_TIMESTAMP
		)`); err != nil {
		t.Fatalf("legacy fridge: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO fridge (name, quantity, expiration_date) VALUES (?, ?, ?)`, "Eggs", 12, "2026-11-20"); err != nil {
		t.Fatalf("item: %v", err)
	}
	db.Close()

	migrated, err := InitDB(path)
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { migrated.Close() })

	var nStores, nNull int
	if err := migrated.QueryRow(`SELECT COUNT(*) FROM stores`).Scan(&nStores); err != nil {
		t.Fatalf("count stores: %v", err)
	}
	if nStores != 0 {
		t.Fatalf("stores = %d, want 0 when no users exist", nStores)
	}
	if err := migrated.QueryRow(`SELECT COUNT(*) FROM fridge WHERE store_id IS NULL`).Scan(&nNull); err != nil {
		t.Fatalf("count orphans: %v", err)
	}
	if nNull != 1 {
		t.Fatalf("orphan items = %d, want 1", nNull)
	}

	res, err := migrated.Exec(`INSERT INTO users (email, password_hash) VALUES (?, ?)`, "first@example.com", "x")
	if err != nil {
		t.Fatalf("first user: %v", err)
	}
	userID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("user id: %v", err)
	}
	if err := ClaimOrphanItems(migrated, userID); err != nil {
		t.Fatalf("ClaimOrphanItems: %v", err)
	}
	var ownerID, storeID int64
	if err := migrated.QueryRow(`SELECT id, owner_id FROM stores`).Scan(&storeID, &ownerID); err != nil {
		t.Fatalf("claimed store: %v", err)
	}
	if ownerID != userID {
		t.Fatalf("owner = %d, want %d", ownerID, userID)
	}
	var itemStore int64
	if err := migrated.QueryRow(`SELECT store_id FROM fridge WHERE name = ?`, "Eggs").Scan(&itemStore); err != nil {
		t.Fatalf("item: %v", err)
	}
	if itemStore != storeID {
		t.Fatalf("item store_id = %d, want %d", itemStore, storeID)
	}
}

func TestMigrateAddsItemDetails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pre-details.db")
	db, err := sql.Open("sqlite", sqliteDSN(path))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	for _, up := range []func(*sql.DB) error{
		migrate001InitialFridgeSchema,
		migrate002UsersSessionsInvites,
		migrate003UserAdminAndInviteMeta,
		migrate004LoginAttempts,
		migrate005StoresAndItemScope,
	} {
		if err := up(db); err != nil {
			t.Fatalf("migrate: %v", err)
		}
	}
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			id INTEGER PRIMARY KEY,
			name TEXT NOT NULL,
			applied_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`); err != nil {
		t.Fatalf("schema_migrations: %v", err)
	}
	for id, name := range map[int]string{
		1: "initial_fridge_schema",
		2: "users_sessions_invites",
		3: "user_admin_and_invite_meta",
		4: "login_attempts",
		5: "stores_and_item_scope",
	} {
		if err := recordMigration(db, id, name); err != nil {
			t.Fatalf("stamp %d: %v", id, err)
		}
	}
	res, err := db.Exec(`INSERT INTO users (email, password_hash) VALUES (?, ?)`, "owner@example.com", "x")
	if err != nil {
		t.Fatalf("user: %v", err)
	}
	ownerID, _ := res.LastInsertId()
	st, err := db.Exec(`INSERT INTO stores (name, owner_id) VALUES (?, ?)`, "Test", ownerID)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	storeID, _ := st.LastInsertId()
	if _, err := db.Exec(
		`INSERT INTO fridge (store_id, name, quantity, expiration_date) VALUES (?, ?, ?, ?)`,
		storeID, "Milk", 1, "2026-12-01",
	); err != nil {
		t.Fatalf("item: %v", err)
	}
	db.Close()

	migrated, err := InitDB(path)
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { migrated.Close() })

	var barcode, brand, serving sql.NullString
	var calories, protein, carbs, fat sql.NullFloat64
	if err := migrated.QueryRow(
		`SELECT barcode, brand, serving_size, calories, protein_g, carbs_g, fat_g FROM fridge WHERE name = ?`,
		"Milk",
	).Scan(&barcode, &brand, &serving, &calories, &protein, &carbs, &fat); err != nil {
		t.Fatalf("new columns missing or row lost: %v", err)
	}
	if barcode.Valid || brand.Valid || serving.Valid || calories.Valid || protein.Valid || carbs.Valid || fat.Valid {
		t.Fatalf("legacy item got unexpected values after migrate 6")
	}
	ids, err := appliedMigrationIDs(migrated)
	if err != nil {
		t.Fatalf("appliedMigrationIDs: %v", err)
	}
	if !ids[6] {
		t.Fatalf("applied migrations = %v, want 6", ids)
	}
}
