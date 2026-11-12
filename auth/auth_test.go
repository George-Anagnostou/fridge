package auth

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/George-Anagnostou/fridge/fridge"
)

func setupDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := fridge.InitDB(filepath.Join(t.TempDir(), "fridge.db"))
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestHashPasswordAndVerify(t *testing.T) {
	hash, err := HashPassword("correct-horse")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if err := VerifyPassword(hash, "correct-horse"); err != nil {
		t.Fatalf("VerifyPassword(correct): %v", err)
	}
	if err := VerifyPassword(hash, "wrong-password"); !errors.Is(err, ErrInvalidPassword) {
		t.Fatalf("VerifyPassword(wrong) = %v, want ErrInvalidPassword", err)
	}
	if _, err := HashPassword("short"); !errors.Is(err, ErrPasswordTooShort) {
		t.Fatalf("HashPassword(short) = %v, want ErrPasswordTooShort", err)
	}
}

func TestNormalizeEmail(t *testing.T) {
	got, err := NormalizeEmail("  User@Example.COM ")
	if err != nil {
		t.Fatalf("NormalizeEmail: %v", err)
	}
	if got != "user@example.com" {
		t.Fatalf("NormalizeEmail = %q, want user@example.com", got)
	}
	if _, err := NormalizeEmail("not-an-email"); !errors.Is(err, ErrInvalidEmail) {
		t.Fatalf("NormalizeEmail(invalid) = %v, want ErrInvalidEmail", err)
	}
}

func TestCreateUserUniqueEmail(t *testing.T) {
	db := setupDB(t)
	u, err := CreateUser(db, "Alex@Example.com", "password123")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if u.Email != "alex@example.com" {
		t.Errorf("Email = %q, want lowercase", u.Email)
	}
	_, err = CreateUser(db, "alex@example.com", "password123")
	if !errors.Is(err, ErrEmailTaken) {
		t.Fatalf("second CreateUser = %v, want ErrEmailTaken", err)
	}
}

func TestAuthenticate(t *testing.T) {
	db := setupDB(t)
	if _, err := CreateUser(db, "user@example.com", "password123"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	u, err := Authenticate(db, "User@Example.com", "password123")
	if err != nil {
		t.Fatalf("Authenticate(good): %v", err)
	}
	if u.Email != "user@example.com" {
		t.Errorf("Email = %q", u.Email)
	}
	if _, err := Authenticate(db, "user@example.com", "nope-nope"); !errors.Is(err, ErrBadCredentials) {
		t.Fatalf("Authenticate(bad password) = %v, want ErrBadCredentials", err)
	}
	if _, err := Authenticate(db, "missing@example.com", "password123"); !errors.Is(err, ErrBadCredentials) {
		t.Fatalf("Authenticate(unknown) = %v, want ErrBadCredentials", err)
	}
}

func TestSessionCreateAndLookup(t *testing.T) {
	db := setupDB(t)
	u, err := CreateUser(db, "user@example.com", "password123")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	token, exp, err := CreateSession(db, u.ID, time.Hour)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if !exp.After(time.Now()) {
		t.Fatal("expiresAt should be in the future")
	}
	s, err := SessionByToken(db, token)
	if err != nil {
		t.Fatalf("SessionByToken: %v", err)
	}
	if s.User.Email != u.Email || s.UserID != u.ID {
		t.Fatalf("session user = %+v, want %+v", s.User, u)
	}
	if _, err := SessionByToken(db, "not-a-real-token"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("SessionByToken(unknown) = %v, want ErrSessionNotFound", err)
	}
	if err := DeleteSession(db, token); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if _, err := SessionByToken(db, token); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("SessionByToken after delete = %v, want ErrSessionNotFound", err)
	}
}

func TestSessionExpired(t *testing.T) {
	db := setupDB(t)
	u, err := CreateUser(db, "user@example.com", "password123")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	token, err := RandomToken()
	if err != nil {
		t.Fatalf("RandomToken: %v", err)
	}
	past := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339)
	if _, err := db.Exec(
		`INSERT INTO sessions (user_id, token_hash, expires_at) VALUES (?, ?, ?)`,
		u.ID, HashToken(token), past,
	); err != nil {
		t.Fatalf("insert expired session: %v", err)
	}
	if _, err := SessionByToken(db, token); !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("SessionByToken(expired) = %v, want ErrSessionExpired", err)
	}
}

func TestInviteRedeemOnceAndExpiry(t *testing.T) {
	db := setupDB(t)

	token, _, err := CreateInvite(db, time.Hour)
	if err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}
	if err := RedeemInvite(db, token); err != nil {
		t.Fatalf("RedeemInvite first: %v", err)
	}
	if err := RedeemInvite(db, token); !errors.Is(err, ErrInviteUsed) {
		t.Fatalf("RedeemInvite second = %v, want ErrInviteUsed", err)
	}
	if err := RedeemInvite(db, "missing-token"); !errors.Is(err, ErrInviteNotFound) {
		t.Fatalf("RedeemInvite(missing) = %v, want ErrInviteNotFound", err)
	}

	expired, err := RandomToken()
	if err != nil {
		t.Fatalf("RandomToken: %v", err)
	}
	past := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	if _, err := db.Exec(
		`INSERT INTO invites (token_hash, expires_at) VALUES (?, ?)`,
		HashToken(expired), past,
	); err != nil {
		t.Fatalf("insert expired invite: %v", err)
	}
	if err := RedeemInvite(db, expired); !errors.Is(err, ErrInviteExpired) {
		t.Fatalf("RedeemInvite(expired) = %v, want ErrInviteExpired", err)
	}
}
