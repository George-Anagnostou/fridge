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
	"strconv"
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
	// srv.Client() may return a shared instance; copy so each jar stays separate.
	base := srv.Client()
	c := &http.Client{
		Transport:     base.Transport,
		CheckRedirect: base.CheckRedirect,
		Jar:           jar,
		Timeout:       5 * time.Second,
	}
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

func signupAdmin(t *testing.T, srv *httptest.Server, db *sql.DB, client *http.Client, email, password string) {
	t.Helper()
	invite, _, err := auth.CreateInviteSpec(db, auth.InviteSpec{TTL: time.Hour, GrantsAdmin: true})
	if err != nil {
		t.Fatalf("CreateInviteSpec: %v", err)
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
		t.Fatalf("POST /signup admin: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusSeeOther {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST /signup admin status = %d; body: %s", resp.StatusCode, b)
	}
}

func createStore(t *testing.T, srv *httptest.Server, client *http.Client, name string) int64 {
	t.Helper()
	token, _, status := getCSRF(t, client, srv.URL+"/")
	if status != http.StatusOK {
		t.Fatalf("GET / for create store status = %d", status)
	}
	// Do not follow the 303 so we can read the new store ID from Location.
	prev := client.CheckRedirect
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
	defer func() { client.CheckRedirect = prev }()
	resp, err := client.PostForm(srv.URL+"/stores", url.Values{
		"csrf_token": {token},
		"name":       {name},
	})
	if err != nil {
		t.Fatalf("POST /stores: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST /stores status = %d, want 303; body: %s", resp.StatusCode, b)
	}
	loc := resp.Header.Get("Location")
	m := regexp.MustCompile(`/stores/(\d+)`).FindStringSubmatch(loc)
	if len(m) != 2 {
		t.Fatalf("POST /stores Location = %q, want /stores/{id}", loc)
	}
	id, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil {
		t.Fatalf("store id: %v", err)
	}
	return id
}

