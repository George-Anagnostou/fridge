package lookup

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestNewUSDAClientFromEnvDisabledWithoutKey(t *testing.T) {
	t.Setenv("USDA_FDC_API_KEY", "")
	if c := NewUSDAClientFromEnv(); c != nil {
		t.Fatalf("NewUSDAClientFromEnv() = %v, want nil when key is unset", c)
	}
}

func TestUSDAExactGtinMatch(t *testing.T) {
	var hits atomic.Int32
	var sawKey, brandedOnly bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Query().Get("api_key") != "test-key" {
			t.Errorf("api_key = %q", r.URL.Query().Get("api_key"))
		} else {
			sawKey = true
		}
		if r.URL.Path != "/foods/search" {
			t.Errorf("path = %s", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("request json: %v", err)
		}
		if types, _ := req["dataType"].([]any); len(types) == 1 && types[0] == "Branded" {
			brandedOnly = true
		} else {
			t.Errorf("dataType = %#v, want [Branded]", req["dataType"])
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"foods": []any{
				map[string]any{
					"description":     "Wrong Milk",
					"brandName":       "Other",
					"gtinUpc":         "9999999999999",
					"servingSize":     100,
					"servingSizeUnit": "g",
					"foodNutrients": []any{
						map[string]any{"nutrientId": 1008, "nutrientName": "Energy", "unitName": "KCAL", "value": 1},
					},
				},
				map[string]any{
					"description":              "Whole Milk",
					"brandName":                "Dairy Co",
					"gtinUpc":                  "123456789012", // 12 digits, pad to 13
					"householdServingFullText": "1 cup",
					"servingSize":              240,
					"servingSizeUnit":          "ml",
					"foodNutrients": []any{
						map[string]any{"nutrientId": 1008, "nutrientName": "Energy", "unitName": "KCAL", "value": 150},
						map[string]any{"nutrientId": 1003, "nutrientName": "Protein", "unitName": "G", "value": 8},
						map[string]any{"nutrientId": 1005, "nutrientName": "Carbohydrate, by difference", "unitName": "G", "value": 12},
						map[string]any{"nutrientId": 1004, "nutrientName": "Total lipid (fat)", "unitName": "G", "value": 8},
					},
				},
			},
		})
	}))
	t.Cleanup(srv.Close)

	cache := offTestCache(t)
	client := NewUSDAClient(srv.URL, "test-key", srv.Client())
	ctx := context.Background()

	res, err := cache.GetOrFetch(ctx, "0123456789012", SourceUSDA, client)
	if err != nil {
		t.Fatalf("GetOrFetch: %v", err)
	}
	if !res.Found || res.Product.Name != "Whole Milk" || res.Product.Brand != "Dairy Co" {
		t.Fatalf("product = %+v, want exact gtin match (not Wrong Milk)", res.Product)
	}
	if res.Product.ServingSize != "1 cup" || res.Product.Source != SourceUSDA {
		t.Fatalf("serving/source = %+v", res.Product)
	}
	if res.Product.Calories == nil || *res.Product.Calories != 150 {
		t.Fatalf("calories = %v", res.Product.Calories)
	}
	if !sawKey || !brandedOnly {
		t.Fatalf("request missing api_key or Branded filter")
	}
	if hits.Load() != 1 {
		t.Fatalf("hits = %d, want 1", hits.Load())
	}

	res, err = cache.GetOrFetch(ctx, "123456789012", SourceUSDA, client)
	if err != nil || !res.Found || hits.Load() != 1 {
		t.Fatalf("cached hit found=%v hits=%d err=%v", res.Found, hits.Load(), err)
	}
}

func TestUSDAIgnoresNonMatchingGtin(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"foods": []any{
				map[string]any{
					"description": "Unrelated",
					"gtinUpc":     "1111111111111",
					"foodNutrients": []any{
						map[string]any{"nutrientId": 1008, "nutrientName": "Energy", "unitName": "KCAL", "value": 50},
					},
				},
			},
		})
	}))
	t.Cleanup(srv.Close)

	cache := offTestCache(t)
	client := NewUSDAClient(srv.URL, "test-key", srv.Client())
	ctx := context.Background()

	res, err := cache.GetOrFetch(ctx, "12345678", SourceUSDA, client)
	if err != nil {
		t.Fatalf("GetOrFetch: %v", err)
	}
	if res.Found {
		t.Fatalf("found unmatched gtin: %+v", res.Product)
	}
	if hits.Load() != 1 {
		t.Fatalf("hits = %d, want 1", hits.Load())
	}
	res, err = cache.GetOrFetch(ctx, "12345678", SourceUSDA, client)
	if err != nil || res.Found || hits.Load() != 1 {
		t.Fatalf("cached miss found=%v hits=%d err=%v", res.Found, hits.Load(), err)
	}
}

