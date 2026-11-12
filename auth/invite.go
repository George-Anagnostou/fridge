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

// RegisterWithInvite creates a user if the invite is valid, in a single
// transaction so a failed sign-up does not consume the invite.
func RegisterWithInvite(db *sql.DB, email, password, inviteToken string) (User, error) {
	email, err := NormalizeEmail(email)
	if err != nil {
		return User{}, err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return User{}, err
	}
	if inviteToken == "" {
		return User{}, ErrInviteNotFound
	}

	tx, err := db.Begin()
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback()

	var expires string
	var usedAt sql.NullString
	err = tx.QueryRow(
		`SELECT expires_at, used_at FROM invites WHERE token_hash = ?`,
		HashToken(inviteToken),
	).Scan(&expires, &usedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrInviteNotFound
	}
	if err != nil {
		return User{}, err
	}
	if usedAt.Valid {
		return User{}, ErrInviteUsed
	}
	exp, err := time.Parse(time.RFC3339, expires)
	if err != nil {
		return User{}, err
	}
	now := time.Now().UTC()
	if !exp.After(now) {
		return User{}, ErrInviteExpired
	}

	res, err := tx.Exec(`INSERT INTO users (email, password_hash) VALUES (?, ?)`, email, hash)
	if err != nil {
		if isUniqueViolation(err) {
			return User{}, ErrEmailTaken
		}
		return User{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return User{}, err
	}

	upd, err := tx.Exec(
		`UPDATE invites SET used_at = ? WHERE token_hash = ? AND used_at IS NULL AND expires_at > ?`,
		now.Format(time.RFC3339), HashToken(inviteToken), now.Format(time.RFC3339),
	)
	if err != nil {
		return User{}, err
	}
	n, err := upd.RowsAffected()
	if err != nil {
		return User{}, err
	}
	if n == 0 {
		return User{}, ErrInviteUsed
	}
	if err := tx.Commit(); err != nil {
		return User{}, err
	}
	return User{ID: id, Email: email}, nil
}
