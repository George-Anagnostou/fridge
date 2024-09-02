package fridge

import (
	"database/sql"

	_ "github.com/mattn/go-sqlite3"
)

var db *sql.DB

func InitDB() (*sql.DB, error) {
	db, err := sql.Open("sqlite3", "./fridge.db")
	if err != nil {
		return nil, err
	}

	_, err = db.Exec(`
        CREATE TABLE IF NOT EXISTS fridge (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            name TEXT NOT NULL,
            quantity NUMBER NOT NULL,
            expiration_date DATETIME,
            last_changed DATETIME DEFAULT CURRENT_TIMESTAMP
    );`)
	if err != nil {
		return nil, err
	}

	createTrigerSQL := `
    CREATE TRIGGER IF NOT EXISTS update_last_changed
    AFTER UPDATE ON fridge
    FOR EACH ROW
    BEGIN
        UPDATE fridge SET last_changed = CURRENT_TIMESTAMP WHERE id = OLD.id;
    END;
    `

	_, err = db.Exec(createTrigerSQL)
	if err != nil {
		return nil, err
	}

	return db, nil
}
