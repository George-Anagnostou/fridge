package main

import (
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/George-Anagnostou/fridge/auth"
	"github.com/George-Anagnostou/fridge/fridge"
)

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := fridge.InitDB(filepath.Join(t.TempDir(), "fridge.db"))
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func testServer(t *testing.T) (*httptest.Server, *sql.DB) {
	t.Helper()
	db := testDB(t)
	srv := httptest.NewServer(newMux(db))
	t.Cleanup(srv.Close)
	return srv, db
}

func newClient(t *testing.T, srv *httptest.Server) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}
	c := srv.Client()
	c.Jar = jar
	c.Timeout = 5 * time.Second
	return c
}

func csrfFromHTML(t *testing.T, body string) string {
	t.Helper()
	m := regexp.MustCompile(`name="csrf_token" value="([^"]+)"`).FindStringSubmatch(body)
	if len(m) != 2 {
		t.Fatalf("csrf_token not found in body: %s", body)
	}
	return m[1]
}

func getCSRF(t *testing.T, client *http.Client, rawURL string) (token, body string, status int) {
	t.Helper()
	resp, err := client.Get(rawURL)
	if err != nil {
		t.Fatalf("GET %s: %v", rawURL, err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if resp.StatusCode == http.StatusOK {
		return csrfFromHTML(t, string(b)), string(b), resp.StatusCode
	}
	return "", string(b), resp.StatusCode
}

func signup(t *testing.T, srv *httptest.Server, db *sql.DB, client *http.Client, email, password string) {
	t.Helper()
	invite, _, err := auth.CreateInvite(db, time.Hour)
	if err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}
	token, _, status := getCSRF(t, client, srv.URL+"/signup?token="+url.QueryEscape(invite))
	if status != http.StatusOK {
		t.Fatalf("GET /signup status = %d", status)
	}
	resp, err := client.PostForm(srv.URL+"/signup", url.Values{
		"csrf_token": {token},
		"token":      {invite},
		"email":      {email},
		"password":   {password},
	})
	if err != nil {
		t.Fatalf("POST /signup: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusSeeOther {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST /signup status = %d; body: %s", resp.StatusCode, b)
	}
}

func TestGetIndexAnyCwd(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	empty := t.TempDir()
	if err := os.Chdir(empty); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(wd); err != nil {
			t.Errorf("restore cwd: %v", err)
		}
	})

	srv, db := testServer(t)
	client := newClient(t, srv)
	signup(t, srv, db, client, "user@example.com", "password123")

	resp, err := client.Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET / status = %d, want 200; body: %s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), "Fridge Manager") {
		t.Fatalf("GET / body missing title, got: %s", body)
	}
}

func TestGetItemsEmptyJSONArray(t *testing.T) {
	srv, db := testServer(t)
	client := newClient(t, srv)
	signup(t, srv, db, client, "user@example.com", "password123")

	resp, err := client.Get(srv.URL + "/items")
	if err != nil {
		t.Fatalf("GET /items: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /items status = %d, want 200; body: %s", resp.StatusCode, body)
	}

	var items []fridge.Item
	if err := json.Unmarshal(body, &items); err != nil {
		t.Fatalf("decode JSON: %v; body: %s", err, body)
	}
	if items == nil {
		t.Fatalf("GET /items decoded nil slice, raw body: %s", body)
	}
	if string(body) != "[]\n" && string(body) != "[]" {
		if strings.TrimSpace(string(body)) == "null" {
			t.Fatalf("GET /items body = %q, want []", body)
		}
		if len(items) != 0 {
			t.Fatalf("GET /items len = %d, want 0; body: %s", len(items), body)
		}
	}
}

func TestRemoveInvalidDoesNotHang(t *testing.T) {
	srv, db := testServer(t)
	client := newClient(t, srv)
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
	signup(t, srv, db, client, "user@example.com", "password123")

	token, _, status := getCSRF(t, client, srv.URL+"/")
	if status != http.StatusOK {
		t.Fatalf("GET / status = %d", status)
	}

	postRemove := func(id string) *http.Response {
		t.Helper()
		resp, err := client.PostForm(srv.URL+"/remove", url.Values{
			"id":         {id},
			"csrf_token": {token},
		})
		if err != nil {
			t.Fatalf("POST /remove id=%q: %v", id, err)
		}
		return resp
	}

	resp := postRemove("not-a-number")
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid id status = %d, want 400; body: %s", resp.StatusCode, body)
	}

	resp = postRemove("999")
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("missing id status = %d, want 303; body: %s", resp.StatusCode, body)
	}

	resp, err := client.Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("GET / after failed removes: %v", err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET / after failed removes status = %d, want 200; body: %s", resp.StatusCode, body)
	}

	token, _, status = getCSRF(t, client, srv.URL+"/")
	if status != http.StatusOK {
		t.Fatalf("GET / for add csrf status = %d", status)
	}
	resp, err = client.PostForm(srv.URL+"/", url.Values{
		"csrf_token": {token},
		"name":       {"Milk"},
		"quantity":   {"1"},
		"expiration": {"2026-11-17"},
	})
	if err != nil {
		t.Fatalf("POST / add after failed removes: %v", err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("POST / add status = %d, want 200 or 303; body: %s", resp.StatusCode, body)
	}
}

