package lookup

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/George-Anagnostou/fridge/fridge"
)

func testCache(t *testing.T) (*Cache, *time.Time) {
	t.Helper()
	db, err := fridge.InitDB(filepath.Join(t.TempDir(), "fridge.db"))
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	now := time.Date(2026, 11, 16, 15, 0, 0, 0, time.UTC)
	clock := now
	c := NewCache(db)
	c.now = func() time.Time { return clock }
	return c, &clock
}

type fakeSource struct {
	calls int
	fn    func(barcode string) (Result, error)
}

func (f *fakeSource) Lookup(_ context.Context, barcode string) (Result, error) {
	f.calls++
	if f.fn != nil {
		return f.fn(barcode)
	}
	return Result{}, errors.New("no stub")
}

func TestCacheHitMissAndExpiry(t *testing.T) {
	c, clock := testCache(t)
	ctx := context.Background()
	src := &fakeSource{fn: func(barcode string) (Result, error) {
		return Result{Found: true, Product: Product{Name: "Milk", Brand: "Dairy Co", Barcode: barcode}}, nil
	}}

	res, err := c.GetOrFetch(ctx, "12345678", SourceOFF, src)
	if err != nil {
		t.Fatalf("first fetch: %v", err)
	}
	if !res.Found || res.Product.Name != "Milk" || res.Product.Barcode != "12345678" {
		t.Fatalf("first result = %+v", res)
	}
	if src.calls != 1 {
		t.Fatalf("calls after first fetch = %d, want 1", src.calls)
	}

	res, err = c.GetOrFetch(ctx, "1234-5678", SourceOFF, src)
	if err != nil {
		t.Fatalf("cached hit: %v", err)
	}
	if !res.Found || res.Product.Name != "Milk" || src.calls != 1 {
		t.Fatalf("hit = %+v calls=%d, want cached Milk", res, src.calls)
	}

	*clock = clock.Add(CacheHitTTL + time.Second)
	res, err = c.GetOrFetch(ctx, "12345678", SourceOFF, src)
	if err != nil {
		t.Fatalf("expired refetch: %v", err)
	}
	if src.calls != 2 {
		t.Fatalf("calls after expiry = %d, want 2", src.calls)
	}
	if !res.Found || res.Product.Name != "Milk" {
		t.Fatalf("refetch result = %+v", res)
	}
}

func TestCacheMissTTL(t *testing.T) {
	c, clock := testCache(t)
	ctx := context.Background()
	src := &fakeSource{fn: func(string) (Result, error) {
		return Result{Found: false}, nil
	}}

	res, err := c.GetOrFetch(ctx, "00000001", SourceUSDA, src)
	if err != nil {
		t.Fatalf("miss fetch: %v", err)
	}
	if res.Found || src.calls != 1 {
		t.Fatalf("miss = %+v calls=%d", res, src.calls)
	}
	res, err = c.GetOrFetch(ctx, "1", SourceUSDA, src)
	if err != nil {
		t.Fatalf("cached miss: %v", err)
	}
	if res.Found || src.calls != 1 {
		t.Fatalf("cached miss calls=%d result=%+v", src.calls, res)
	}

	*clock = clock.Add(CacheMissTTL + time.Second)
	if _, err := c.GetOrFetch(ctx, "00000001", SourceUSDA, src); err != nil {
		t.Fatalf("expired miss refetch: %v", err)
	}
	if src.calls != 2 {
		t.Fatalf("calls after miss expiry = %d, want 2", src.calls)
	}
}

func TestCacheDoesNotStoreUncacheableErrors(t *testing.T) {
	c, _ := testCache(t)
	ctx := context.Background()

	for _, status := range []int{429, 500, 503} {
		t.Run(itStatus(status), func(t *testing.T) {
			src := &fakeSource{fn: func(string) (Result, error) {
				return Result{}, FetchError{Status: status}
			}}
			_, err := c.GetOrFetch(ctx, "12345678", SourceOFF, src)
			if !Uncacheable(err) {
				t.Fatalf("GetOrFetch = %v, want uncacheable status %d", err, status)
			}
			if src.calls != 1 {
				t.Fatalf("calls = %d, want 1", src.calls)
			}
			if _, ok, err := c.Get(ctx, "12345678", SourceOFF); err != nil || ok {
				t.Fatalf("cache after %d: ok=%v err=%v, want miss", status, ok, err)
			}
			_, err = c.GetOrFetch(ctx, "12345678", SourceOFF, src)
			if !Uncacheable(err) || src.calls != 2 {
				t.Fatalf("second GetOrFetch calls=%d err=%v, want a second fetch", src.calls, err)
			}
		})
	}
}

func itStatus(n int) string {
	if n == 429 {
		return "429"
	}
	if n == 500 {
		return "500"
	}
	return "503"
}
