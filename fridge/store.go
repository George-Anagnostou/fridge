package fridge

import (
	"database/sql"
	"errors"
	"strings"
)

const (
	PermView = "view"
	PermEdit = "edit"
)

var (
	ErrStoreNotFound     = errors.New("store not found")
	ErrItemNotFound      = errors.New("item not found")
	ErrNotStoreOwner     = errors.New("not the store owner")
	ErrInvalidPermission = errors.New("permission must be view or edit")
	ErrInvalidStoreName  = errors.New("store name is required")
	ErrAlreadyMember     = errors.New("user is already a member of this store")
	ErrCannotAddOwner    = errors.New("the owner already has full access")
	ErrMemberNotFound    = errors.New("member not found")
	ErrForbidden         = errors.New("forbidden")
)

type Store struct {
	ID        int64
	Name      string
	OwnerID   int64
	CreatedAt string
}

// StoreListItem is a store the user can access, plus their permission.
// Permission is "owner", "edit", or "view".
type StoreListItem struct {
	Store
	Permission string
}

type Member struct {
	UserID     int64
	Email      string
	Permission string
}

type Access struct {
	Store Store
	Owner bool
	Perm  string
}

func (a Access) CanRead() bool {
	return a.Owner || a.Perm == PermView || a.Perm == PermEdit
}

func (a Access) CanEdit() bool {
	return a.Owner || a.Perm == PermEdit
}

func ValidPermission(p string) bool {
	return p == PermView || p == PermEdit
}

func CreateStore(db *sql.DB, ownerID int64, name string) (Store, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Store{}, ErrInvalidStoreName
	}
	res, err := db.Exec(`INSERT INTO stores (name, owner_id) VALUES (?, ?)`, name, ownerID)
	if err != nil {
		return Store{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Store{}, err
	}
	return StoreByID(db, id)
}

func RenameStore(db *sql.DB, storeID, actorID int64, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return ErrInvalidStoreName
	}
	access, err := StoreAccess(db, storeID, actorID)
	if err != nil {
		return err
	}
	if !access.Owner {
		return ErrNotStoreOwner
	}
	_, err = db.Exec(`UPDATE stores SET name = ? WHERE id = ?`, name, storeID)
	return err
}

func DeleteStore(db *sql.DB, storeID, actorID int64) error {
	access, err := StoreAccess(db, storeID, actorID)
	if err != nil {
		return err
	}
	if !access.Owner {
		return ErrNotStoreOwner
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM fridge WHERE store_id = ?`, storeID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM invites WHERE store_id = ? AND used_at IS NULL`, storeID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM store_members WHERE store_id = ?`, storeID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM stores WHERE id = ?`, storeID); err != nil {
		return err
	}
	return tx.Commit()
}

func StoreByID(db *sql.DB, id int64) (Store, error) {
	var s Store
	err := db.QueryRow(`SELECT id, name, owner_id, created_at FROM stores WHERE id = ?`, id).
		Scan(&s.ID, &s.Name, &s.OwnerID, &s.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Store{}, ErrStoreNotFound
	}
	return s, err
}

func ListStoresForUser(db *sql.DB, userID int64) ([]StoreListItem, error) {
	rows, err := db.Query(`
		SELECT s.id, s.name, s.owner_id, s.created_at,
			CASE WHEN s.owner_id = ? THEN 'owner' ELSE m.permission END
		FROM stores s
		LEFT JOIN store_members m ON m.store_id = s.id AND m.user_id = ?
		WHERE s.owner_id = ? OR m.user_id = ?
		ORDER BY s.name COLLATE NOCASE, s.id`,
		userID, userID, userID, userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []StoreListItem{}
	for rows.Next() {
		var item StoreListItem
		if err := rows.Scan(&item.ID, &item.Name, &item.OwnerID, &item.CreatedAt, &item.Permission); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// StoreAccess returns the user's access to a store. Outsiders and unknown
// IDs both return ErrStoreNotFound so callers can 404.
func StoreAccess(db *sql.DB, storeID, userID int64) (Access, error) {
	s, err := StoreByID(db, storeID)
	if err != nil {
		return Access{}, err
	}
	if s.OwnerID == userID {
		return Access{Store: s, Owner: true, Perm: PermEdit}, nil
	}
	var perm string
	err = db.QueryRow(
		`SELECT permission FROM store_members WHERE store_id = ? AND user_id = ?`,
		storeID, userID,
	).Scan(&perm)
	if errors.Is(err, sql.ErrNoRows) {
		return Access{}, ErrStoreNotFound
	}
	if err != nil {
		return Access{}, err
	}
	return Access{Store: s, Owner: false, Perm: perm}, nil
}

func ListMembers(db *sql.DB, storeID, actorID int64) ([]Member, error) {
	access, err := StoreAccess(db, storeID, actorID)
	if err != nil {
		return nil, err
	}
	if !access.Owner {
		return nil, ErrNotStoreOwner
	}
	rows, err := db.Query(`
		SELECT m.user_id, u.email, m.permission
		FROM store_members m
		JOIN users u ON u.id = m.user_id
		WHERE m.store_id = ?
		ORDER BY u.email COLLATE NOCASE`, storeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Member{}
	for rows.Next() {
		var m Member
		if err := rows.Scan(&m.UserID, &m.Email, &m.Permission); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func AddMember(db *sql.DB, storeID, actorID, userID int64, permission string) error {
	if !ValidPermission(permission) {
		return ErrInvalidPermission
	}
	access, err := StoreAccess(db, storeID, actorID)
	if err != nil {
		return err
	}
	if !access.Owner {
		return ErrNotStoreOwner
	}
	if userID == access.Store.OwnerID {
		return ErrCannotAddOwner
	}
	_, err = db.Exec(
		`INSERT INTO store_members (store_id, user_id, permission) VALUES (?, ?, ?)`,
		storeID, userID, permission,
	)
	if err != nil {
		if isUniqueErr(err) {
			return ErrAlreadyMember
		}
		return err
	}
	return nil
}

func SetMemberPermission(db *sql.DB, storeID, actorID, userID int64, permission string) error {
	if !ValidPermission(permission) {
		return ErrInvalidPermission
	}
	access, err := StoreAccess(db, storeID, actorID)
	if err != nil {
		return err
	}
	if !access.Owner {
		return ErrNotStoreOwner
	}
	res, err := db.Exec(
		`UPDATE store_members SET permission = ? WHERE store_id = ? AND user_id = ?`,
		permission, storeID, userID,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrMemberNotFound
	}
	return nil
}

func RemoveMember(db *sql.DB, storeID, actorID, userID int64) error {
	access, err := StoreAccess(db, storeID, actorID)
	if err != nil {
		return err
	}
	if !access.Owner {
		return ErrNotStoreOwner
	}
	res, err := db.Exec(`DELETE FROM store_members WHERE store_id = ? AND user_id = ?`, storeID, userID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrMemberNotFound
	}
	return nil
}

func isUniqueErr(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "unique")
}