func TestResolverDisabledWithoutUSDAKey(t *testing.T) {
	var usdaHits atomic.Int32
	// A server that must not be called when the client is unset.
	usda := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		usdaHits.Add(1)
		t.Errorf("USDA was called despite a missing key")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"foods":[]}`))
	}))
	t.Cleanup(usda.Close)

	var offHits atomic.Int32
	off := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		offHits.Add(1)
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"status":"failure"}`))
	}))
	t.Cleanup(off.Close)

	t.Setenv("USDA_FDC_API_KEY", "")
	if NewUSDAClientFromEnv() != nil {
		t.Fatal("expected nil USDA client without a key")
	}

	r := &Resolver{
		Cache: offTestCache(t),
		OFF:   NewOFFClient(off.URL, DefaultUserAgent, off.Client()),
		USDA:  NewUSDAClientFromEnv(),
	}
	res, err := r.Lookup(context.Background(), "12345678")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if res.Found {
		t.Fatalf("found = true, want miss when OFF misses and USDA is disabled")
	}
	if offHits.Load() != 1 || usdaHits.Load() != 0 {
		t.Fatalf("off hits=%d usda hits=%d, want 1 and 0", offHits.Load(), usdaHits.Load())
	}
}

func TestResolverOFFFieldsWinAndUSDAFillsGaps(t *testing.T) {
	const code = "1234567890123"
	off := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "success",
			"product": map[string]any{
				"product_name": "OFF Name",
				"brands":       "OFF Brand",
				"serving_size": "30 g",
				// no nutrition: USDA should fill these
			},
		})
	}))
	t.Cleanup(off.Close)

	usda := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"foods": []any{
				map[string]any{
					"description":     "USDA Name",
					"brandName":       "USDA Brand",
					"gtinUpc":         code,
					"servingSize":     100,
					"servingSizeUnit": "g",
					"foodNutrients": []any{
						map[string]any{"nutrientId": 1008, "nutrientName": "Energy", "unitName": "KCAL", "value": 200},
						map[string]any{"nutrientId": 1003, "nutrientName": "Protein", "unitName": "G", "value": 5},
						map[string]any{"nutrientId": 1005, "nutrientName": "Carbohydrate, by difference", "unitName": "G", "value": 20},
						map[string]any{"nutrientId": 1004, "nutrientName": "Total lipid (fat)", "unitName": "G", "value": 2},
					},
				},
			},
		})
	}))
	t.Cleanup(usda.Close)

	r := &Resolver{
		Cache: offTestCache(t),
		OFF:   NewOFFClient(off.URL, DefaultUserAgent, off.Client()),
		USDA:  NewUSDAClient(usda.URL, "test-key", usda.Client()),
	}
	res, err := r.Lookup(context.Background(), code)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if !res.Found {
		t.Fatal("found = false")
	}
	p := res.Product
	if p.Name != "OFF Name" || p.Brand != "OFF Brand" || p.ServingSize != "30 g" {
		t.Fatalf("OFF fields should win: %+v", p)
	}
	if p.Calories == nil || *p.Calories != 200 || p.ProteinG == nil || *p.ProteinG != 5 {
		t.Fatalf("USDA should fill nutrition gaps: %+v", p)
	}
	if p.CarbsG == nil || *p.CarbsG != 20 || p.FatG == nil || *p.FatG != 2 {
		t.Fatalf("USDA should fill carb/fat gaps: %+v", p)
	}
	if p.Source != SourceOFF {
		t.Fatalf("source = %q, want off (OFF was the primary hit)", p.Source)
	}
}

func TestResolverUSDAAloneWhenOFFMisses(t *testing.T) {
	const code = "1234567890123"
	off := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"status":"failure"}`))
	}))
	t.Cleanup(off.Close)

	usda := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"foods": []any{
				map[string]any{
					"description": "USDA Only",
					"brandName":   "Fallback",
					"gtinUpc":     code,
					"foodNutrients": []any{
						map[string]any{"nutrientId": 1008, "nutrientName": "Energy", "unitName": "KCAL", "value": 90},
					},
				},
			},
		})
	}))
	t.Cleanup(usda.Close)

	r := &Resolver{
		Cache: offTestCache(t),
		OFF:   NewOFFClient(off.URL, DefaultUserAgent, off.Client()),
		USDA:  NewUSDAClient(usda.URL, "test-key", usda.Client()),
	}
	res, err := r.Lookup(context.Background(), code)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if !res.Found || res.Product.Name != "USDA Only" || res.Product.Source != SourceUSDA {
		t.Fatalf("product = %+v, want USDA-only hit", res.Product)
	}
}
