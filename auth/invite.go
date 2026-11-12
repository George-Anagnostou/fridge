package auth

import (
	"database/sql"
	"errors"
	"time"
)

const DefaultInviteTTL = 7 * 24 * time.Hour

var (
	ErrInviteNotFound = errors.New("invite not found")
	ErrInviteExpired  = errors.New("invite expired")
	ErrInviteUsed     = errors.New("invite already used")
)

func CreateInvite(db *sql.DB, ttl time.Duration) (plainToken string, expiresAt time.Time, err error) {
	if ttl <= 0 {
		ttl = DefaultInviteTTL
	}
	plainToken, err = RandomToken()
	if err != nil {
		return "", time.Time{}, err
	}
	expiresAt = time.Now().UTC().Add(ttl)
	_, err = db.Exec(
		`INSERT INTO invites (token_hash, expires_at) VALUES (?, ?)`,
		HashToken(plainToken), expiresAt.Format(time.RFC3339),
	)
	if err != nil {
		return "", time.Time{}, err
	}
	return plainToken, expiresAt, nil
}

// RedeemInvite marks a valid, unused, unexpired invite as used.
// It is safe to call concurrently: only one caller can succeed.
func RedeemInvite(db *sql.DB, plainToken string) error {
	if plainToken == "" {
		return ErrInviteNotFound
	}
	var expires string
	var usedAt sql.NullString
	err := db.QueryRow(
		`SELECT expires_at, used_at FROM invites WHERE token_hash = ?`,
		HashToken(plainToken),
	).Scan(&expires, &usedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrInviteNotFound
	}
	if err != nil {
		return err
	}
	if usedAt.Valid {
		return ErrInviteUsed
	}
	exp, err := time.Parse(time.RFC3339, expires)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	if !exp.After(now) {
		return ErrInviteExpired
	}
	res, err := db.Exec(
		`UPDATE invites SET used_at = ? WHERE token_hash = ? AND used_at IS NULL AND expires_at > ?`,
		now.Format(time.RFC3339), HashToken(plainToken), now.Format(time.RFC3339),
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrInviteUsed
	}
	return nil
}
