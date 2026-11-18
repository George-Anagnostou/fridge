package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/George-Anagnostou/fridge/lookup"
)

func (s *server) resolveLookup(ctx context.Context, raw string) (lookup.Result, error) {
	r := &lookup.Resolver{
		Cache: lookup.NewCache(s.db),
		OFF:   s.off,
		USDA:  s.usda,
	}
	return r.Lookup(ctx, raw)
}

func (s *server) lookupHandler(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.Query().Get("barcode")
	res, err := s.resolveLookup(r.Context(), raw)
	if errors.Is(err, lookup.ErrInvalidBarcode) {
		http.Error(w, "Invalid barcode", http.StatusBadRequest)
		return
	}
	if err != nil {
		status := http.StatusBadGateway
		var fe lookup.FetchError
		if errors.As(err, &fe) && fe.Status == 429 {
			status = http.StatusTooManyRequests
		}
		http.Error(w, "Unable to look up barcode", status)
		log.Println("lookup:", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if !res.Found {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"found":   false,
			"barcode": res.Product.Barcode,
		})
		return
	}
	if err := json.NewEncoder(w).Encode(res.Product); err != nil {
		http.Error(w, "Failed to encode lookup result", http.StatusInternalServerError)
	}
}
