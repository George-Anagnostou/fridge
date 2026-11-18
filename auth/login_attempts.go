package auth

import (
	"database/sql"
	"time"
)

const (
	LoginAttemptWindow = 10 * time.Minute
	LoginAttemptLimit  = 10
)

// AllowLoginAttempt records a login try for ip and reports whether it is
// within the rate limit. Rows older than the window are deleted on each call.
func AllowLoginAttempt(db *sql.DB, ip string, now time.Time) (bool, error) {
	if ip == "" {
		ip = "unknown"
	}
	if now.IsZero() {
		now = time.Now()
	}
	now = now.UTC()
	cutoff := now.Add(-LoginAttemptWindow).Format(time.RFC3339)

	if _, err := db.Exec(`DELETE FROM login_attempts WHERE at <= ?`, cutoff); err != nil {
		return false, err
	}

	var n int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM login_attempts WHERE ip = ? AND at > ?`,
		ip, cutoff,
	).Scan(&n); err != nil {
		return false, err
	}
	if n >= LoginAttemptLimit {
		return false, nil
	}

	_, err := db.Exec(`INSERT INTO login_attempts (ip, at) VALUES (?, ?)`, ip, now.Format(time.RFC3339))
	if err != nil {
		return false, err
	}
	return true, nil
}
