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
	opts := fmt.Sprintf("_busy_timeout=%d&_foreign_keys=on", busyTimeoutMS)

	if strings.HasPrefix(path, "file:") {
		if strings.Contains(path, "?") {
			return path + "&" + opts
		}
		return path + "?" + opts
	}
	if strings.Contains(path, "?") {
		return "file:" + path + "&" + opts
	}
	return "file:" + path + "?" + opts
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

	if err := Migrate(db); err != nil {
		db.Close()
		return nil, err
	}

	return db, nil
}
