package main

import (
	"context"
	"crypto/subtle"
	"errors"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/George-Anagnostou/fridge/auth"
)

const (
	sessionCookieName = "session"
	csrfCookieName    = "csrf"
)

type ctxKey int

const userContextKey ctxKey = 1

type formPage struct {
	CSRF  string
	Error string
	Token string
	Next  string
	Email string
}

func cookieSecureFromEnv() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("COOKIE_SECURE")))
	return v == "1" || v == "true" || v == "yes"
}

func (s *server) setCookie(w http.ResponseWriter, name, value string, ttl time.Duration) {
	c := &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.cookieSecure,
	}
	if ttl > 0 {
		c.Expires = time.Now().Add(ttl)
		c.MaxAge = int(ttl.Seconds())
	} else if ttl < 0 {
		c.MaxAge = -1
		c.Expires = time.Unix(0, 0)
	}
	http.SetCookie(w, c)
}

func (s *server) clearCookie(w http.ResponseWriter, name string) {
	s.setCookie(w, name, "", -1)
}

func (s *server) ensureCSRF(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, err := r.Cookie(csrfCookieName); err != nil {
			tok, err := auth.RandomToken()
			if err != nil {
				http.Error(w, "Unable to start a session", http.StatusInternalServerError)
				return
			}
			s.setCookie(w, csrfCookieName, tok, s.sessionTTL)
			// Make the new token visible to this request (for form rendering).
			r.AddCookie(&http.Cookie{Name: csrfCookieName, Value: tok})
		}
		next(w, r)
	}
}

func (s *server) csrfToken(r *http.Request) string {
	c, err := r.Cookie(csrfCookieName)
	if err != nil {
		return ""
	}
	return c.Value
}

func (s *server) requireCSRFOnPost(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodDelete || r.Method == http.MethodPatch {
			provided := r.FormValue("csrf_token")
			if provided == "" {
				provided = r.Header.Get("X-CSRF-Token")
			}
			expected := s.csrfToken(r)
			if expected == "" || provided == "" ||
				subtle.ConstantTimeCompare([]byte(expected), []byte(provided)) != 1 {
				http.Error(w, "Invalid CSRF token", http.StatusForbidden)
				return
			}
		}
		next(w, r)
	}
}

func (s *server) currentUser(r *http.Request) (auth.User, bool) {
	c, err := r.Cookie(sessionCookieName)
	if err != nil || c.Value == "" {
		return auth.User{}, false
	}
	sess, err := auth.SessionByToken(s.db, c.Value)
	if err != nil {
		return auth.User{}, false
	}
	return sess.User, true
}

func (s *server) requireLogin(next http.HandlerFunc, api bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := s.currentUser(r)
		if !ok {
			if api {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			nextURL := r.URL.RequestURI()
			http.Redirect(w, r, "/login?next="+urlQueryEscape(nextURL), http.StatusSeeOther)
			return
		}
		ctx := context.WithValue(r.Context(), userContextKey, user)
		next(w, r.WithContext(ctx))
	}
}

func userFrom(r *http.Request) (auth.User, bool) {
	u, ok := r.Context().Value(userContextKey).(auth.User)
	return u, ok
}

func (s *server) protectHTML(next http.HandlerFunc) http.HandlerFunc {
	return s.ensureCSRF(s.requireLogin(s.requireCSRFOnPost(next), false))
}

func (s *server) protectAPI(next http.HandlerFunc) http.HandlerFunc {
	return s.ensureCSRF(s.requireLogin(s.requireCSRFOnPost(next), true))
}

func (s *server) loginForm(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.currentUser(r); ok {
		http.Redirect(w, r, safeNext(r.URL.Query().Get("next")), http.StatusSeeOther)
		return
	}
	s.renderAuth(w, "login.html", formPage{
		CSRF: s.csrfToken(r),
		Next: safeNext(r.URL.Query().Get("next")),
	})
}

