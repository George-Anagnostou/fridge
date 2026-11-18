package lookup

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultOFFBaseURL is the public Open Food Facts API origin.
	DefaultOFFBaseURL = "https://world.openfoodfacts.org"
	// DefaultUserAgent identifies this app to Open Food Facts. Set OFF_CONTACT
	// to append a contact address, as OFF requests.
	DefaultUserAgent = "Fridge/0.1 (https://github.com/George-Anagnostou/fridge)"
	// OFFRateLimit is the maximum number of OFF requests allowed per minute
	// (under the 15/min guideline).
	OFFRateLimit = 14
	// OFFRequestTimeout is the HTTP timeout for a single OFF request.
	OFFRequestTimeout = 8 * time.Second
)

// OFFUserAgent returns the default User-Agent, with " ; OFF_CONTACT" appended
// when that environment variable is set.
func OFFUserAgent() string {
	ua := DefaultUserAgent
	if contact := strings.TrimSpace(os.Getenv("OFF_CONTACT")); contact != "" {
		ua += " ; " + contact
	}
	return ua
}

func offBaseURL() string {
	if base := strings.TrimSpace(os.Getenv("OFF_BASE_URL")); base != "" {
		return strings.TrimRight(base, "/")
	}
	return DefaultOFFBaseURL
}

// OFFClient looks up barcodes with the Open Food Facts v3 product API.
type OFFClient struct {
	baseURL    string
	userAgent  string
	httpClient *http.Client
	limiter    *rateLimiter
}

// NewOFFClientFromEnv builds a production OFF client from OFF_BASE_URL and
// OFF_CONTACT. Tests should construct a client pointed at httptest instead.
func NewOFFClientFromEnv() *OFFClient {
	return NewOFFClient(offBaseURL(), OFFUserAgent(), nil)
}

// NewOFFClient builds an OFF client. httpClient may be nil to use a default
// with OFFRequestTimeout. The rate limiter is always applied.
func NewOFFClient(baseURL, userAgent string, httpClient *http.Client) *OFFClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: OFFRequestTimeout}
	}
	if userAgent == "" {
		userAgent = DefaultUserAgent
	}
	return &OFFClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		userAgent:  userAgent,
		httpClient: httpClient,
		limiter:    newRateLimiter(OFFRateLimit, time.Minute),
	}
}

func (c *OFFClient) Lookup(ctx context.Context, barcode string) (Result, error) {
	if err := c.limiter.Wait(ctx); err != nil {
		return Result{}, err
	}
	rawURL := c.baseURL + "/api/v3/product/" + url.PathEscape(barcode)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Result{}, err
	}

	switch {
	case resp.StatusCode == http.StatusOK:
		return parseOFFProduct(barcode, body)
	case resp.StatusCode == http.StatusNotFound:
		return Result{Found: false, Product: Product{Barcode: barcode}}, nil
	case resp.StatusCode == 429 || resp.StatusCode >= 500:
		return Result{}, FetchError{Status: resp.StatusCode, Err: fmt.Errorf("open food facts HTTP %d", resp.StatusCode)}
	default:
		return Result{}, FetchError{Status: resp.StatusCode, Err: fmt.Errorf("open food facts HTTP %d", resp.StatusCode)}
	}
}

type offResponse struct {
	Status  string          `json:"status"`
	Product json.RawMessage `json:"product"`
}

func parseOFFProduct(barcode string, body []byte) (Result, error) {
	var env offResponse
	if err := json.Unmarshal(body, &env); err != nil {
		return Result{}, err
	}
	status := strings.ToLower(env.Status)
	if status == "failure" || status == "fail" || len(env.Product) == 0 || string(env.Product) == "null" {
		return Result{Found: false, Product: Product{Barcode: barcode}}, nil
	}
	var raw map[string]any
	if err := json.Unmarshal(env.Product, &raw); err != nil {
		return Result{}, err
	}
	prod := mapOFFProduct(barcode, raw)
	if prod.Name == "" && prod.Brand == "" && prod.ServingSize == "" &&
		prod.Calories == nil && prod.ProteinG == nil && prod.CarbsG == nil && prod.FatG == nil {
		return Result{Found: false, Product: Product{Barcode: barcode}}, nil
	}
	return Result{Found: true, Product: prod}, nil
}

