package auth

import (
	"database/sql"
	"errors"
	"time"
)

const DefaultSessionTTL = 14 * 24 * time.Hour

var (
	ErrSessionNotFound = errors.New("session not found")
	ErrSessionExpired  = errors.New("session expired")
)

type Session struct {
	ID        int64
	UserID    int64
	ExpiresAt time.Time
	User      User
}

func CreateSession(db *sql.DB, userID int64, ttl time.Duration) (plainToken string, expiresAt time.Time, err error) {
	if ttl <= 0 {
		ttl = DefaultSessionTTL
	}
	plainToken, err = RandomToken()
	if err != nil {
		return "", time.Time{}, err
	}
	expiresAt = time.Now().UTC().Add(ttl)
	_, err = db.Exec(
		`INSERT INTO sessions (user_id, token_hash, expires_at) VALUES (?, ?, ?)`,
		userID, HashToken(plainToken), expiresAt.Format(time.RFC3339),
	)
	if err != nil {
		return "", time.Time{}, err
	}
	return plainToken, expiresAt, nil
}

func SessionByToken(db *sql.DB, plainToken string) (Session, error) {
	if plainToken == "" {
		return Session{}, ErrSessionNotFound
	}
	var s Session
	var expires string
	err := db.QueryRow(`
		SELECT s.id, s.user_id, s.expires_at, u.id, u.email
		FROM sessions s
		JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = ?`, HashToken(plainToken),
	).Scan(&s.ID, &s.UserID, &expires, &s.User.ID, &s.User.Email)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrSessionNotFound
	}
	if err != nil {
		return Session{}, err
	}
	exp, err := time.Parse(time.RFC3339, expires)
	if err != nil {
		return Session{}, err
	}
	s.ExpiresAt = exp
	if !exp.After(time.Now().UTC()) {
		return Session{}, ErrSessionExpired
	}
	return s, nil
}

func DeleteSession(db *sql.DB, plainToken string) error {
	if plainToken == "" {
		return nil
	}
	_, err := db.Exec(`DELETE FROM sessions WHERE token_hash = ?`, HashToken(plainToken))
	return err
}

func DeleteExpiredSessions(db *sql.DB) error {
	_, err := db.Exec(`DELETE FROM sessions WHERE expires_at <= ?`, time.Now().UTC().Format(time.RFC3339))
	return err
}
