// CLI app to emulate a refridgerator with SQLite backend
package fridge

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

const DateFormat = "2006-01-02"

var (
	ErrNumArgs = errors.New("incorrect number of arguments")
)

type Fridge struct {
	sync.Mutex
	Items []Item
}

type Item struct {
	ID             int       `json:"id"`
	Name           string    `json:"name"`
	Quantity       float64   `json:"quantity"`
	ExpirationDate time.Time `json:"expiration_date"`
	LastChanged    time.Time `json:"last_changed"`
	Barcode        string    `json:"barcode,omitempty"`
	Brand          string    `json:"brand,omitempty"`
	ServingSize    string    `json:"serving_size,omitempty"`
	Calories       *float64  `json:"calories,omitempty"`
	ProteinG       *float64  `json:"protein_g,omitempty"`
	CarbsG         *float64  `json:"carbs_g,omitempty"`
	FatG           *float64  `json:"fat_g,omitempty"`
}

func (i Item) String() string {
	return fmt.Sprintf(`
-------------------------------------
ID:                  %03d
Name:                %s
Quantity:            %.2f
Expiration Date:     %s
Last Changed:        %s
-------------------------------------`, i.ID, i.Name, i.Quantity, i.ExpirationDate.Format(DateFormat), i.LastChanged.Format(DateFormat))
}

// LogString returns a structured string specifically for logging
func (i Item) LogString() string {
	return fmt.Sprintf("Item[ID=%03d, Name=%s, Quantity=%.2f, ExpirationDate=%s, LastChanged=%s]", i.ID, i.Name, i.Quantity, i.ExpirationDate, i.LastChanged)
}

func indentedString(item Item, indent string) string {
	// Get the default string representation
	str := item.String()

	// Split the string into lines
	lines := strings.Split(str, "\n")

	// Prepend the indent to each line
	for i, line := range lines {
		lines[i] = indent + line
	}

	// Join the lines back together
	return strings.Join(lines, "\n")
}

func (f *Fridge) AddItem(db *sql.DB, storeID int64, name string, quantity float64, expiration time.Time) (int, error) {
	return f.AddItemFull(db, storeID, Item{Name: name, Quantity: quantity, ExpirationDate: expiration})
}

func (f *Fridge) ListItems(db *sql.DB, storeID int64) ([]Item, error) {
	rows, err := db.Query("SELECT "+itemColumns+" FROM fridge WHERE store_id = ? ORDER BY expiration_date ASC, id ASC", storeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []Item{}
	for rows.Next() {
		item, err := scanItem(rows.Scan)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (f *Fridge) GetItemByID(db *sql.DB, storeID int64, id int) (Item, error) {
	item, err := scanItem(db.QueryRow("SELECT "+itemColumns+" FROM fridge WHERE store_id = ? AND id = ?", storeID, id).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return Item{}, ErrItemNotFound
	}
	if err != nil {
		return Item{}, err
	}
	return item, nil
}

func (f *Fridge) RemoveItem(db *sql.DB, storeID int64, id int) error {
	res, err := db.Exec("DELETE FROM fridge WHERE store_id = ? AND id = ?", storeID, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrItemNotFound
	}
	return nil
}
