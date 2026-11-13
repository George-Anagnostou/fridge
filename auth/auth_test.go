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

func TestRegisterWithInvite(t *testing.T) {
	db := setupDB(t)

	_, err := RegisterWithInvite(db, "user@example.com", "password123", "")
	if !errors.Is(err, ErrInviteNotFound) {
		t.Fatalf("RegisterWithInvite(no token) = %v, want ErrInviteNotFound", err)
	}
	_, err = RegisterWithInvite(db, "user@example.com", "password123", "bogus")
	if !errors.Is(err, ErrInviteNotFound) {
		t.Fatalf("RegisterWithInvite(bad token) = %v, want ErrInviteNotFound", err)
	}

	token, _, err := CreateInvite(db, time.Hour)
	if err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}
	u, err := RegisterWithInvite(db, "User@Example.com", "password123", token)
	if err != nil {
		t.Fatalf("RegisterWithInvite: %v", err)
	}
	if u.Email != "user@example.com" {
		t.Errorf("Email = %q", u.Email)
	}
	if _, err := RegisterWithInvite(db, "other@example.com", "password123", token); !errors.Is(err, ErrInviteUsed) {
		t.Fatalf("RegisterWithInvite(reused) = %v, want ErrInviteUsed", err)
	}
	// The first invite was used; a failed attempt must not consume a new invite
	// when the email is already taken... actually we used a different email with same token.
	// Add a fresh invite and try a taken email — invite should remain usable.
	token2, _, err := CreateInvite(db, time.Hour)
	if err != nil {
		t.Fatalf("CreateInvite 2: %v", err)
	}
	_, err = RegisterWithInvite(db, "user@example.com", "password123", token2)
	if !errors.Is(err, ErrEmailTaken) {
		t.Fatalf("RegisterWithInvite(taken) = %v, want ErrEmailTaken", err)
	}
	u2, err := RegisterWithInvite(db, "other@example.com", "password123", token2)
	if err != nil {
		t.Fatalf("RegisterWithInvite after taken-email failure: %v", err)
	}
	if u2.Email != "other@example.com" {
		t.Errorf("Email = %q", u2.Email)
	}
}

func TestAdminGrantingInvite(t *testing.T) {
	db := setupDB(t)
	token, _, err := CreateInviteSpec(db, InviteSpec{TTL: time.Hour, GrantsAdmin: true})
	if err != nil {
		t.Fatalf("CreateInviteSpec: %v", err)
	}
	u, err := RegisterWithInvite(db, "admin@example.com", "password123", token)
	if err != nil {
		t.Fatalf("RegisterWithInvite: %v", err)
	}
	if !u.IsAdmin {
		t.Fatal("expected IsAdmin true from grants_admin invite")
	}
	got, err := UserByID(db, u.ID)
	if err != nil {
		t.Fatalf("UserByID: %v", err)
	}
	if !got.IsAdmin {
		t.Fatal("stored user is not admin")
	}

	// A normal invite does not grant admin.
	token2, _, err := CreateInvite(db, time.Hour)
	if err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}
	u2, err := RegisterWithInvite(db, "user@example.com", "password123", token2)
	if err != nil {
		t.Fatalf("RegisterWithInvite normal: %v", err)
	}
	if u2.IsAdmin {
		t.Fatal("normal invite should not grant admin")
	}
}

func TestInviteCreatedBy(t *testing.T) {
	db := setupDB(t)
	admin, err := CreateUser(db, "admin@example.com", "password123")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	// Promote via SQL so we have an existing admin id to attribute.
	if _, err := db.Exec(`UPDATE users SET is_admin = 1 WHERE id = ?`, admin.ID); err != nil {
		t.Fatalf("promote: %v", err)
	}
	id := admin.ID
	token, _, err := CreateInviteSpec(db, InviteSpec{TTL: time.Hour, CreatedBy: &id})
	if err != nil {
		t.Fatalf("CreateInviteSpec: %v", err)
	}
	var createdBy sql.NullInt64
	if err := db.QueryRow(`SELECT created_by FROM invites WHERE token_hash = ?`, HashToken(token)).Scan(&createdBy); err != nil {
		t.Fatalf("lookup created_by: %v", err)
	}
	if !createdBy.Valid || createdBy.Int64 != id {
		t.Fatalf("created_by = %v, want %d", createdBy, id)
	}
}
