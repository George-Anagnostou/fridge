package lookup

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

const (
	SourceOFF  = "off"
	SourceUSDA = "usda"

	// CacheHitTTL is how long a found lookup stays valid.
	CacheHitTTL = 14 * 24 * time.Hour
	// CacheMissTTL is how long a not-found lookup stays valid.
	CacheMissTTL = 24 * time.Hour
)

// Product is the cached/normalized product payload returned by lookup.
type Product struct {
	Barcode     string   `json:"barcode"`
	Name        string   `json:"name,omitempty"`
	Brand       string   `json:"brand,omitempty"`
	ServingSize string   `json:"serving_size,omitempty"`
	Calories    *float64 `json:"calories,omitempty"`
	ProteinG    *float64 `json:"protein_g,omitempty"`
	CarbsG      *float64 `json:"carbs_g,omitempty"`
	FatG        *float64 `json:"fat_g,omitempty"`
}

// Result is a cacheable lookup outcome. Found is false for a definitive miss.
type Result struct {
	Found   bool    `json:"found"`
	Product Product `json:"product,omitempty"`
}

// Source looks up a normalized barcode in an upstream catalog.
type Source interface {
	Lookup(ctx context.Context, barcode string) (Result, error)
}

// FetchError is returned by a Source for an upstream HTTP failure.
// Status 429 and 5xx must not be written to the cache.
type FetchError struct {
	Status int
	Err    error
}

func (e FetchError) Error() string {
	if e.Err != nil {
		return e.Err.Error()
	}
	return "lookup fetch failed"
}

func (e FetchError) Unwrap() error { return e.Err }

func Uncacheable(err error) bool {
	var fe FetchError
	if !errors.As(err, &fe) {
		return false
	}
	return fe.Status == 429 || fe.Status >= 500
}

// Cache stores lookup results in SQLite.
type Cache struct {
	db  *sql.DB
	now func() time.Time
}

func NewCache(db *sql.DB) *Cache {
	return &Cache{db: db, now: func() time.Time { return time.Now().UTC() }}
}

func (c *Cache) currentTime() time.Time {
	if c.now != nil {
		return c.now().UTC()
	}
	return time.Now().UTC()
}

// Get returns a still-valid cache entry for the already-normalized barcode.
// ok is false on a miss or expired row.
func (c *Cache) Get(_ context.Context, barcode, source string) (Result, bool, error) {
	var payload sql.NullString
	var foundInt int
	var fetchedAt string
	err := c.db.QueryRow(
		`SELECT payload, found, fetched_at FROM lookup_cache WHERE barcode = ? AND source = ?`,
		barcode, source,
	).Scan(&payload, &foundInt, &fetchedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Result{}, false, nil
	}
	if err != nil {
		return Result{}, false, err
	}
	fetched, err := time.Parse(time.RFC3339, fetchedAt)
	if err != nil {
		return Result{}, false, err
	}
	ttl := CacheMissTTL
	if foundInt != 0 {
		ttl = CacheHitTTL
	}
	if !fetched.Add(ttl).After(c.currentTime()) {
		return Result{}, false, nil
	}
	res := Result{Found: foundInt != 0}
	if res.Found && payload.Valid && payload.String != "" {
		if err := json.Unmarshal([]byte(payload.String), &res.Product); err != nil {
			return Result{}, false, err
		}
	}
	return res, true, nil
}

func (c *Cache) put(barcode, source string, res Result) error {
	var payload any
	if res.Found {
		res.Product.Barcode = barcode
		b, err := json.Marshal(res.Product)
		if err != nil {
			return err
		}
		payload = string(b)
	}
	found := 0
	if res.Found {
		found = 1
	}
	_, err := c.db.Exec(
		`INSERT INTO lookup_cache (barcode, source, payload, found, fetched_at)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT (barcode, source) DO UPDATE SET
			payload = excluded.payload,
			found = excluded.found,
			fetched_at = excluded.fetched_at`,
		barcode, source, payload, found, c.currentTime().Format(time.RFC3339),
	)
	return err
}

// GetOrFetch returns a cached result if one is still valid, otherwise it
// calls src and stores the result. 429 and 5xx responses are not cached.
func (c *Cache) GetOrFetch(ctx context.Context, barcode, source string, src Source) (Result, error) {
	norm, err := NormalizeBarcode(barcode)
	if err != nil {
		return Result{}, err
	}
	if cached, ok, err := c.Get(ctx, norm, source); err != nil {
		return Result{}, err
	} else if ok {
		return cached, nil
	}
	res, err := src.Lookup(ctx, norm)
	if err != nil {
		return Result{}, err
	}
	res.Product.Barcode = norm
	if err := c.put(norm, source, res); err != nil {
		return Result{}, err
	}
	return res, nil
}

// GetCached walks sources in order and returns the first still-valid found
// entry, or a cached miss if every source has a valid miss. ok is false
// when nothing usable is in the cache.
func (c *Cache) GetCached(ctx context.Context, barcode string, sources ...string) (Result, bool, error) {
	norm, err := NormalizeBarcode(barcode)
	if err != nil {
		return Result{}, false, err
	}
	if len(sources) == 0 {
		sources = []string{SourceOFF, SourceUSDA}
	}
	allMiss := true
	for _, source := range sources {
		res, ok, err := c.Get(ctx, norm, source)
		if err != nil {
			return Result{}, false, err
		}
		if !ok {
			allMiss = false
			continue
		}
		if res.Found {
			if res.Product.Barcode == "" {
				res.Product.Barcode = norm
			}
			return res, true, nil
		}
	}
	if allMiss {
		return Result{Found: false, Product: Product{Barcode: norm}}, true, nil
	}
	return Result{Found: false, Product: Product{Barcode: norm}}, false, nil
}
