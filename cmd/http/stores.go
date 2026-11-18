package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/George-Anagnostou/fridge/auth"
	"github.com/George-Anagnostou/fridge/fridge"
	"github.com/George-Anagnostou/fridge/lookup"
)

type storesPage struct {
	CSRF    string
	Email   string
	IsAdmin bool
	Stores  []fridge.StoreListItem
	Error   string
}

type storePage struct {
	CSRF        string
	Email       string
	IsAdmin     bool
	Store       fridge.Store
	CanEdit     bool
	IsOwner     bool
	UseSoon     []RenderItem
	Items       []RenderItem
	UseSoonDays int
	Error       string
	Prefill     itemForm
	LookupMsg   string
}

type itemForm struct {
	Name        string
	Quantity    string
	Barcode     string
	Brand       string
	ServingSize string
	Calories    string
	ProteinG    string
	CarbsG      string
	FatG        string
	OpenDetails bool
}

type membersPage struct {
	CSRF      string
	Email     string
	Store     fridge.Store
	Members   []fridge.Member
	Error     string
	InviteURL string
	Expires   string
	Shared    string
}

func (s *server) lookupAccess(w http.ResponseWriter, r *http.Request) (auth.User, fridge.Access, bool) {
	u, ok := userFrom(r)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return auth.User{}, fridge.Access{}, false
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return auth.User{}, fridge.Access{}, false
	}
	access, err := fridge.StoreAccess(s.db, id, u.ID)
	if err != nil {
		if errors.Is(err, fridge.ErrStoreNotFound) {
			http.NotFound(w, r)
			return auth.User{}, fridge.Access{}, false
		}
		http.Error(w, "Unable to load store", http.StatusInternalServerError)
		log.Println("store access:", err)
		return auth.User{}, fridge.Access{}, false
	}
	return u, access, true
}

func (s *server) requireOwner(next func(http.ResponseWriter, *http.Request, auth.User, fridge.Access)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, access, ok := s.lookupAccess(w, r)
		if !ok {
			return
		}
		if !access.Owner {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		next(w, r, u, access)
	}
}

func (s *server) listStoresHandler(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r)
	stores, err := fridge.ListStoresForUser(s.db, u.ID)
	if err != nil {
		http.Error(w, "Unable to list stores", http.StatusInternalServerError)
		log.Println("list stores:", err)
		return
	}
	s.renderStores(w, storesPage{
		CSRF:    s.csrfToken(r),
		Email:   u.Email,
		IsAdmin: u.IsAdmin,
		Stores:  stores,
	})
}

func (s *server) createStoreHandler(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r)
	store, err := fridge.CreateStore(s.db, u.ID, r.FormValue("name"))
	if err != nil {
		if errors.Is(err, fridge.ErrInvalidStoreName) {
			stores, _ := fridge.ListStoresForUser(s.db, u.ID)
			w.WriteHeader(http.StatusBadRequest)
			s.renderStores(w, storesPage{
				CSRF:    s.csrfToken(r),
				Email:   u.Email,
				IsAdmin: u.IsAdmin,
				Stores:  stores,
				Error:   "Enter a store name.",
			})
			return
		}
		http.Error(w, "Unable to create store", http.StatusInternalServerError)
		log.Println("create store:", err)
		return
	}
	http.Redirect(w, r, "/stores/"+strconv.FormatInt(store.ID, 10), http.StatusSeeOther)
}

func (s *server) storePageHandler(w http.ResponseWriter, r *http.Request) {
	u, access, ok := s.lookupAccess(w, r)
	if !ok {
		return
	}
	if r.Method == http.MethodPost {
		if !access.CanEdit() {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		if err := s.addItemFromForm(r, access.Store.ID); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			s.renderStore(w, r, u, access, itemErrorMessage(err))
			return
		}
		http.Redirect(w, r, "/stores/"+strconv.FormatInt(access.Store.ID, 10), http.StatusSeeOther)
		return
	}
	s.renderStore(w, r, u, access, "")
}

func (s *server) renderStore(w http.ResponseWriter, r *http.Request, u auth.User, access fridge.Access, errMsg string) {
	items, err := s.fridge.ListItems(s.db, access.Store.ID)
	if err != nil {
		http.Error(w, "Unable to get items", http.StatusInternalServerError)
		log.Println("list items:", err)
		return
	}
	useSoon, rest := fridge.PartitionUseSoon(items, time.Now())
	prefill, lookupMsg := s.prefillItemForm(r, access)
	if err := s.tmpl.ExecuteTemplate(w, "store.html", storePage{
		CSRF:        s.csrfToken(r),
		Email:       u.Email,
		IsAdmin:     u.IsAdmin,
		Store:       access.Store,
		CanEdit:     access.CanEdit(),
		IsOwner:     access.Owner,
		UseSoon:     toRenderItems(useSoon),
		Items:       toRenderItems(rest),
		UseSoonDays: fridge.UseSoonDays,
		Error:       errMsg,
		Prefill:     prefill,
		LookupMsg:   lookupMsg,
	}); err != nil {
		http.Error(w, "Unable to render template", http.StatusInternalServerError)
		log.Println("Error rendering template:", err)
	}
}

