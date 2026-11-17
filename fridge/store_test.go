package fridge

import (
	"database/sql"
	"errors"
	"testing"
)

func addUserSQL(t *testing.T, db *sql.DB, email string, admin bool) int64 {
	t.Helper()
	adminVal := 0
	if admin {
		adminVal = 1
	}
	res, err := db.Exec(`INSERT INTO users (email, password_hash, is_admin) VALUES (?, ?, ?)`, email, "x", adminVal)
	if err != nil {
		t.Fatalf("insert user %s: %v", email, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("user id: %v", err)
	}
	return id
}

func TestStoreIsolationAndPermissions(t *testing.T) {
	db := openTempDB(t)
	ownerA := addUserSQL(t, db, "a@example.com", false)
	ownerB := addUserSQL(t, db, "b@example.com", false)
	viewer := addUserSQL(t, db, "view@example.com", false)
	editor := addUserSQL(t, db, "edit@example.com", false)

	storeA, err := CreateStore(db, ownerA, "A's store")
	if err != nil {
		t.Fatalf("CreateStore A: %v", err)
	}
	storeB, err := CreateStore(db, ownerB, "B's store")
	if err != nil {
		t.Fatalf("CreateStore B: %v", err)
	}

	f := &Fridge{}
	idA, err := f.AddItem(db, storeA.ID, "Milk", 1, date(t, "2026-12-01"))
	if err != nil {
		t.Fatalf("AddItem A: %v", err)
	}
	if _, err := f.AddItem(db, storeB.ID, "Eggs", 6, date(t, "2026-12-02")); err != nil {
		t.Fatalf("AddItem B: %v", err)
	}

	itemsA, err := f.ListItems(db, storeA.ID)
	if err != nil {
		t.Fatalf("ListItems A: %v", err)
	}
	if len(itemsA) != 1 || itemsA[0].Name != "Milk" {
		t.Fatalf("store A items = %+v, want only Milk", itemsA)
	}
	itemsB, err := f.ListItems(db, storeB.ID)
	if err != nil {
		t.Fatalf("ListItems B: %v", err)
	}
	if len(itemsB) != 1 || itemsB[0].Name != "Eggs" {
		t.Fatalf("store B items = %+v, want only Eggs", itemsB)
	}
	if _, err := f.GetItemByID(db, storeB.ID, idA); !errors.Is(err, ErrItemNotFound) {
		t.Fatalf("GetItemByID from other store = %v, want ErrItemNotFound", err)
	}

	if _, err := StoreAccess(db, storeA.ID, ownerB); !errors.Is(err, ErrStoreNotFound) {
		t.Fatalf("owner B access to A = %v, want ErrStoreNotFound", err)
	}

	if err := AddMember(db, storeA.ID, ownerA, viewer, PermView); err != nil {
		t.Fatalf("AddMember view: %v", err)
	}
	if err := AddMember(db, storeA.ID, ownerA, editor, PermEdit); err != nil {
		t.Fatalf("AddMember edit: %v", err)
	}
	if err := AddMember(db, storeA.ID, ownerA, ownerA, PermEdit); !errors.Is(err, ErrCannotAddOwner) {
		t.Fatalf("AddMember(owner) = %v, want ErrCannotAddOwner", err)
	}

	viewAcc, err := StoreAccess(db, storeA.ID, viewer)
	if err != nil || !viewAcc.CanRead() || viewAcc.CanEdit() || viewAcc.Owner {
		t.Fatalf("viewer access = %+v, %v", viewAcc, err)
	}
	editAcc, err := StoreAccess(db, storeA.ID, editor)
	if err != nil || !editAcc.CanEdit() || editAcc.Owner {
		t.Fatalf("editor access = %+v, %v", editAcc, err)
	}

	if err := RenameStore(db, storeA.ID, viewer, "Hacked"); !errors.Is(err, ErrNotStoreOwner) {
		t.Fatalf("viewer rename = %v, want ErrNotStoreOwner", err)
	}
	if err := RenameStore(db, storeA.ID, editor, "Hacked"); !errors.Is(err, ErrNotStoreOwner) {
		t.Fatalf("editor rename = %v, want ErrNotStoreOwner", err)
	}
	if err := AddMember(db, storeA.ID, editor, ownerB, PermView); !errors.Is(err, ErrNotStoreOwner) {
		t.Fatalf("editor add member = %v, want ErrNotStoreOwner", err)
	}
	if err := SetMemberPermission(db, storeA.ID, viewer, editor, PermView); !errors.Is(err, ErrNotStoreOwner) {
		t.Fatalf("viewer set permission = %v, want ErrNotStoreOwner", err)
	}
	if err := RemoveMember(db, storeA.ID, editor, viewer); !errors.Is(err, ErrNotStoreOwner) {
		t.Fatalf("editor remove member = %v, want ErrNotStoreOwner", err)
	}

	if err := SetMemberPermission(db, storeA.ID, ownerA, viewer, PermEdit); err != nil {
		t.Fatalf("owner set permission: %v", err)
	}
	viewAcc, err = StoreAccess(db, storeA.ID, viewer)
	if err != nil || !viewAcc.CanEdit() {
		t.Fatalf("after promotion viewer access = %+v, %v", viewAcc, err)
	}
	if err := SetMemberPermission(db, storeA.ID, ownerA, viewer, PermView); err != nil {
		t.Fatalf("owner demote: %v", err)
	}
	if err := RemoveMember(db, storeA.ID, ownerA, viewer); err != nil {
		t.Fatalf("owner remove member: %v", err)
	}
	if _, err := StoreAccess(db, storeA.ID, viewer); !errors.Is(err, ErrStoreNotFound) {
		t.Fatalf("removed viewer access = %v, want ErrStoreNotFound", err)
	}

	if err := RenameStore(db, storeA.ID, ownerA, "A renamed"); err != nil {
		t.Fatalf("owner rename: %v", err)
	}
	got, err := StoreByID(db, storeA.ID)
	if err != nil || got.Name != "A renamed" {
		t.Fatalf("renamed store = %+v, %v", got, err)
	}

	listed, err := ListStoresForUser(db, ownerA)
	if err != nil || len(listed) != 1 || listed[0].Permission != "owner" {
		t.Fatalf("ListStoresForUser(owner A) = %+v, %v", listed, err)
	}
	listed, err = ListStoresForUser(db, editor)
	if err != nil || len(listed) != 1 || listed[0].ID != storeA.ID || listed[0].Permission != PermEdit {
		t.Fatalf("ListStoresForUser(editor) = %+v, %v", listed, err)
	}
	listed, err = ListStoresForUser(db, ownerB)
	if err != nil || len(listed) != 1 || listed[0].ID != storeB.ID {
		t.Fatalf("ListStoresForUser(owner B) = %+v, %v", listed, err)
	}
}
