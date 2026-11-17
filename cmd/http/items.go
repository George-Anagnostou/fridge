package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/George-Anagnostou/fridge/auth"
	"github.com/George-Anagnostou/fridge/fridge"
)

type itemFormPage struct {
	CSRF  string
	Email string
	Store fridge.Store
	Item  RenderItem
	Error string
}

func itemFromForm(r *http.Request) (fridge.Item, error) {
	item := fridge.Item{
		Name:        r.FormValue("name"),
		Barcode:     r.FormValue("barcode"),
		Brand:       r.FormValue("brand"),
		ServingSize: r.FormValue("serving_size"),
	}
	qty, err := strconv.ParseFloat(r.FormValue("quantity"), 64)
	if err != nil {
		return fridge.Item{}, fridge.ErrInvalidQuantity
	}
	item.Quantity = qty
	exp, err := time.Parse(fridge.DateFormat, r.FormValue("expiration"))
	if err != nil {
		return fridge.Item{}, fridge.ErrInvalidExpiration
	}
	item.ExpirationDate = exp
	for _, field := range []struct {
		name string
		dst  **float64
	}{
		{"calories", &item.Calories},
		{"protein_g", &item.ProteinG},
		{"carbs_g", &item.CarbsG},
		{"fat_g", &item.FatG},
	} {
		v, err := optionalFormFloat(r.FormValue(field.name))
		if err != nil {
			return fridge.Item{}, fridge.ErrInvalidNutrition
		}
		*field.dst = v
	}
	return item, fridge.ValidateItem(item)
}

func optionalFormFloat(raw string) (*float64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return nil, err
	}
	return &v, nil
}

func (s *server) addItemFromForm(r *http.Request, storeID int64) error {
	item, err := itemFromForm(r)
	if err != nil {
		return err
	}
	_, err = s.fridge.AddItemFull(s.db, storeID, item)
	return err
}

func (s *server) editItemForm(w http.ResponseWriter, r *http.Request) {
	u, access, item, ok := s.lookupEditableItem(w, r)
	if !ok {
		return
	}
	s.renderItemForm(w, r, u, access, item, "")
}

func (s *server) editItemSubmit(w http.ResponseWriter, r *http.Request) {
	u, access, existing, ok := s.lookupEditableItem(w, r)
	if !ok {
		return
	}
	item, err := itemFromForm(r)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		s.renderItemForm(w, r, u, access, existing, itemErrorMessage(err))
		return
	}
	item.ID = existing.ID
	if err := s.fridge.UpdateItem(s.db, access.Store.ID, item); err != nil {
		if errors.Is(err, fridge.ErrItemNotFound) {
			http.NotFound(w, r)
			return
		}
		if errors.Is(err, fridge.ErrInvalidItemName) || errors.Is(err, fridge.ErrInvalidQuantity) ||
			errors.Is(err, fridge.ErrInvalidExpiration) || errors.Is(err, fridge.ErrInvalidNutrition) {
			w.WriteHeader(http.StatusBadRequest)
			s.renderItemForm(w, r, u, access, item, itemErrorMessage(err))
			return
		}
		http.Error(w, "Unable to update item", http.StatusInternalServerError)
		log.Println("update item:", err)
		return
	}
	http.Redirect(w, r, "/stores/"+strconv.FormatInt(access.Store.ID, 10), http.StatusSeeOther)
}

func itemErrorMessage(err error) string {
	switch {
	case errors.Is(err, fridge.ErrInvalidItemName):
		return "Enter an item name."
	case errors.Is(err, fridge.ErrInvalidQuantity):
		return "Quantity must be zero or greater."
	case errors.Is(err, fridge.ErrInvalidExpiration):
		return "Enter a valid expiration date."
	case errors.Is(err, fridge.ErrInvalidNutrition):
		return "Nutrition values must be zero or greater."
	default:
		return "Could not save that item."
	}
}

func (s *server) lookupEditableItem(w http.ResponseWriter, r *http.Request) (auth.User, fridge.Access, fridge.Item, bool) {
	u, access, ok := s.lookupAccess(w, r)
	if !ok {
		return auth.User{}, fridge.Access{}, fridge.Item{}, false
	}
	if !access.CanEdit() {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return auth.User{}, fridge.Access{}, fridge.Item{}, false
	}
	itemID, err := strconv.Atoi(r.PathValue("itemID"))
	if err != nil || itemID <= 0 {
		http.NotFound(w, r)
		return auth.User{}, fridge.Access{}, fridge.Item{}, false
	}
	item, err := s.fridge.GetItemByID(s.db, access.Store.ID, itemID)
	if err != nil {
		if errors.Is(err, fridge.ErrItemNotFound) {
			http.NotFound(w, r)
			return auth.User{}, fridge.Access{}, fridge.Item{}, false
		}
		http.Error(w, "Unable to load item", http.StatusInternalServerError)
		log.Println("get item:", err)
		return auth.User{}, fridge.Access{}, fridge.Item{}, false
	}
	return u, access, item, true
}

func (s *server) renderItemForm(w http.ResponseWriter, r *http.Request, u auth.User, access fridge.Access, item fridge.Item, errMsg string) {
	if err := s.tmpl.ExecuteTemplate(w, "item_edit.html", itemFormPage{
		CSRF:  s.csrfToken(r),
		Email: u.Email,
		Store: access.Store,
		Item:  toRenderItem(item),
		Error: errMsg,
	}); err != nil {
		http.Error(w, "Unable to render template", http.StatusInternalServerError)
		log.Println("Error rendering template:", err)
	}
}

func (s *server) getStoreItemAPI(w http.ResponseWriter, r *http.Request) {
	_, access, ok := s.lookupAccess(w, r)
	if !ok {
		return
	}
	itemID, err := strconv.Atoi(r.PathValue("itemID"))
	if err != nil || itemID <= 0 {
		http.NotFound(w, r)
		return
	}
	item, err := s.fridge.GetItemByID(s.db, access.Store.ID, itemID)
	if err != nil {
		if errors.Is(err, fridge.ErrItemNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "Unable to get item", http.StatusInternalServerError)
		log.Println("get item:", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(item); err != nil {
		http.Error(w, "Failed to encode item to JSON", http.StatusInternalServerError)
	}
}

func (s *server) postStoreItemAPI(w http.ResponseWriter, r *http.Request) {
	_, access, existing, ok := s.lookupEditableItem(w, r)
	if !ok {
		return
	}
	var item fridge.Item
	if err := json.NewDecoder(r.Body).Decode(&item); err != nil {
		http.Error(w, "Invalid JSON data", http.StatusBadRequest)
		return
	}
	item.ID = existing.ID
	if err := s.fridge.UpdateItem(s.db, access.Store.ID, item); err != nil {
		if errors.Is(err, fridge.ErrInvalidItemName) || errors.Is(err, fridge.ErrInvalidQuantity) ||
			errors.Is(err, fridge.ErrInvalidExpiration) || errors.Is(err, fridge.ErrInvalidNutrition) {
			http.Error(w, itemErrorMessage(err), http.StatusBadRequest)
			return
		}
		if errors.Is(err, fridge.ErrItemNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "Error updating item", http.StatusInternalServerError)
		log.Println("update item:", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"message": "Item updated successfully"}`)
}
