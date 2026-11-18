package lookup

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	// DefaultUSDABaseURL is the FoodData Central v1 API root.
	DefaultUSDABaseURL = "https://api.nal.usda.gov/fdc/v1"
	// USDARequestTimeout is the HTTP timeout for a single FDC request.
	USDARequestTimeout = 8 * time.Second
)

func usdaBaseURL() string {
	if base := strings.TrimSpace(os.Getenv("USDA_FDC_BASE_URL")); base != "" {
		return strings.TrimRight(base, "/")
	}
	return DefaultUSDABaseURL
}

// USDAClient looks up branded foods in USDA FoodData Central by GTIN/UPC.
// It is nil-safe to leave unset when no API key is configured.
type USDAClient struct {
	baseURL    string
	apiKey     string
	userAgent  string
	httpClient *http.Client
}

// NewUSDAClientFromEnv returns a USDA client when USDA_FDC_API_KEY is set,
// or nil when the key is unset so the fallback is skipped. Tests should
// construct a client pointed at httptest instead of calling this.
func NewUSDAClientFromEnv() *USDAClient {
	key := strings.TrimSpace(os.Getenv("USDA_FDC_API_KEY"))
	if key == "" {
		return nil
	}
	return NewUSDAClient(usdaBaseURL(), key, nil)
}

// NewUSDAClient builds a USDA client. httpClient may be nil to use a default
// with USDARequestTimeout.
func NewUSDAClient(baseURL, apiKey string, httpClient *http.Client) *USDAClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: USDARequestTimeout}
	}
	return &USDAClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		apiKey:     apiKey,
		userAgent:  DefaultUserAgent,
		httpClient: httpClient,
	}
}

func (c *USDAClient) Lookup(ctx context.Context, barcode string) (Result, error) {
	if c == nil || strings.TrimSpace(c.apiKey) == "" {
		return Result{Found: false, Product: Product{Barcode: barcode}}, nil
	}

	endpoint, err := url.Parse(c.baseURL + "/foods/search")
	if err != nil {
		return Result{}, err
	}
	q := endpoint.Query()
	q.Set("api_key", c.apiKey)
	endpoint.RawQuery = q.Encode()

	payload, err := json.Marshal(map[string]any{
		"query":    barcode,
		"dataType": []string{"Branded"},
		"pageSize": 25,
	})
	if err != nil {
		return Result{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(payload))
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

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
		return parseUSDASearch(barcode, body)
	case resp.StatusCode == http.StatusNotFound:
		return Result{Found: false, Product: Product{Barcode: barcode}}, nil
	case resp.StatusCode == 429 || resp.StatusCode >= 500:
		return Result{}, FetchError{Status: resp.StatusCode, Err: fmt.Errorf("usda fdc HTTP %d", resp.StatusCode)}
	default:
		return Result{}, FetchError{Status: resp.StatusCode, Err: fmt.Errorf("usda fdc HTTP %d", resp.StatusCode)}
	}
}

type usdaSearch struct {
	Foods []usdaFood `json:"foods"`
}

type usdaFood struct {
	Description   string         `json:"description"`
	BrandName     string         `json:"brandName"`
	BrandOwner    string         `json:"brandOwner"`
	GtinUpc       string         `json:"gtinUpc"`
	ServingSize   *float64       `json:"servingSize"`
	ServingUnit   string         `json:"servingSizeUnit"`
	Household     string         `json:"householdServingFullText"`
	FoodNutrients []usdaNutrient `json:"foodNutrients"`
}

type usdaNutrient struct {
	NutrientID   int     `json:"nutrientId"`
	NutrientName string  `json:"nutrientName"`
	UnitName     string  `json:"unitName"`
	Value        float64 `json:"value"`
}

func parseUSDASearch(barcode string, body []byte) (Result, error) {
	var env usdaSearch
	if err := json.Unmarshal(body, &env); err != nil {
		return Result{}, err
	}
	for _, food := range env.Foods {
		if !gtinMatches(barcode, food.GtinUpc) {
			continue
		}
		return Result{Found: true, Product: mapUSDAFood(barcode, food)}, nil
	}
	return Result{Found: false, Product: Product{Barcode: barcode}}, nil
}

func gtinMatches(want, got string) bool {
	a, err := NormalizeBarcode(want)
	if err != nil {
		return false
	}
	b, err := NormalizeBarcode(got)
	if err != nil {
		return false
	}
	return a == b
}

func mapUSDAFood(barcode string, food usdaFood) Product {
	p := Product{
		Barcode: barcode,
		Source:  SourceUSDA,
		Name:    strings.TrimSpace(food.Description),
		Brand:   firstNonEmpty(food.BrandName, food.BrandOwner),
	}
	p.ServingSize = usdaServing(food)
	p.Calories = usdaNutrientValue(food.FoodNutrients, 1008, "Energy", "KCAL")
	p.ProteinG = usdaNutrientValue(food.FoodNutrients, 1003, "Protein", "G")
	p.CarbsG = usdaNutrientValue(food.FoodNutrients, 1005, "Carbohydrate, by difference", "G")
	p.FatG = usdaNutrientValue(food.FoodNutrients, 1004, "Total lipid (fat)", "G")
	return p
}

func usdaServing(food usdaFood) string {
	if s := strings.TrimSpace(food.Household); s != "" {
		return s
	}
	if food.ServingSize == nil {
		return ""
	}
	unit := strings.TrimSpace(food.ServingUnit)
	return strings.TrimSpace(fmt.Sprintf("%g %s", *food.ServingSize, unit))
}

func usdaNutrientValue(nutrients []usdaNutrient, id int, name, unit string) *float64 {
	unit = strings.ToUpper(unit)
	name = strings.ToLower(name)
	for _, n := range nutrients {
		if n.NutrientID == id && (unit == "" || strings.EqualFold(n.UnitName, unit)) {
			v := n.Value
			return &v
		}
	}
	for _, n := range nutrients {
		if strings.EqualFold(n.UnitName, unit) && strings.ToLower(n.NutrientName) == name {
			v := n.Value
			return &v
		}
	}
	return nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}
