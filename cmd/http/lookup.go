package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/George-Anagnostou/fridge/lookup"
)

func (s *server) lookupHandler(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.Query().Get("barcode")
	cache := lookup.NewCache(s.db)
	res, ok, err := cache.GetCached(r.Context(), raw)
	if errors.Is(err, lookup.ErrInvalidBarcode) {
		http.Error(w, "Invalid barcode", http.StatusBadRequest)
		return
	}
	if err != nil {
		http.Error(w, "Unable to look up barcode", http.StatusInternalServerError)
		log.Println("lookup:", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if !ok || !res.Found {
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