func (s *server) renameStoreHandler(w http.ResponseWriter, r *http.Request) {
	u, access, ok := s.lookupAccess(w, r)
	if !ok {
		return
	}
	if !access.Owner {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	if err := fridge.RenameStore(s.db, access.Store.ID, u.ID, r.FormValue("name")); err != nil {
		if errors.Is(err, fridge.ErrInvalidStoreName) {
			w.WriteHeader(http.StatusBadRequest)
			s.renderStore(w, r, u, access, "Enter a store name.")
			return
		}
		http.Error(w, "Unable to rename store", http.StatusInternalServerError)
		log.Println("rename store:", err)
		return
	}
	http.Redirect(w, r, "/stores/"+strconv.FormatInt(access.Store.ID, 10), http.StatusSeeOther)
}

func (s *server) deleteStoreHandler(w http.ResponseWriter, r *http.Request) {
	u, access, ok := s.lookupAccess(w, r)
	if !ok {
		return
	}
	if !access.Owner {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	if err := fridge.DeleteStore(s.db, access.Store.ID, u.ID); err != nil {
		http.Error(w, "Unable to delete store", http.StatusInternalServerError)
		log.Println("delete store:", err)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *server) removeStoreItemHandler(w http.ResponseWriter, r *http.Request) {
	_, access, ok := s.lookupAccess(w, r)
	if !ok {
		return
	}
	if !access.CanEdit() {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	id, err := strconv.Atoi(r.FormValue("id"))
	if err != nil {
		http.Error(w, "Unable to get item to remove", http.StatusBadRequest)
		return
	}
	if err := s.fridge.RemoveItem(s.db, access.Store.ID, id); err != nil && !errors.Is(err, fridge.ErrItemNotFound) {
		http.Error(w, "Unable to remove item", http.StatusInternalServerError)
		log.Println("remove item:", err)
		return
	}
	http.Redirect(w, r, "/stores/"+strconv.FormatInt(access.Store.ID, 10), http.StatusSeeOther)
}

func (s *server) getStoreItemsAPI(w http.ResponseWriter, r *http.Request) {
	_, access, ok := s.lookupAccess(w, r)
	if !ok {
		return
	}
	items, err := s.fridge.ListItems(s.db, access.Store.ID)
	if err != nil {
		http.Error(w, "Unable to get items", http.StatusInternalServerError)
		log.Println("list items:", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(items); err != nil {
		http.Error(w, "Failed to encode items to JSON", http.StatusInternalServerError)
	}
}

func (s *server) postStoreItemsAPI(w http.ResponseWriter, r *http.Request) {
	_, access, ok := s.lookupAccess(w, r)
	if !ok {
		return
	}
	if !access.CanEdit() {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	var item fridge.Item
	if err := json.NewDecoder(r.Body).Decode(&item); err != nil {
		http.Error(w, "Invalid JSON data", http.StatusBadRequest)
		return
	}
	if _, err := s.fridge.AddItemFull(s.db, access.Store.ID, item); err != nil {
		if errors.Is(err, fridge.ErrInvalidItemName) || errors.Is(err, fridge.ErrInvalidQuantity) ||
			errors.Is(err, fridge.ErrInvalidExpiration) || errors.Is(err, fridge.ErrInvalidNutrition) {
			http.Error(w, itemErrorMessage(err), http.StatusBadRequest)
			return
		}
		http.Error(w, "Error adding item", http.StatusInternalServerError)
		log.Println("add item:", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"message": "Item added successfully"}`)
}

func (s *server) membersForm(w http.ResponseWriter, r *http.Request, u auth.User, access fridge.Access) {
	s.renderMembers(w, r, u, access, membersPage{})
}

func (s *server) membersShare(w http.ResponseWriter, r *http.Request, u auth.User, access fridge.Access) {
	email := r.FormValue("email")
	perm := r.FormValue("permission")
	if !fridge.ValidPermission(perm) {
		s.renderMembers(w, r, u, access, membersPage{Error: "Choose view or edit permission."})
		return
	}
	existing, _, err := auth.UserByEmail(s.db, email)
	if err == nil {
		if err := fridge.AddMember(s.db, access.Store.ID, u.ID, existing.ID, perm); err != nil {
			msg := "Unable to add that member."
			switch {
			case errors.Is(err, fridge.ErrAlreadyMember):
				msg = "That user is already a member."
			case errors.Is(err, fridge.ErrCannotAddOwner):
				msg = "The owner already has full access."
			}
			s.renderMembers(w, r, u, access, membersPage{Error: msg})
			return
		}
		s.renderMembers(w, r, u, access, membersPage{Shared: existing.Email})
		return
	}
	if !errors.Is(err, auth.ErrUserNotFound) && !errors.Is(err, auth.ErrInvalidEmail) {
		http.Error(w, "Unable to share store", http.StatusInternalServerError)
		log.Println("share lookup:", err)
		return
	}
	if errors.Is(err, auth.ErrInvalidEmail) {
		s.renderMembers(w, r, u, access, membersPage{Error: "Enter a valid email address."})
		return
	}
	id := u.ID
	storeID := access.Store.ID
	token, expiresAt, err := auth.CreateInviteSpec(s.db, auth.InviteSpec{
		TTL:             auth.DefaultInviteTTL,
		CreatedBy:       &id,
		StoreID:         &storeID,
		StorePermission: perm,
	})
	if err != nil {
		http.Error(w, "Unable to create an invite", http.StatusInternalServerError)
		log.Println("store invite:", err)
		return
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	inviteURL := scheme + "://" + r.Host + "/signup?token=" + url.QueryEscape(token)
	s.renderMembers(w, r, u, access, membersPage{
		InviteURL: inviteURL,
		Expires:   expiresAt.UTC().Format(time.RFC3339),
	})
}

func (s *server) membersUpdate(w http.ResponseWriter, r *http.Request, u auth.User, access fridge.Access) {
	userID, err := strconv.ParseInt(r.FormValue("user_id"), 10, 64)
	if err != nil {
		http.Error(w, "Invalid member", http.StatusBadRequest)
		return
	}
	if err := fridge.SetMemberPermission(s.db, access.Store.ID, u.ID, userID, r.FormValue("permission")); err != nil {
		if errors.Is(err, fridge.ErrMemberNotFound) || errors.Is(err, fridge.ErrInvalidPermission) {
			s.renderMembers(w, r, u, access, membersPage{Error: "Unable to update that member."})
			return
		}
		http.Error(w, "Unable to update member", http.StatusInternalServerError)
		log.Println("update member:", err)
		return
	}
	http.Redirect(w, r, "/stores/"+strconv.FormatInt(access.Store.ID, 10)+"/members", http.StatusSeeOther)
}

func (s *server) membersRemove(w http.ResponseWriter, r *http.Request, u auth.User, access fridge.Access) {
	userID, err := strconv.ParseInt(r.FormValue("user_id"), 10, 64)
	if err != nil {
		http.Error(w, "Invalid member", http.StatusBadRequest)
		return
	}
	if err := fridge.RemoveMember(s.db, access.Store.ID, u.ID, userID); err != nil {
		if errors.Is(err, fridge.ErrMemberNotFound) {
			s.renderMembers(w, r, u, access, membersPage{Error: "That member was not found."})
			return
		}
		http.Error(w, "Unable to remove member", http.StatusInternalServerError)
		log.Println("remove member:", err)
		return
	}
	http.Redirect(w, r, "/stores/"+strconv.FormatInt(access.Store.ID, 10)+"/members", http.StatusSeeOther)
}

func (s *server) renderMembers(w http.ResponseWriter, r *http.Request, u auth.User, access fridge.Access, extra membersPage) {
	members, err := fridge.ListMembers(s.db, access.Store.ID, u.ID)
	if err != nil {
		http.Error(w, "Unable to list members", http.StatusInternalServerError)
		log.Println("list members:", err)
		return
	}
	extra.CSRF = s.csrfToken(r)
	extra.Email = u.Email
	extra.Store = access.Store
	extra.Members = members
	if err := s.tmpl.ExecuteTemplate(w, "members.html", extra); err != nil {
		http.Error(w, "Unable to render template", http.StatusInternalServerError)
		log.Println("Error rendering template:", err)
	}
}

func (s *server) renderStores(w http.ResponseWriter, data storesPage) {
	if err := s.tmpl.ExecuteTemplate(w, "index.html", data); err != nil {
		http.Error(w, "Unable to render template", http.StatusInternalServerError)
		log.Println("Error rendering template:", err)
	}
}

func (s *server) prefillItemForm(r *http.Request, access fridge.Access) (itemForm, string) {
	form := itemForm{Quantity: "1"}
	if !access.CanEdit() || r.Method != http.MethodGet {
		return form, ""
	}
	raw := strings.TrimSpace(r.URL.Query().Get("barcode"))
	if raw == "" {
		return form, ""
	}
	form.Barcode = raw
	form.OpenDetails = true
	res, err := s.resolveLookup(r.Context(), raw)
	if err != nil {
		return form, "Lookup failed. You can enter the details yourself."
	}
	if !res.Found {
		if res.Product.Barcode != "" {
			form.Barcode = res.Product.Barcode
		}
		return form, "No product found. You can enter the details yourself."
	}
	form = itemFormFromProduct(res.Product)
	msg := "Found a product."
	if res.Product.Name != "" {
		msg = "Found " + res.Product.Name + "."
	}
	return form, msg
}

func itemFormFromProduct(p lookup.Product) itemForm {
	return itemForm{
		Name:        p.Name,
		Quantity:    "1",
		Barcode:     p.Barcode,
		Brand:       p.Brand,
		ServingSize: p.ServingSize,
		Calories:    formatOptionalFloat(p.Calories),
		ProteinG:    formatOptionalFloat(p.ProteinG),
		CarbsG:      formatOptionalFloat(p.CarbsG),
		FatG:        formatOptionalFloat(p.FatG),
		OpenDetails: true,
	}
}
