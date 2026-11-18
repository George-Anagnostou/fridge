package fridge

import (
	"database/sql"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

const DefaultDBPath = "./fridge.db"

// DBPath returns the SQLite path from FRIDGE_DB, or DefaultDBPath if unset.
func DBPath() string {
	if p := os.Getenv("FRIDGE_DB"); p != "" {
		return p
	}
	return DefaultDBPath
}

func sqliteDSN(path string) string {
	if strings.HasPrefix(path, "file:") {
		u, err := url.Parse(path)
		if err == nil {
			q := u.Query()
			q.Add("_pragma", "foreign_keys(1)")
			q.Add("_pragma", "busy_timeout(5000)")
			u.RawQuery = q.Encode()
			return u.String()
		}
	}

	abs := path
	if resolved, err := filepath.Abs(path); err == nil {
		abs = resolved
	}
	u := url.URL{
		Scheme: "file",
		Path:   abs,
	}
	q := u.Query()
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "busy_timeout(5000)")
	u.RawQuery = q.Encode()
	return u.String()
}

func InitDB(path string) (*sql.DB, error) {
	if path == "" {
		path = DefaultDBPath
	}

	db, err := sql.Open("sqlite", sqliteDSN(path))
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