func mapOFFProduct(barcode string, raw map[string]any) Product {
	p := Product{
		Barcode:     barcode,
		Source:      SourceOFF,
		Name:        firstString(raw, "product_name", "product_name_en", "abbreviated_product_name", "generic_name"),
		Brand:       firstBrand(raw),
		ServingSize: firstString(raw, "serving_size"),
	}
	// expiration_date (and similar OFF date fields) are intentionally ignored.

	nutriments, _ := raw["nutriments"].(map[string]any)
	servingCals := nutrientValue(nutriments, "energy-kcal_serving")
	servingProt := nutrientValue(nutriments, "proteins_serving")
	servingCarb := nutrientValue(nutriments, "carbohydrates_serving")
	servingFat := nutrientValue(nutriments, "fat_serving")
	hasServing := servingCals != nil || servingProt != nil || servingCarb != nil || servingFat != nil

	if hasServing {
		p.Calories, p.ProteinG, p.CarbsG, p.FatG = servingCals, servingProt, servingCarb, servingFat
		return p
	}

	per100Cals := nutrientValue(nutriments, "energy-kcal_100g", "energy-kcal")
	per100Prot := nutrientValue(nutriments, "proteins_100g", "proteins")
	per100Carb := nutrientValue(nutriments, "carbohydrates_100g", "carbohydrates")
	per100Fat := nutrientValue(nutriments, "fat_100g", "fat")
	if per100Cals != nil || per100Prot != nil || per100Carb != nil || per100Fat != nil {
		// Label the nutrition as per 100g so it is not mistaken for a serving.
		p.ServingSize = "per 100g"
		p.Calories, p.ProteinG, p.CarbsG, p.FatG = per100Cals, per100Prot, per100Carb, per100Fat
	}
	return p
}

func firstBrand(raw map[string]any) string {
	brands := firstString(raw, "brands")
	if brands == "" {
		return ""
	}
	parts := strings.Split(brands, ",")
	return strings.TrimSpace(parts[0])
}

func firstString(raw map[string]any, keys ...string) string {
	for _, k := range keys {
		if s, ok := raw[k].(string); ok {
			if s = strings.TrimSpace(s); s != "" {
				return s
			}
		}
	}
	return ""
}

func nutrientValue(nutriments map[string]any, keys ...string) *float64 {
	if nutriments == nil {
		return nil
	}
	for _, k := range keys {
		if v, ok := asFloat(nutriments[k]); ok {
			return &v
		}
	}
	return nil
}

func asFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	case string:
		var f float64
		if _, err := fmt.Sscan(strings.TrimSpace(n), &f); err == nil {
			return f, true
		}
	}
	return 0, false
}

type rateLimiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	times  []time.Time
	now    func() time.Time
}

func newRateLimiter(max int, window time.Duration) *rateLimiter {
	return &rateLimiter{max: max, window: window, now: time.Now}
}

func (l *rateLimiter) Wait(ctx context.Context) error {
	if l == nil || l.max <= 0 {
		return ctx.Err()
	}
	for {
		l.mu.Lock()
		now := l.now()
		cut := now.Add(-l.window)
		kept := l.times[:0]
		for _, t := range l.times {
			if t.After(cut) {
				kept = append(kept, t)
			}
		}
		l.times = kept
		if len(l.times) < l.max {
			l.times = append(l.times, now)
			l.mu.Unlock()
			return nil
		}
		wait := l.times[0].Add(l.window).Sub(now)
		l.mu.Unlock()
		if wait < 0 {
			wait = 0
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
