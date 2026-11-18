package lookup

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/George-Anagnostou/fridge/fridge"
)

func offTestCache(t *testing.T) *Cache {
	t.Helper()
	db, err := fridge.InitDB(filepath.Join(t.TempDir(), "fridge.db"))
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return NewCache(db)
}

func TestOFFUserAgentIncludesContact(t *testing.T) {
	t.Setenv("OFF_CONTACT", "fridge@example.com")
	got := OFFUserAgent()
	want := DefaultUserAgent + " ; fridge@example.com"
	if got != want {
		t.Fatalf("OFFUserAgent() = %q, want %q", got, want)
	}
}

func TestOFFLookupHTTP(t *testing.T) {
	const code = "1234567890123"
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path != "/api/v3/product/"+code {
			t.Errorf("path = %s, want /api/v3/product/%s", r.URL.Path, code)
		}
		if ua := r.Header.Get("User-Agent"); ua != DefaultUserAgent {
			t.Errorf("User-Agent = %q, want %q", ua, DefaultUserAgent)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "success",
			"product": map[string]any{
				"product_name":    "Test Milk",
				"brands":          "Dairy Co, Other",
				"serving_size":    "240 ml",
				"expiration_date": "2026-01-01",
				"nutriments": map[string]any{
					"energy-kcal_serving":   150.0,
					"proteins_serving":      8.0,
					"carbohydrates_serving": 12.0,
					"fat_serving":           8.0,
					"energy-kcal_100g":      63.0,
				},
			},
		})
	}))
	t.Cleanup(srv.Close)

	cache := offTestCache(t)
	client := NewOFFClient(srv.URL, DefaultUserAgent, srv.Client())
	ctx := context.Background()

	res, err := cache.GetOrFetch(ctx, code, SourceOFF, client)
	if err != nil {
		t.Fatalf("first fetch: %v", err)
	}
	if !res.Found || res.Product.Name != "Test Milk" || res.Product.Brand != "Dairy Co" {
		t.Fatalf("product = %+v", res.Product)
	}
	if res.Product.ServingSize != "240 ml" || res.Product.Source != SourceOFF {
		t.Fatalf("serving/source = %+v", res.Product)
	}
	if res.Product.Calories == nil || *res.Product.Calories != 150 {
		t.Fatalf("calories = %v, want 150 (per serving, not 100g)", res.Product.Calories)
	}
	if hits.Load() != 1 {
		t.Fatalf("hits after first fetch = %d, want 1", hits.Load())
	}

	res, err = cache.GetOrFetch(ctx, code, SourceOFF, client)
	if err != nil {
		t.Fatalf("cached hit: %v", err)
	}
	if !res.Found || res.Product.Name != "Test Milk" || hits.Load() != 1 {
		t.Fatalf("cached hit = %+v hits=%d", res, hits.Load())
	}
}

func TestOFFLookupPer100gFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "success",
			"product": map[string]any{
				"product_name": "Oats",
				"nutriments": map[string]any{
					"energy-kcal_100g":   389.0,
					"proteins_100g":      16.9,
					"carbohydrates_100g": 66.3,
					"fat_100g":           6.9,
				},
			},
		})
	}))
	t.Cleanup(srv.Close)

	client := NewOFFClient(srv.URL, DefaultUserAgent, srv.Client())
	res, err := client.Lookup(context.Background(), "12345678")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if !res.Found || res.Product.ServingSize != "per 100g" {
		t.Fatalf("product = %+v, want serving size labeled per 100g", res.Product)
	}
	if res.Product.Calories == nil || *res.Product.Calories != 389 {
		t.Fatalf("calories = %v, want 389", res.Product.Calories)
	}
}

func TestOFFLookup404CachedMiss(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.Error(w, `{"status":"failure","result":{"id":"product_not_found"}}`, http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	cache := offTestCache(t)
	client := NewOFFClient(srv.URL, DefaultUserAgent, srv.Client())
	ctx := context.Background()

	res, err := cache.GetOrFetch(ctx, "00000001", SourceOFF, client)
	if err != nil {
		t.Fatalf("first miss: %v", err)
	}
	if res.Found {
		t.Fatalf("found = true, want cached miss")
	}
	if hits.Load() != 1 {
		t.Fatalf("hits = %d, want 1", hits.Load())
	}

	res, err = cache.GetOrFetch(ctx, "00000001", SourceOFF, client)
	if err != nil {
		t.Fatalf("cached miss: %v", err)
	}
	if res.Found || hits.Load() != 1 {
		t.Fatalf("second call found=%v hits=%d, want cached miss", res.Found, hits.Load())
	}
}

func TestOFFLookupUncacheableStatus(t *testing.T) {
	for _, status := range []int{429, 500, 503} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var hits atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				w.WriteHeader(status)
			}))
			t.Cleanup(srv.Close)

			cache := offTestCache(t)
			client := NewOFFClient(srv.URL, DefaultUserAgent, srv.Client())
			ctx := context.Background()

			_, err := cache.GetOrFetch(ctx, "12345678", SourceOFF, client)
			if !Uncacheable(err) {
				t.Fatalf("first GetOrFetch = %v, want uncacheable %d", err, status)
			}
			if _, ok, err := cache.Get(ctx, "12345678", SourceOFF); err != nil || ok {
				t.Fatalf("cache after %d: ok=%v err=%v, want miss", status, ok, err)
			}
			_, err = cache.GetOrFetch(ctx, "12345678", SourceOFF, client)
			if !Uncacheable(err) || hits.Load() != 2 {
				t.Fatalf("second GetOrFetch hits=%d err=%v, want a second fetch", hits.Load(), err)
			}
		})
	}
}

func TestOFFBaseURLFromEnv(t *testing.T) {
	t.Setenv("OFF_BASE_URL", "https://off.example.test/")
	if got := offBaseURL(); got != "https://off.example.test" {
		t.Fatalf("offBaseURL() = %q", got)
	}
	t.Setenv("OFF_BASE_URL", "")
	if got := offBaseURL(); got != DefaultOFFBaseURL {
		t.Fatalf("default offBaseURL() = %q", got)
	}
}
