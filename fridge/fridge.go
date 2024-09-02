// CLI app to emulate a refridgerator with SQLite backend
package fridge

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
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
}

func (i Item) String() string {
	return fmt.Sprintf(`-------------------------------------
ID:                  %03d
Name:                %s
Quantity:            %.2f
Expiration Date:     %s
Last Changed:        %s
-------------------------------------`, i.ID, i.Name, i.Quantity, i.ExpirationDate.Format(DateFormat), i.LastChanged.Format(DateFormat))
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

func (f *Fridge) AddItem(db *sql.DB, name string, quantity float64, expiration time.Time) (int, error) {
	insertSQL := "INSERT INTO fridge (name, quantity, expiration_date) VALUES (?, ?, ?)"
	result, err := db.Exec(insertSQL, name, quantity, expiration.Format("2006-01-02"))
	if err != nil {
		return 0, err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}

	return int(id), nil
}

func (f *Fridge) ListItems(db *sql.DB) ([]Item, error) {
	rows, err := db.Query("SELECT id, name, quantity, expiration_date, last_changed FROM fridge")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []Item
	for rows.Next() {
		var item Item
		err := rows.Scan(&item.ID, &item.Name, &item.Quantity, &item.ExpirationDate, &item.LastChanged)
		if err != nil {
			return nil, err
		}

		items = append(items, item)
	}
	return items, nil
}

func (f *Fridge) GetItemByID(db *sql.DB, id int) (Item, error) {
	row := db.QueryRow("SELECT id, name, quantity, expiration_date, last_changed FROM fridge WHERE id = ?", id)

	var item Item
	err := row.Scan(&item.ID, &item.Name, &item.Quantity, &item.ExpirationDate, &item.LastChanged)
	if err != nil {
		return item, err
	}

	return item, nil
}

func (f *Fridge) RemoveItem(db *sql.DB, id int) error {
	deleteSQL := "DELETE FROM fridge WHERE id = ?"
	_, err := db.Exec(deleteSQL, id)
	if err != nil {
		return err
	}
	return nil
}
