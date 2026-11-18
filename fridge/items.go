package fridge

import (
	"database/sql"
	"errors"
	"math"
	"strings"
	"time"
)

// UseSoonDays is the number of days ahead (including today) an item
// counts as "use soon". Expired items are listed first in that section.
const UseSoonDays = 3

var (
	ErrInvalidItemName   = errors.New("item name is required")
	ErrInvalidQuantity   = errors.New("quantity must be zero or greater")
	ErrInvalidExpiration = errors.New("expiration date is required")
	ErrInvalidNutrition  = errors.New("nutrition values must be zero or greater")
)

func ValidateItem(item Item) error {
	if strings.TrimSpace(item.Name) == "" {
		return ErrInvalidItemName
	}
	if math.IsNaN(item.Quantity) || math.IsInf(item.Quantity, 0) || item.Quantity < 0 {
		return ErrInvalidQuantity
	}
	if item.ExpirationDate.IsZero() {
		return ErrInvalidExpiration
	}
	for _, v := range []*float64{item.Calories, item.ProteinG, item.CarbsG, item.FatG} {
		if v == nil {
			continue
		}
		if math.IsNaN(*v) || math.IsInf(*v, 0) || *v < 0 {
			return ErrInvalidNutrition
		}
	}
	return nil
}

func itemDate(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// IsUseSoon reports whether exp is already past or falls within UseSoonDays
// of now, comparing calendar dates.
func IsUseSoon(exp, now time.Time) bool {
	if exp.IsZero() {
		return false
	}
	today := itemDate(now)
	day := itemDate(exp)
	return !day.After(today.AddDate(0, 0, UseSoonDays))
}

func IsExpired(exp, now time.Time) bool {
	if exp.IsZero() {
		return false
	}
	return itemDate(exp).Before(itemDate(now))
}

// PartitionUseSoon splits items into a use-soon list (expired first, then
// those expiring within UseSoonDays, both by date then id) and the rest
// (by date then id).
func PartitionUseSoon(items []Item, now time.Time) (useSoon, rest []Item) {
	useSoon = []Item{}
	rest = []Item{}
	for _, item := range items {
		if IsUseSoon(item.ExpirationDate, now) {
			useSoon = append(useSoon, item)
		} else {
			rest = append(rest, item)
		}
	}
	sortItemsByDate(useSoon)
	sortItemsByDate(rest)
	return useSoon, rest
}

func sortItemsByDate(items []Item) {
	for i := 1; i < len(items); i++ {
		for j := i; j > 0; j-- {
			if lessItem(items[j], items[j-1]) {
				items[j], items[j-1] = items[j-1], items[j]
			}
		}
	}
}

func lessItem(a, b Item) bool {
	da, db := itemDate(a.ExpirationDate), itemDate(b.ExpirationDate)
	if !da.Equal(db) {
		return da.Before(db)
	}
	return a.ID < b.ID
}

const itemColumns = `id, name, quantity, expiration_date, last_changed, barcode, brand, serving_size, calories, protein_g, carbs_g, fat_g`

func scanItem(scan func(dest ...any) error) (Item, error) {
	var item Item
	var barcode, brand, serving sql.NullString
	var calories, protein, carbs, fat sql.NullFloat64
	err := scan(
		&item.ID,
		&item.Name,
		&item.Quantity,
		&item.ExpirationDate,
		&item.LastChanged,
		&barcode,
		&brand,
		&serving,
		&calories,
		&protein,
		&carbs,
		&fat,
	)
	if err != nil {
		return Item{}, err
	}
	item.Barcode = barcode.String
	item.Brand = brand.String
	item.ServingSize = serving.String
	item.Calories = nullFloatPtr(calories)
	item.ProteinG = nullFloatPtr(protein)
	item.CarbsG = nullFloatPtr(carbs)
	item.FatG = nullFloatPtr(fat)
	return item, nil
}

func nullFloatPtr(n sql.NullFloat64) *float64 {
	if !n.Valid {
		return nil
	}
	v := n.Float64
	return &v
}

func nullableString(s string) any {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return s
}

func nullableFloat(v *float64) any {
	if v == nil {
		return nil
	}
	return *v
}

func (f *Fridge) AddItemFull(db *sql.DB, storeID int64, item Item) (int, error) {
	item.Name = strings.TrimSpace(item.Name)
	item.Barcode = strings.TrimSpace(item.Barcode)
	item.Brand = strings.TrimSpace(item.Brand)
	item.ServingSize = strings.TrimSpace(item.ServingSize)
	if err := ValidateItem(item); err != nil {
		return 0, err
	}
	res, err := db.Exec(
		`INSERT INTO fridge (store_id, name, quantity, expiration_date, barcode, brand, serving_size, calories, protein_g, carbs_g, fat_g)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		storeID,
		item.Name,
		item.Quantity,
		item.ExpirationDate.Format(DateFormat),
		nullableString(item.Barcode),
		nullableString(item.Brand),
		nullableString(item.ServingSize),
		nullableFloat(item.Calories),
		nullableFloat(item.ProteinG),
		nullableFloat(item.CarbsG),
		nullableFloat(item.FatG),
	)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	return int(id), nil
}

func (f *Fridge) UpdateItem(db *sql.DB, storeID int64, item Item) error {
	item.Name = strings.TrimSpace(item.Name)
	item.Barcode = strings.TrimSpace(item.Barcode)
	item.Brand = strings.TrimSpace(item.Brand)
	item.ServingSize = strings.TrimSpace(item.ServingSize)
	if err := ValidateItem(item); err != nil {
		return err
	}
	res, err := db.Exec(
		`UPDATE fridge SET name = ?, quantity = ?, expiration_date = ?, barcode = ?, brand = ?, serving_size = ?, calories = ?, protein_g = ?, carbs_g = ?, fat_g = ?
		 WHERE store_id = ? AND id = ?`,
		item.Name,
		item.Quantity,
		item.ExpirationDate.Format(DateFormat),
		nullableString(item.Barcode),
		nullableString(item.Brand),
		nullableString(item.ServingSize),
		nullableFloat(item.Calories),
		nullableFloat(item.ProteinG),
		nullableFloat(item.CarbsG),
		nullableFloat(item.FatG),
		storeID,
		item.ID,
	)
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
