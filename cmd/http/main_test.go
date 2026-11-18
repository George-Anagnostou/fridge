package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/George-Anagnostou/fridge/fridge"
)

func testServer(t *testing.T) (*httptest.Server, *fridge.Fridge) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fridge.db")
	db, err := fridge.InitDB(path)
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	srv := httptest.NewServer(newMux(db))
	t.Cleanup(srv.Close)
	return srv, &fridge.Fridge{}
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

	srv, _ := testServer(t)
	client := srv.Client()
	client.Timeout = 2 * time.Second

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
	srv, _ := testServer(t)
	client := srv.Client()
	client.Timeout = 2 * time.Second

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
		// Accept compact or newline-terminated empty arrays, but not null.
		if strings.TrimSpace(string(body)) == "null" {
			t.Fatalf("GET /items body = %q, want []", body)
		}
		if len(items) != 0 {
			t.Fatalf("GET /items len = %d, want 0; body: %s", len(items), body)
		}
	}
}

func TestRemoveInvalidDoesNotHang(t *testing.T) {
	srv, _ := testServer(t)
	client := srv.Client()
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
	client.Timeout = 2 * time.Second

	postRemove := func(id string) *http.Response {
		t.Helper()
		resp, err := client.PostForm(srv.URL+"/remove", url.Values{"id": {id}})
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
	// Missing ids still attempt the delete and redirect; the important part is
	// that this returns instead of hanging from a leftover mutex lock.
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

	resp, err = client.PostForm(srv.URL+"/", url.Values{
		"name":       {"Milk"},
		"quantity":   {"1"},
		"expiration": {"2026-11-17"},
	})
	if err != nil {
		t.Fatalf("POST / add after failed removes: %v", err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST / add status = %d, want 200; body: %s", resp.StatusCode, body)
	}
}
