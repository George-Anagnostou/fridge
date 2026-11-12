package auth

import (
	"database/sql"
	"errors"
	"strings"
)

var (
	ErrInvalidEmail   = errors.New("invalid email")
	ErrEmailTaken     = errors.New("email already registered")
	ErrUserNotFound   = errors.New("user not found")
	ErrBadCredentials = errors.New("invalid email or password")
)

// dummyHash is used to keep login timing similar when the email is unknown.
var dummyHash string

func init() {
	var err error
	dummyHash, err = HashPassword("dummy-password-for-timing")
	if err != nil {
		panic("auth: dummy password hash: " + err.Error())
	}
}

type User struct {
	ID    int64
	Email string
}

func NormalizeEmail(email string) (string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	at := strings.LastIndex(email, "@")
	if at < 1 || at == len(email)-1 {
		return "", ErrInvalidEmail
	}
	domain := email[at+1:]
	if !strings.Contains(domain, ".") || strings.ContainsAny(email, " \t\n") {
		return "", ErrInvalidEmail
	}
	return email, nil
}

func CreateUser(db *sql.DB, email, password string) (User, error) {
	email, err := NormalizeEmail(email)
	if err != nil {
		return User{}, err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return User{}, err
	}
	res, err := db.Exec(`INSERT INTO users (email, password_hash) VALUES (?, ?)`, email, hash)
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
	return User{ID: id, Email: email}, nil
}

func UserByEmail(db *sql.DB, email string) (User, string, error) {
	email, err := NormalizeEmail(email)
	if err != nil {
		return User{}, "", err
	}
	var u User
	var hash string
	err = db.QueryRow(`SELECT id, email, password_hash FROM users WHERE email = ?`, email).
		Scan(&u.ID, &u.Email, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, "", ErrUserNotFound
	}
	if err != nil {
		return User{}, "", err
	}
	return u, hash, nil
}

func UserByID(db *sql.DB, id int64) (User, error) {
	var u User
	err := db.QueryRow(`SELECT id, email FROM users WHERE id = ?`, id).Scan(&u.ID, &u.Email)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrUserNotFound
	}
	if err != nil {
		return User{}, err
	}
	return u, nil
}

// Authenticate verifies email and password. Unknown emails still perform a
// password hash comparison so timing does not reveal whether the account exists.
func Authenticate(db *sql.DB, email, password string) (User, error) {
	u, hash, err := UserByEmail(db, email)
	if err != nil {
		_ = VerifyPassword(dummyHash, password)
		if errors.Is(err, ErrUserNotFound) || errors.Is(err, ErrInvalidEmail) {
			return User{}, ErrBadCredentials
		}
		return User{}, err
	}
	if err := VerifyPassword(hash, password); err != nil {
		return User{}, ErrBadCredentials
	}
	return u, nil
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "unique")
}