func storePath(id int64, suffix string) string {
	return "/stores/" + strconv.FormatInt(id, 10) + suffix
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
	storeID := createStore(t, srv, client, "Test store")

	resp, err := client.Get(srv.URL + storePath(storeID, "/items"))
	if err != nil {
		t.Fatalf("GET /stores/{id}/items: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /stores/{id}/items status = %d, want 200; body: %s", resp.StatusCode, body)
	}

	var items []fridge.Item
	if err := json.Unmarshal(body, &items); err != nil {
		t.Fatalf("decode JSON: %v; body: %s", err, body)
	}
	if items == nil {
		t.Fatalf("GET /stores/{id}/items decoded nil slice, raw body: %s", body)
	}
	if string(body) != "[]\n" && string(body) != "[]" {
		if strings.TrimSpace(string(body)) == "null" {
			t.Fatalf("GET /stores/{id}/items body = %q, want []", body)
		}
		if len(items) != 0 {
			t.Fatalf("GET /stores/{id}/items len = %d, want 0; body: %s", len(items), body)
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
	storeID := createStore(t, srv, client, "Test store")

	token, _, status := getCSRF(t, client, srv.URL+storePath(storeID, ""))
	if status != http.StatusOK {
		t.Fatalf("GET /stores/{id} status = %d", status)
	}

	postRemove := func(id string) *http.Response {
		t.Helper()
		resp, err := client.PostForm(srv.URL+storePath(storeID, "/items/remove"), url.Values{
			"id":         {id},
			"csrf_token": {token},
		})
		if err != nil {
			t.Fatalf("POST remove id=%q: %v", id, err)
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

	resp, err := client.Get(srv.URL + storePath(storeID, ""))
	if err != nil {
		t.Fatalf("GET store after failed removes: %v", err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET store after failed removes status = %d, want 200; body: %s", resp.StatusCode, body)
	}

	token, _, status = getCSRF(t, client, srv.URL+storePath(storeID, ""))
	if status != http.StatusOK {
		t.Fatalf("GET store for add csrf status = %d", status)
	}
	resp, err = client.PostForm(srv.URL+storePath(storeID, ""), url.Values{
		"csrf_token": {token},
		"name":       {"Milk"},
		"quantity":   {"1"},
		"expiration": {"2026-11-17"},
	})
	if err != nil {
		t.Fatalf("POST add after failed removes: %v", err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("POST add status = %d, want 200 or 303; body: %s", resp.StatusCode, body)
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

	resp, err = client.Get(srv.URL + "/stores/1/items")
	if err != nil {
		t.Fatalf("GET /stores/1/items: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("GET /stores/1/items unauthenticated status = %d, want 401", resp.StatusCode)
	}

	resp, err = client.PostForm(srv.URL+"/stores/1/items", url.Values{})
	if err != nil {
		t.Fatalf("POST /stores/1/items: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("POST /stores/1/items unauthenticated status = %d, want 401", resp.StatusCode)
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
	resp, err := client.PostForm(srv.URL+"/stores", url.Values{
		"name":       {"Milk"},
		"quantity":   {"1"},
		"expiration": {"2026-11-17"},
	})
	if err != nil {
		t.Fatalf("POST /stores without csrf: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("POST /stores without csrf status = %d, want 403; body: %s", resp.StatusCode, body)
	}

	// Wrong token
	resp, err = client.PostForm(srv.URL+"/stores", url.Values{
		"csrf_token": {"totally-wrong"},
		"name":       {"Milk"},
		"quantity":   {"1"},
		"expiration": {"2026-11-17"},
	})
	if err != nil {
		t.Fatalf("POST /stores bad csrf: %v", err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("POST /stores bad csrf status = %d, want 403; body: %s", resp.StatusCode, body)
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

func TestNonAdminCannotCreateInvites(t *testing.T) {
	srv, db := testServer(t)
	client := newClient(t, srv)
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
	signup(t, srv, db, client, "user@example.com", "password123")

	resp, err := client.Get(srv.URL + "/admin/invites")
	if err != nil {
		t.Fatalf("GET /admin/invites: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("GET /admin/invites as non-admin status = %d, want 403; body: %s", resp.StatusCode, body)
	}

	token, _, status := getCSRF(t, client, srv.URL+"/")
	if status != http.StatusOK {
		t.Fatalf("GET / status = %d", status)
	}
	resp, err = client.PostForm(srv.URL+"/admin/invites", url.Values{"csrf_token": {token}})
	if err != nil {
		t.Fatalf("POST /admin/invites: %v", err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("POST /admin/invites as non-admin status = %d, want 403; body: %s", resp.StatusCode, body)
	}
}

func TestAdminCanCreateSignUpInvite(t *testing.T) {
	srv, db := testServer(t)
	client := newClient(t, srv)
	signupAdmin(t, srv, db, client, "admin@example.com", "password123")

	token, body, status := getCSRF(t, client, srv.URL+"/admin/invites")
	if status != http.StatusOK {
		t.Fatalf("GET /admin/invites as admin status = %d; body: %s", status, body)
	}

	resp, err := client.PostForm(srv.URL+"/admin/invites", url.Values{"csrf_token": {token}})
	if err != nil {
		t.Fatalf("POST /admin/invites: %v", err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /admin/invites status = %d; body: %s", resp.StatusCode, out)
	}
	if !strings.Contains(string(out), "/signup?token=") {
		t.Fatalf("expected invite link in body, got: %s", out)
	}

	// The link should create a non-admin user.
	m := regexp.MustCompile(`/signup\?token=([^"&\s<]+)`).FindStringSubmatch(string(out))
	if len(m) != 2 {
		t.Fatalf("could not parse invite token from: %s", out)
	}
	inviteToken, err := url.QueryUnescape(m[1])
	if err != nil {
		t.Fatalf("unescape token: %v", err)
	}
	u, err := auth.RegisterWithInvite(db, "newbie@example.com", "password123", inviteToken)
	if err != nil {
		t.Fatalf("RegisterWithInvite via admin link: %v", err)
	}
	if u.IsAdmin {
		t.Fatal("admin-created sign-up invite should not grant admin")
	}
}

func TestAdminGrantingInviteSignsUpAdmin(t *testing.T) {
	srv, db := testServer(t)
	client := newClient(t, srv)
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
	signupAdmin(t, srv, db, client, "admin@example.com", "password123")

	resp, err := client.Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET / status = %d; body: %s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), "/admin/invites") {
		t.Fatalf("admin home page missing invites link, got: %s", body)
	}
}

func TestClientIPTrustProxy(t *testing.T) {
	newReq := func(remote string) *http.Request {
		req := httptest.NewRequest(http.MethodPost, "/login", nil)
		req.RemoteAddr = remote
		req.Header.Set("X-Real-IP", "203.0.113.10")
		req.Header.Set("X-Forwarded-For", "198.51.100.2, 10.0.0.1")
		return req
	}

	t.Setenv("TRUST_PROXY", "")
	req := newReq("127.0.0.1:1234")
	if got := clientIP(req); got != "127.0.0.1" {
		t.Fatalf("TRUST_PROXY unset clientIP = %q, want 127.0.0.1 (headers ignored)", got)
	}

	t.Setenv("TRUST_PROXY", "1")
	if got := clientIP(newReq("10.0.0.8:1234")); got != "10.0.0.8" {
		t.Fatalf("non-loopback peer clientIP = %q, want 10.0.0.8 (spoofed X-Real-IP ignored)", got)
	}
	if got := clientIP(newReq("127.0.0.1:1234")); got != "203.0.113.10" {
		t.Fatalf("loopback peer clientIP = %q, want X-Real-IP 203.0.113.10", got)
	}
	if got := clientIP(newReq("[::1]:1234")); got != "203.0.113.10" {
		t.Fatalf("IPv6 loopback peer clientIP = %q, want X-Real-IP 203.0.113.10", got)
	}

	req = newReq("127.0.0.1:1234")
	req.Header.Del("X-Real-IP")
	if got := clientIP(req); got != "127.0.0.1" {
		t.Fatalf("loopback peer without X-Real-IP clientIP = %q, want 127.0.0.1 (leading XFF ignored)", got)
	}
}

func TestListenAddr(t *testing.T) {
	t.Setenv("FRIDGE_ADDR", "")
	t.Setenv("PORT", "")
	if got := listenAddr(); got != "127.0.0.1:8080" {
		t.Fatalf("default listenAddr = %q, want 127.0.0.1:8080", got)
	}

	t.Setenv("PORT", "3000")
	if got := listenAddr(); got != "127.0.0.1:3000" {
		t.Fatalf("PORT listenAddr = %q, want 127.0.0.1:3000", got)
	}

	t.Setenv("FRIDGE_ADDR", "0.0.0.0:9090")
	t.Setenv("PORT", "3000")
	if got := listenAddr(); got != "0.0.0.0:9090" {
		t.Fatalf("FRIDGE_ADDR overrides PORT, listenAddr = %q, want 0.0.0.0:9090", got)
	}
}

func TestLoginAttemptLimitHTTP(t *testing.T) {
	srv, db := testServer(t)
	if _, err := auth.CreateUser(db, "user@example.com", "password123"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	client := newClient(t, srv)

	token, _, status := getCSRF(t, client, srv.URL+"/login")
	if status != http.StatusOK {
		t.Fatalf("GET /login status = %d", status)
	}
	// Seed the window with the limit number of attempts.
	now := time.Now()
	for i := 0; i < auth.LoginAttemptLimit; i++ {
		ok, err := auth.AllowLoginAttempt(db, "127.0.0.1", now.Add(time.Duration(i)*time.Second))
		if err != nil || !ok {
			t.Fatalf("seed attempt %d: %v %v", i, ok, err)
		}
	}
	resp, err := client.PostForm(srv.URL+"/login", url.Values{
		"csrf_token": {token},
		"email":      {"user@example.com"},
		"password":   {"password123"},
	})
	if err != nil {
		t.Fatalf("POST /login: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("POST /login over limit status = %d, want 429", resp.StatusCode)
	}
}

func TestStoreIsolationAndPermissionsHTTP(t *testing.T) {
	srv, db := testServer(t)
	owner := newClient(t, srv)
	signup(t, srv, db, owner, "owner@example.com", "password123")
	storeID := createStore(t, srv, owner, "Owner store")

	outsider := newClient(t, srv)
	signup(t, srv, db, outsider, "other@example.com", "password123")

	resp, err := outsider.Get(srv.URL + storePath(storeID, ""))
	if err != nil {
		t.Fatalf("outsider GET store: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("outsider GET store status = %d, want 404; body: %s", resp.StatusCode, body)
	}

	// Add an existing user as view, then as a second user as edit, via the owner members form.
	viewUser, err := auth.CreateUser(db, "view@example.com", "password123")
	if err != nil {
		t.Fatalf("CreateUser view: %v", err)
	}
	editUser, err := auth.CreateUser(db, "edit@example.com", "password123")
	if err != nil {
		t.Fatalf("CreateUser edit: %v", err)
	}
	ownerRow, _, err := auth.UserByEmail(db, "owner@example.com")
	if err != nil {
		t.Fatalf("owner lookup: %v", err)
	}
	if err := fridge.AddMember(db, storeID, ownerRow.ID, viewUser.ID, fridge.PermView); err != nil {
		t.Fatalf("AddMember view: %v", err)
	}
	if err := fridge.AddMember(db, storeID, ownerRow.ID, editUser.ID, fridge.PermEdit); err != nil {
		t.Fatalf("AddMember edit: %v", err)
	}

	viewer := newClient(t, srv)
	loginAs(t, srv, viewer, "view@example.com", "password123")
	editor := newClient(t, srv)
	loginAs(t, srv, editor, "edit@example.com", "password123")

	resp, err = viewer.Get(srv.URL + storePath(storeID, ""))
	if err != nil {
		t.Fatalf("viewer GET: %v", err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("viewer GET status = %d; body: %s", resp.StatusCode, body)
	}
	if strings.Contains(string(body), "Add Item") {
		t.Fatalf("viewer saw add form: %s", body)
	}

	token := csrfFromHTML(t, string(body))
	resp, err = viewer.PostForm(srv.URL+storePath(storeID, ""), url.Values{
		"csrf_token": {token},
		"name":       {"Milk"},
		"quantity":   {"1"},
		"expiration": {"2026-11-17"},
	})
	if err != nil {
		t.Fatalf("viewer POST add: %v", err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("viewer add status = %d, want 403; body: %s", resp.StatusCode, body)
	}

	token, _, status := getCSRF(t, editor, srv.URL+storePath(storeID, ""))
	if status != http.StatusOK {
		t.Fatalf("editor GET status = %d", status)
	}
	resp, err = editor.PostForm(srv.URL+storePath(storeID, ""), url.Values{
		"csrf_token": {token},
		"name":       {"Milk"},
		"quantity":   {"1"},
		"expiration": {"2026-11-17"},
	})
	if err != nil {
		t.Fatalf("editor POST add: %v", err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("editor add status = %d, want 200 or 303; body: %s", resp.StatusCode, body)
	}

	// View and edit members cannot manage members.
	resp, err = viewer.Get(srv.URL + storePath(storeID, "/members"))
	if err != nil {
		t.Fatalf("viewer GET members: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("viewer GET members status = %d, want 403", resp.StatusCode)
	}
	token, _, status = getCSRF(t, editor, srv.URL+storePath(storeID, ""))
	if status != http.StatusOK {
		t.Fatalf("editor GET for member csrf status = %d", status)
	}
	resp, err = editor.PostForm(srv.URL+storePath(storeID, "/members"), url.Values{
		"csrf_token": {token},
		"email":      {"someone@example.com"},
		"permission": {"view"},
	})
	if err != nil {
		t.Fatalf("editor POST members: %v", err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("editor POST members status = %d, want 403; body: %s", resp.StatusCode, body)
	}

	// Owner can open the members page.
	resp, err = owner.Get(srv.URL + storePath(storeID, "/members"))
	if err != nil {
		t.Fatalf("owner GET members: %v", err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("owner GET members status = %d; body: %s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), "view@example.com") {
		t.Fatalf("owner members page missing viewer email, got: %s", body)
	}
}

func loginAs(t *testing.T, srv *httptest.Server, client *http.Client, email, password string) {
	t.Helper()
	token, _, status := getCSRF(t, client, srv.URL+"/login")
	if status != http.StatusOK {
		t.Fatalf("GET /login status = %d", status)
	}
	resp, err := client.PostForm(srv.URL+"/login", url.Values{
		"csrf_token": {token},
		"email":      {email},
		"password":   {password},
	})
	if err != nil {
		t.Fatalf("POST /login %s: %v", email, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusSeeOther {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST /login %s status = %d; body: %s", email, resp.StatusCode, b)
	}
}

func TestStoreInviteCreatesAccountAndMembership(t *testing.T) {
	srv, db := testServer(t)
	owner := newClient(t, srv)
	signup(t, srv, db, owner, "owner@example.com", "password123")
	storeID := createStore(t, srv, owner, "Shared store")

	token, body, status := getCSRF(t, owner, srv.URL+storePath(storeID, "/members"))
	if status != http.StatusOK {
		t.Fatalf("GET members status = %d; body: %s", status, body)
	}
	resp, err := owner.PostForm(srv.URL+storePath(storeID, "/members"), url.Values{
		"csrf_token": {token},
		"email":      {"new@example.com"},
		"permission": {"edit"},
	})
	if err != nil {
		t.Fatalf("POST share: %v", err)
	}
	bodyBytes, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST share status = %d; body: %s", resp.StatusCode, bodyBytes)
	}
	m := regexp.MustCompile(`/signup\?token=([^"]+)`).FindStringSubmatch(string(bodyBytes))
	if len(m) != 2 {
		t.Fatalf("invite link missing from members page: %s", bodyBytes)
	}
	invite, err := url.QueryUnescape(m[1])
	if err != nil {
		t.Fatalf("unescape token: %v", err)
	}

	guest := newClient(t, srv)
	csrf, _, status := getCSRF(t, guest, srv.URL+"/signup?token="+url.QueryEscape(invite))
	if status != http.StatusOK {
		t.Fatalf("GET /signup status = %d", status)
	}
	prev := guest.CheckRedirect
	guest.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
	resp, err = guest.PostForm(srv.URL+"/signup", url.Values{
		"csrf_token": {csrf},
		"token":      {invite},
		"email":      {"new@example.com"},
		"password":   {"password123"},
	})
	guest.CheckRedirect = prev
	if err != nil {
		t.Fatalf("POST /signup: %v", err)
	}
	bodyBytes, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("POST /signup status = %d, want 303; body: %s", resp.StatusCode, bodyBytes)
	}
	if loc := resp.Header.Get("Location"); loc != storePath(storeID, "") {
		t.Fatalf("POST /signup Location = %q, want %s", loc, storePath(storeID, ""))
	}

	resp, err = guest.Get(srv.URL + storePath(storeID, ""))
	if err != nil {
		t.Fatalf("new member GET store: %v", err)
	}
	bodyBytes, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("new member GET store status = %d; body: %s", resp.StatusCode, bodyBytes)
	}
	if !strings.Contains(string(bodyBytes), "Add Item") {
		t.Fatalf("edit member should see add form: %s", bodyBytes)
	}

	// Invite is single-use.
	other := newClient(t, srv)
	csrf, _, status = getCSRF(t, other, srv.URL+"/signup?token="+url.QueryEscape(invite))
	if status != http.StatusOK {
		t.Fatalf("GET /signup reused status = %d", status)
	}
	resp, err = other.PostForm(srv.URL+"/signup", url.Values{
		"csrf_token": {csrf},
		"token":      {invite},
		"email":      {"another@example.com"},
		"password":   {"password123"},
	})
	if err != nil {
		t.Fatalf("POST /signup reused: %v", err)
	}
	bodyBytes, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusSeeOther {
		t.Fatalf("reused store invite succeeded with status %d; body: %s", resp.StatusCode, bodyBytes)
	}
}

func addItem(t *testing.T, srv *httptest.Server, client *http.Client, storeID int64, fields url.Values) int {
	t.Helper()
	token, _, status := getCSRF(t, client, srv.URL+storePath(storeID, ""))
	if status != http.StatusOK {
		t.Fatalf("GET store status = %d", status)
	}
	fields.Set("csrf_token", token)
	prev := client.CheckRedirect
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
	resp, err := client.PostForm(srv.URL+storePath(storeID, ""), fields)
	client.CheckRedirect = prev
	if err != nil {
		t.Fatalf("POST add item: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("POST add item status = %d, want 303; body: %s", resp.StatusCode, body)
	}
	_, page, status := getCSRF(t, client, srv.URL+storePath(storeID, ""))
	if status != http.StatusOK {
		t.Fatalf("GET store after add status = %d", status)
	}
	matches := regexp.MustCompile(`/items/(\d+)/edit`).FindAllStringSubmatch(page, -1)
	if len(matches) == 0 {
		t.Fatalf("no edit link after add; body: %s", page)
	}
	id, err := strconv.Atoi(matches[len(matches)-1][1])
	if err != nil {
		t.Fatalf("item id: %v", err)
	}
	return id
}

func TestEditItemPermissionsAndValidation(t *testing.T) {
	srv, db := testServer(t)
	owner := newClient(t, srv)
	signup(t, srv, db, owner, "owner@example.com", "password123")
	storeID := createStore(t, srv, owner, "Pantry")
	itemID := addItem(t, srv, owner, storeID, url.Values{
		"name":       {"Milk"},
		"quantity":   {"1"},
		"expiration": {"2026-12-01"},
		"brand":      {"Dairy Co"},
	})

	viewUser, err := auth.CreateUser(db, "view@example.com", "password123")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	ownerRow, _, err := auth.UserByEmail(db, "owner@example.com")
	if err != nil {
		t.Fatalf("owner lookup: %v", err)
	}
	if err := fridge.AddMember(db, storeID, ownerRow.ID, viewUser.ID, fridge.PermView); err != nil {
		t.Fatalf("AddMember: %v", err)
	}
	viewer := newClient(t, srv)
	loginAs(t, srv, viewer, "view@example.com", "password123")

	editURL := srv.URL + storePath(storeID, "/items/"+strconv.Itoa(itemID)+"/edit")
	resp, err := viewer.Get(editURL)
	if err != nil {
		t.Fatalf("viewer GET edit: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("viewer GET edit status = %d, want 403; body: %s", resp.StatusCode, body)
	}

	token, _, status := getCSRF(t, viewer, srv.URL+storePath(storeID, ""))
	if status != http.StatusOK {
		t.Fatalf("viewer GET store status = %d", status)
	}
	resp, err = viewer.PostForm(editURL, url.Values{
		"csrf_token": {token},
		"name":       {"Hacked"},
		"quantity":   {"9"},
		"expiration": {"2026-12-01"},
	})
	if err != nil {
		t.Fatalf("viewer POST edit: %v", err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("viewer POST edit status = %d, want 403; body: %s", resp.StatusCode, body)
	}

	token, formBody, status := getCSRF(t, owner, editURL)
	if status != http.StatusOK {
		t.Fatalf("owner GET edit status = %d; body: %s", status, formBody)
	}
	if !strings.Contains(formBody, "Dairy Co") {
		t.Fatalf("edit form missing brand, got: %s", formBody)
	}

	resp, err = owner.PostForm(editURL, url.Values{
		"csrf_token": {token},
		"name":       {""},
		"quantity":   {"2"},
		"expiration": {"2026-12-10"},
	})
	if err != nil {
		t.Fatalf("POST empty name: %v", err)
	}
	bodyBytes, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("empty name status = %d, want 400; body: %s", resp.StatusCode, bodyBytes)
	}

	token, _, status = getCSRF(t, owner, editURL)
	if status != http.StatusOK {
		t.Fatalf("GET edit csrf status = %d", status)
	}
	resp, err = owner.PostForm(editURL, url.Values{
		"csrf_token": {token},
		"name":       {"Milk"},
		"quantity":   {"-1"},
		"expiration": {"2026-12-10"},
	})
	if err != nil {
		t.Fatalf("POST bad qty: %v", err)
	}
	bodyBytes, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("negative qty status = %d, want 400; body: %s", resp.StatusCode, bodyBytes)
	}

	token, _, status = getCSRF(t, owner, editURL)
	if status != http.StatusOK {
		t.Fatalf("GET edit csrf status = %d", status)
	}
	resp, err = owner.PostForm(editURL, url.Values{
		"csrf_token": {token},
		"name":       {"Milk"},
		"quantity":   {"1"},
		"expiration": {"not-a-date"},
	})
	if err != nil {
		t.Fatalf("POST bad date: %v", err)
	}
	bodyBytes, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad date status = %d, want 400; body: %s", resp.StatusCode, bodyBytes)
	}

	token, _, status = getCSRF(t, owner, editURL)
	if status != http.StatusOK {
		t.Fatalf("GET edit csrf status = %d", status)
	}
	resp, err = owner.PostForm(editURL, url.Values{
		"csrf_token": {token},
		"name":       {"Milk"},
		"quantity":   {"1"},
		"expiration": {"2026-12-10"},
		"calories":   {"-5"},
	})
	if err != nil {
		t.Fatalf("POST bad calories: %v", err)
	}
	bodyBytes, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("negative calories status = %d, want 400; body: %s", resp.StatusCode, bodyBytes)
	}

	token, _, status = getCSRF(t, owner, editURL)
	if status != http.StatusOK {
		t.Fatalf("GET edit csrf status = %d", status)
	}
	prev := owner.CheckRedirect
	owner.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
	resp, err = owner.PostForm(editURL, url.Values{
		"csrf_token":   {token},
		"name":         {"Milk 2%"},
		"quantity":     {"2"},
		"expiration":   {"2026-12-10"},
		"barcode":      {"051000012345"},
		"brand":        {"Dairy Co"},
		"serving_size": {"1 cup"},
		"calories":     {"80"},
		"protein_g":    {"8"},
		"carbs_g":      {"12"},
		"fat_g":        {"2.5"},
	})
	owner.CheckRedirect = prev
	if err != nil {
		t.Fatalf("POST save: %v", err)
	}
	bodyBytes, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("POST save status = %d, want 303; body: %s", resp.StatusCode, bodyBytes)
	}

	resp, err = owner.Get(srv.URL + storePath(storeID, "/items/"+strconv.Itoa(itemID)))
	if err != nil {
		t.Fatalf("GET item JSON: %v", err)
	}
	bodyBytes, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET item JSON status = %d; body: %s", resp.StatusCode, bodyBytes)
	}
	var got fridge.Item
	if err := json.Unmarshal(bodyBytes, &got); err != nil {
		t.Fatalf("decode item: %v; body: %s", err, bodyBytes)
	}
	if got.Name != "Milk 2%" || got.Quantity != 2 || got.Barcode != "051000012345" || got.Brand != "Dairy Co" || got.ServingSize != "1 cup" {
		t.Fatalf("updated item = %+v", got)
	}
	if got.Calories == nil || *got.Calories != 80 || got.FatG == nil || *got.FatG != 2.5 {
		t.Fatalf("updated nutrition = %+v", got)
	}
}

func TestUseSoonOrderingHTTP(t *testing.T) {
	srv, db := testServer(t)
	owner := newClient(t, srv)
	signup(t, srv, db, owner, "owner@example.com", "password123")
	storeID := createStore(t, srv, owner, "Pantry")

	today := time.Now()
	add := func(name, exp string) {
		t.Helper()
		addItem(t, srv, owner, storeID, url.Values{
			"name":       {name},
			"quantity":   {"1"},
			"expiration": {exp},
		})
	}
	add("Later", today.AddDate(0, 0, 10).Format(fridge.DateFormat))
	add("Expired", today.AddDate(0, 0, -2).Format(fridge.DateFormat))
	add("Soon", today.AddDate(0, 0, 1).Format(fridge.DateFormat))

	_, body, status := getCSRF(t, owner, srv.URL+storePath(storeID, ""))
	if status != http.StatusOK {
		t.Fatalf("GET store status = %d", status)
	}
	useSoonAt := strings.Index(body, "<h2>Use soon</h2>")
	itemsAt := strings.Index(body, "<h2>Items</h2>")
	if useSoonAt < 0 || itemsAt < 0 || useSoonAt > itemsAt {
		t.Fatalf("expected Use soon section above Items, got: %s", body)
	}
	section := body[useSoonAt:itemsAt]
	expiredAt := strings.Index(section, "Expired")
	soonAt := strings.Index(section, "Soon")
	if expiredAt < 0 || soonAt < 0 || expiredAt > soonAt {
		t.Fatalf("use-soon order want Expired then Soon, got: %s", section)
	}
	if strings.Contains(section, "Later") {
		t.Fatalf("Later should not be in use-soon: %s", section)
	}
	rest := body[itemsAt:]
	if !strings.Contains(rest, "Later") {
		t.Fatalf("Later missing from remaining items: %s", rest)
	}
	if strings.Contains(rest, "Expired") || strings.Contains(rest, ">Soon<") || strings.Contains(rest, "Soon -") {
		t.Fatalf("expired/soon should not be in remaining items: %s", rest)
	}
}
