// CLI app to emulate a refridgerator with SQLite backend
package fridge

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

const DateFormat = "2006-01-02"

var (
	ErrNumArgs = errors.New("incorrect number of arguments")
)

type Fridge struct {
	Items []Item
}

type Item struct {
	ID             int
	Name           string
	Quantity       float64
	ExpirationDate time.Time
	LastChanged    time.Time
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

func (f *Fridge) AddItem(db *sql.DB, name string, quantity float64, expiration time.Time) error {
	insertSQL := "INSERT INTO fridge (name, quantity, expiration_date) VALUES (?, ?, ?)"
	_, err := db.Exec(insertSQL, name, quantity, expiration.Format("2006-01-02"))
	if err != nil {
		return err
	}
	fmt.Printf("inserted %s into fridge\n", name)
	return nil
}

func (f *Fridge) ListItems(db *sql.DB) error {
	rows, err := db.Query("SELECT id, name, quantity, expiration_date, last_changed FROM fridge")
	if err != nil {
		return err
	}
	defer rows.Close()

	fmt.Println("Fridge contents")
	for rows.Next() {
		var item Item
		err := rows.Scan(&item.ID, &item.Name, &item.Quantity, &item.ExpirationDate, &item.LastChanged)
		if err != nil {
			return err
		}

		fmt.Println(item)
	}
	return nil
}

func (f *Fridge) RemoveItem(db *sql.DB, id int) error {
	deleteSQL := "DELETE FROM fridge WHERE id = ?"
	_, err := db.Exec(deleteSQL, id)
	if err != nil {
		return err
	}
	fmt.Println("successfully removed item")
	return nil
}