func (s *server) loginSubmit(w http.ResponseWriter, r *http.Request) {
	ok, err := auth.AllowLoginAttempt(s.db, clientIP(r), time.Now())
	if err != nil {
		http.Error(w, "Unable to process login", http.StatusInternalServerError)
		log.Println("login attempt:", err)
		return
	}
	if !ok {
		http.Error(w, "Too many login attempts, try again later", http.StatusTooManyRequests)
		return
	}
	email := r.FormValue("email")
	password := r.FormValue("password")
	next := safeNext(r.FormValue("next"))

	user, err := auth.Authenticate(s.db, email, password)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		s.renderAuth(w, "login.html", formPage{
			CSRF:  s.csrfToken(r),
			Next:  next,
			Email: email,
			Error: "Invalid email or password.",
		})
		return
	}
	if err := s.establishSession(w, user.ID); err != nil {
		http.Error(w, "Unable to create a session", http.StatusInternalServerError)
		log.Println("create session:", err)
		return
	}
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func (s *server) signupForm(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.currentUser(r); ok {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	s.renderAuth(w, "signup.html", formPage{
		CSRF:  s.csrfToken(r),
		Token: r.URL.Query().Get("token"),
	})
}

func (s *server) signupSubmit(w http.ResponseWriter, r *http.Request) {
	token := r.FormValue("token")
	email := r.FormValue("email")
	password := r.FormValue("password")

	user, err := auth.RegisterWithInvite(s.db, email, password, token)
	if err != nil {
		status := http.StatusBadRequest
		msg := "Unable to sign up with that invite."
		switch {
		case errors.Is(err, auth.ErrInviteNotFound), errors.Is(err, auth.ErrInviteExpired), errors.Is(err, auth.ErrInviteUsed):
			msg = "This invite is invalid, expired, or already used."
		case errors.Is(err, auth.ErrInvalidEmail):
			msg = "Enter a valid email address."
		case errors.Is(err, auth.ErrPasswordTooShort):
			msg = "Password must be at least 8 characters."
		case errors.Is(err, auth.ErrEmailTaken):
			msg = "That email is already registered."
			status = http.StatusConflict
		default:
			status = http.StatusInternalServerError
			log.Println("signup:", err)
		}
		w.WriteHeader(status)
		s.renderAuth(w, "signup.html", formPage{
			CSRF:  s.csrfToken(r),
			Token: token,
			Email: email,
			Error: msg,
		})
		return
	}
	if err := s.establishSession(w, user.ID); err != nil {
		http.Error(w, "Account created, but sign-in failed. Please log in.", http.StatusInternalServerError)
		log.Println("signup session:", err)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookieName); err == nil {
		if err := auth.DeleteSession(s.db, c.Value); err != nil {
			log.Println("delete session:", err)
		}
	}
	s.clearCookie(w, sessionCookieName)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (s *server) establishSession(w http.ResponseWriter, userID int64) error {
	token, _, err := auth.CreateSession(s.db, userID, s.sessionTTL)
	if err != nil {
		return err
	}
	s.setCookie(w, sessionCookieName, token, s.sessionTTL)
	return nil
}

func (s *server) renderAuth(w http.ResponseWriter, name string, data formPage) {
	if err := s.tmpl.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, "Unable to render template", http.StatusInternalServerError)
		log.Println("Error rendering template:", err)
	}
}

func safeNext(v string) string {
	if v == "" || !strings.HasPrefix(v, "/") || strings.HasPrefix(v, "//") {
		return "/"
	}
	return v
}

func urlQueryEscape(v string) string {
	return url.QueryEscape(v)
}

func trustProxyFromEnv() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("TRUST_PROXY")))
	return v == "1" || v == "true" || v == "yes"
}

func clientIP(r *http.Request) string {
	if trustProxyFromEnv() {
		if ip := strings.TrimSpace(r.Header.Get("X-Real-IP")); ip != "" {
			return stripHostPort(ip)
		}
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			first := strings.TrimSpace(strings.Split(xff, ",")[0])
			if first != "" {
				return stripHostPort(first)
			}
		}
	}
	return stripHostPort(r.RemoteAddr)
}

func stripHostPort(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return host
}
