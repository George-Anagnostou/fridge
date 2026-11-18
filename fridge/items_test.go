package fridge

import (
	"errors"
	"testing"
)

func floatPtr(v float64) *float64 { return &v }

func TestValidateItem(t *testing.T) {
	ok := Item{Name: "Milk", Quantity: 1, ExpirationDate: date(t, "2026-12-01")}
	if err := ValidateItem(ok); err != nil {
		t.Fatalf("valid item: %v", err)
	}
	if err := ValidateItem(Item{Name: "  ", Quantity: 1, ExpirationDate: date(t, "2026-12-01")}); !errors.Is(err, ErrInvalidItemName) {
		t.Fatalf("empty name = %v, want ErrInvalidItemName", err)
	}
	if err := ValidateItem(Item{Name: "Milk", Quantity: -1, ExpirationDate: date(t, "2026-12-01")}); !errors.Is(err, ErrInvalidQuantity) {
		t.Fatalf("negative qty = %v, want ErrInvalidQuantity", err)
	}
	if err := ValidateItem(Item{Name: "Milk", Quantity: 1}); !errors.Is(err, ErrInvalidExpiration) {
		t.Fatalf("zero expiry = %v, want ErrInvalidExpiration", err)
	}
	bad := ok
	bad.Calories = floatPtr(-1)
	if err := ValidateItem(bad); !errors.Is(err, ErrInvalidNutrition) {
		t.Fatalf("negative calories = %v, want ErrInvalidNutrition", err)
	}
	ok.Quantity = 0
	ok.Calories = floatPtr(0)
	if err := ValidateItem(ok); err != nil {
		t.Fatalf("zero qty and calories should be allowed: %v", err)
	}
}

func TestAddItemAgainCreatesNewRow(t *testing.T) {
	db := openTempDB(t)
	_, storeID := seedUserAndStore(t, db)
	f := &Fridge{}
	exp := date(t, "2026-12-01")
	item := Item{Name: "Milk", Quantity: 1, ExpirationDate: exp, Barcode: "01234567", Brand: "Dairy Co"}
	id1, err := f.AddItemFull(db, storeID, item)
	if err != nil {
		t.Fatalf("add 1: %v", err)
	}
	id2, err := f.AddItemFull(db, storeID, item)
	if err != nil {
		t.Fatalf("add 2: %v", err)
	}
	if id1 == id2 {
		t.Fatalf("second add reused id %d", id1)
	}
	items, err := f.ListItems(db, storeID)
	if err != nil {
		t.Fatalf("ListItems: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("ListItems len = %d, want 2", len(items))
	}
}

func TestUpdateItemAndFields(t *testing.T) {
	db := openTempDB(t)
	_, storeID := seedUserAndStore(t, db)
	f := &Fridge{}
	id, err := f.AddItem(db, storeID, "Milk", 1, date(t, "2026-12-01"))
	if err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	updated := Item{
		ID:             id,
		Name:           "Milk 2%",
		Quantity:       2,
		ExpirationDate: date(t, "2026-12-10"),
		Barcode:        "051000012345",
		Brand:          "Dairy Co",
		ServingSize:    "1 cup",
		Calories:       floatPtr(80),
		ProteinG:       floatPtr(8),
		CarbsG:         floatPtr(12),
		FatG:           floatPtr(2.5),
	}
	if err := f.UpdateItem(db, storeID, updated); err != nil {
		t.Fatalf("UpdateItem: %v", err)
	}
	got, err := f.GetItemByID(db, storeID, id)
	if err != nil {
		t.Fatalf("GetItemByID: %v", err)
	}
	if got.Name != "Milk 2%" || got.Quantity != 2 || got.ExpirationDate.Format(DateFormat) != "2026-12-10" {
		t.Fatalf("core fields = %+v", got)
	}
	if got.Barcode != "051000012345" || got.Brand != "Dairy Co" || got.ServingSize != "1 cup" {
		t.Fatalf("text fields = %+v", got)
	}
	if got.Calories == nil || *got.Calories != 80 || got.ProteinG == nil || *got.ProteinG != 8 ||
		got.CarbsG == nil || *got.CarbsG != 12 || got.FatG == nil || *got.FatG != 2.5 {
		t.Fatalf("nutrition = %+v", got)
	}

	if err := f.UpdateItem(db, storeID+99, updated); !errors.Is(err, ErrItemNotFound) {
		t.Fatalf("update other store = %v, want ErrItemNotFound", err)
	}
}

func TestPartitionUseSoon(t *testing.T) {
	now := date(t, "2026-11-16")
	items := []Item{
		{ID: 1, Name: "Later", ExpirationDate: now.AddDate(0, 0, 10)},
		{ID: 2, Name: "Expired yesterday", ExpirationDate: now.AddDate(0, 0, -1)},
		{ID: 3, Name: "Today", ExpirationDate: now},
		{ID: 4, Name: "In 3 days", ExpirationDate: now.AddDate(0, 0, UseSoonDays)},
		{ID: 5, Name: "In 4 days", ExpirationDate: now.AddDate(0, 0, UseSoonDays+1)},
		{ID: 6, Name: "Expired earlier", ExpirationDate: now.AddDate(0, 0, -5)},
	}
	useSoon, rest := PartitionUseSoon(items, now)
	wantSoon := []string{"Expired earlier", "Expired yesterday", "Today", "In 3 days"}
	if len(useSoon) != len(wantSoon) {
		t.Fatalf("use-soon = %v, want %v", names(useSoon), wantSoon)
	}
	for i, name := range wantSoon {
		if useSoon[i].Name != name {
			t.Fatalf("use-soon[%d] = %q, want %q (got %v)", i, useSoon[i].Name, name, names(useSoon))
		}
	}
	wantRest := []string{"In 4 days", "Later"}
	if len(rest) != len(wantRest) {
		t.Fatalf("rest = %v, want %v", names(rest), wantRest)
	}
	for i, name := range wantRest {
		if rest[i].Name != name {
			t.Fatalf("rest[%d] = %q, want %q (got %v)", i, rest[i].Name, name, names(rest))
		}
	}
}

func names(items []Item) []string {
	out := make([]string, len(items))
	for i, item := range items {
		out[i] = item.Name
	}
	return out
}

func TestAddItemRejectsInvalid(t *testing.T) {
	db := openTempDB(t)
	_, storeID := seedUserAndStore(t, db)
	f := &Fridge{}
	if _, err := f.AddItem(db, storeID, "", 1, date(t, "2026-12-01")); !errors.Is(err, ErrInvalidItemName) {
		t.Fatalf("AddItem empty name = %v, want ErrInvalidItemName", err)
	}
}
