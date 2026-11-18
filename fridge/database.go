package fridge

import (
	"database/sql"
	"fmt"
	"os"
	"strings"

	_ "github.com/mattn/go-sqlite3"
)

const DefaultDBPath = "./fridge.db"

const busyTimeoutMS = 5000

// DBPath returns the SQLite path from FRIDGE_DB, or DefaultDBPath if unset.
func DBPath() string {
	if p := os.Getenv("FRIDGE_DB"); p != "" {
		return p
	}
	return DefaultDBPath
}

func sqliteDSN(path string) string {
	const timeout = "_busy_timeout="

	if strings.HasPrefix(path, "file:") {
		if strings.Contains(path, "?") {
			return path + "&" + timeout + fmt.Sprint(busyTimeoutMS)
		}
		return path + "?" + timeout + fmt.Sprint(busyTimeoutMS)
	}
	if strings.Contains(path, "?") {
		return "file:" + path + "&" + timeout + fmt.Sprint(busyTimeoutMS)
	}
	return "file:" + path + "?" + timeout + fmt.Sprint(busyTimeoutMS)
}

func InitDB(path string) (*sql.DB, error) {
	if path == "" {
		path = DefaultDBPath
	}

	db, err := sql.Open("sqlite3", sqliteDSN(path))
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		db.Close()
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
		db.Close()
		return nil, err
	}

	createTriggerSQL := `
    CREATE TRIGGER IF NOT EXISTS update_last_changed
    AFTER UPDATE ON fridge
    FOR EACH ROW
    BEGIN
        UPDATE fridge SET last_changed = CURRENT_TIMESTAMP WHERE id = OLD.id;
    END;
    `

	_, err = db.Exec(createTriggerSQL)
	if err != nil {
		db.Close()
		return nil, err
	}

	return db, nil
}