func TestUnauthenticatedAccess(t *testing.T) {
	srv, _ := testServer(t)
	client := newClient(t, srv)
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}

	resp, err := client.Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("GET / unauthenticated status = %d, want 303; body: %s", resp.StatusCode, body)
	}
	if loc := resp.Header.Get("Location"); !strings.HasPrefix(loc, "/login") {
		t.Fatalf("GET / Location = %q, want /login...", loc)
	}

	resp, err = client.Get(srv.URL + "/items")
	if err != nil {
		t.Fatalf("GET /items: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("GET /items unauthenticated status = %d, want 401", resp.StatusCode)
	}

	resp, err = client.PostForm(srv.URL+"/items", url.Values{})
	if err != nil {
		t.Fatalf("POST /items: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("POST /items unauthenticated status = %d, want 401", resp.StatusCode)
	}
}

func TestSignupRejectedWithoutInvite(t *testing.T) {
	srv, _ := testServer(t)
	client := newClient(t, srv)

	token, _, status := getCSRF(t, client, srv.URL+"/signup")
	if status != http.StatusOK {
		t.Fatalf("GET /signup status = %d", status)
	}
	resp, err := client.PostForm(srv.URL+"/signup", url.Values{
		"csrf_token": {token},
		"token":      {""},
		"email":      {"user@example.com"},
		"password":   {"password123"},
	})
	if err != nil {
		t.Fatalf("POST /signup: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusSeeOther {
		t.Fatalf("POST /signup without invite status = %d, want an error; body: %s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), "invite") {
		t.Fatalf("expected invite error, got: %s", body)
	}
}

func TestLoginLogoutFlow(t *testing.T) {
	srv, db := testServer(t)
	if _, err := auth.CreateUser(db, "user@example.com", "password123"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	client := newClient(t, srv)
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}

	token, _, status := getCSRF(t, client, srv.URL+"/login")
	if status != http.StatusOK {
		t.Fatalf("GET /login status = %d", status)
	}
	resp, err := client.PostForm(srv.URL+"/login", url.Values{
		"csrf_token": {token},
		"email":      {"user@example.com"},
		"password":   {"password123"},
		"next":       {"/"},
	})
	if err != nil {
		t.Fatalf("POST /login: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("POST /login status = %d, want 303; body: %s", resp.StatusCode, body)
	}

	resp, err = client.Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("GET / after login: %v", err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET / after login status = %d; body: %s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), "user@example.com") {
		t.Fatalf("expected signed-in email, got: %s", body)
	}

	token, _, status = getCSRF(t, client, srv.URL+"/")
	if status != http.StatusOK {
		t.Fatalf("GET / csrf status = %d", status)
	}
	resp, err = client.PostForm(srv.URL+"/logout", url.Values{"csrf_token": {token}})
	if err != nil {
		t.Fatalf("POST /logout: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("POST /logout status = %d, want 303", resp.StatusCode)
	}

	resp, err = client.Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("GET / after logout: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("GET / after logout status = %d, want 303", resp.StatusCode)
	}
}

func TestCSRFRejection(t *testing.T) {
	srv, db := testServer(t)
	client := newClient(t, srv)
	signup(t, srv, db, client, "user@example.com", "password123")

	// Missing token
	resp, err := client.PostForm(srv.URL+"/", url.Values{
		"name":       {"Milk"},
		"quantity":   {"1"},
		"expiration": {"2026-11-17"},
	})
	if err != nil {
		t.Fatalf("POST / without csrf: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("POST / without csrf status = %d, want 403; body: %s", resp.StatusCode, body)
	}

	// Wrong token
	resp, err = client.PostForm(srv.URL+"/", url.Values{
		"csrf_token": {"totally-wrong"},
		"name":       {"Milk"},
		"quantity":   {"1"},
		"expiration": {"2026-11-17"},
	})
	if err != nil {
		t.Fatalf("POST / bad csrf: %v", err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("POST / bad csrf status = %d, want 403; body: %s", resp.StatusCode, body)
	}

	// Login without CSRF
	anon := newClient(t, srv)
	resp, err = anon.PostForm(srv.URL+"/login", url.Values{
		"email":    {"user@example.com"},
		"password": {"password123"},
	})
	if err != nil {
		t.Fatalf("POST /login without csrf: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("POST /login without csrf status = %d, want 403", resp.StatusCode)
	}
}
