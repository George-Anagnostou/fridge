package fridge

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func openTempDB(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fridge.db")
	db, err := InitDB(path)
	if err != nil {
		t.Fatalf("InitDB(%q): %v", path, err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close db: %v", err)
		}
	})
	return db
}

func date(t *testing.T, s string) time.Time {
	t.Helper()
	parsed, err := time.Parse(DateFormat, s)
	if err != nil {
		t.Fatalf("parse date %q: %v", s, err)
	}
	return parsed
}

func TestListItemsEmpty(t *testing.T) {
	db := openTempDB(t)
	f := &Fridge{}

	items, err := f.ListItems(db)
	if err != nil {
		t.Fatalf("ListItems: %v", err)
	}
	if items == nil {
		t.Fatal("ListItems returned nil, want empty slice")
	}
	if len(items) != 0 {
		t.Fatalf("ListItems len = %d, want 0", len(items))
	}
}

func TestItemCRUD(t *testing.T) {
	cases := []struct {
		name     string
		itemName string
		qty      float64
		exp      string
	}{
		{name: "milk", itemName: "Milk", qty: 2, exp: "2026-11-17"},
		{name: "zero quantity", itemName: "Herbs", qty: 0, exp: "2026-11-20"},
		{name: "fractional quantity", itemName: "Flour", qty: 1.5, exp: "2027-01-01"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := openTempDB(t)
			f := &Fridge{}
			exp := date(t, tc.exp)

			id, err := f.AddItem(db, tc.itemName, tc.qty, exp)
			if err != nil {
				t.Fatalf("AddItem: %v", err)
			}
			if id == 0 {
				t.Fatal("AddItem returned id 0")
			}

			got, err := f.GetItemByID(db, id)
			if err != nil {
				t.Fatalf("GetItemByID: %v", err)
			}
			if got.Name != tc.itemName {
				t.Errorf("Name = %q, want %q", got.Name, tc.itemName)
			}
			if got.Quantity != tc.qty {
				t.Errorf("Quantity = %v, want %v", got.Quantity, tc.qty)
			}
			if got.ExpirationDate.Format(DateFormat) != tc.exp {
				t.Errorf("ExpirationDate = %v, want %s", got.ExpirationDate, tc.exp)
			}

			items, err := f.ListItems(db)
			if err != nil {
				t.Fatalf("ListItems: %v", err)
			}
			if len(items) != 1 {
				t.Fatalf("ListItems len = %d, want 1", len(items))
			}
			if items[0].ID != id {
				t.Errorf("ListItems[0].ID = %d, want %d", items[0].ID, id)
			}

			if err := f.RemoveItem(db, id); err != nil {
				t.Fatalf("RemoveItem: %v", err)
			}

			items, err = f.ListItems(db)
			if err != nil {
				t.Fatalf("ListItems after remove: %v", err)
			}
			if items == nil {
				t.Fatal("ListItems after remove returned nil, want empty slice")
			}
			if len(items) != 0 {
				t.Fatalf("ListItems after remove len = %d, want 0", len(items))
			}

			if _, err := f.GetItemByID(db, id); err == nil {
				t.Fatal("GetItemByID after remove: want error, got nil")
			}
		})
	}
}

func TestListItemsMultiple(t *testing.T) {
	db := openTempDB(t)
	f := &Fridge{}

	want := []string{"Milk", "Eggs", "Butter"}
	ids := make([]int, 0, len(want))
	for _, name := range want {
		id, err := f.AddItem(db, name, 1, date(t, "2026-12-01"))
		if err != nil {
			t.Fatalf("AddItem(%q): %v", name, err)
		}
		ids = append(ids, id)
	}

	items, err := f.ListItems(db)
	if err != nil {
		t.Fatalf("ListItems: %v", err)
	}
	if len(items) != len(want) {
		t.Fatalf("ListItems len = %d, want %d", len(items), len(want))
	}

	gotNames := map[string]bool{}
	gotIDs := map[int]bool{}
	for _, item := range items {
		gotNames[item.Name] = true
		gotIDs[item.ID] = true
	}
	for i, name := range want {
		if !gotNames[name] {
			t.Errorf("ListItems missing %q", name)
		}
		if !gotIDs[ids[i]] {
			t.Errorf("ListItems missing id %d", ids[i])
		}
	}

	if err := f.RemoveItem(db, ids[1]); err != nil {
		t.Fatalf("RemoveItem: %v", err)
	}
	items, err = f.ListItems(db)
	if err != nil {
		t.Fatalf("ListItems after remove: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("ListItems after remove len = %d, want 2", len(items))
	}
	for _, item := range items {
		if item.ID == ids[1] || item.Name == "Eggs" {
			t.Fatalf("removed item still present: %+v", item)
		}
	}
}

func TestDBPath(t *testing.T) {
	t.Setenv("FRIDGE_DB", "")
	if got := DBPath(); got != DefaultDBPath {
		t.Errorf("DBPath() with empty FRIDGE_DB = %q, want %q", got, DefaultDBPath)
	}

	t.Setenv("FRIDGE_DB", "/tmp/custom.db")
	if got := DBPath(); got != "/tmp/custom.db" {
		t.Errorf("DBPath() = %q, want %q", got, "/tmp/custom.db")
	}
}
