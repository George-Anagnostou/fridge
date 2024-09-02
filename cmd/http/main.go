package main

import (
	"database/sql"
	"html/template"
	"log"
	"net/http"
	"path/filepath"
	"strconv"
	"time"

	"github.com/George-Anagnostou/fridge/fridge"
)

type RenderFridge struct {
	Items []RenderItem
}

type RenderItem struct {
	ID             int
	Name           string
	Quantity       float64
	ExpirationDate string
}

func toRenderItems(items []fridge.Item) []RenderItem {
	var renderItems []RenderItem
	for _, item := range items {
		renderItem := RenderItem{
			ID:             item.ID,
			Name:           item.Name,
			Quantity:       item.Quantity,
			ExpirationDate: item.ExpirationDate.Format(fridge.DateFormat),
		}
		renderItems = append(renderItems, renderItem)
	}

	return renderItems
}

var (
	myFridge fridge.Fridge
	tmpl     *template.Template
	db       *sql.DB
)

func init() {
	// Parse all templates in the templates directory
	tmplPath := filepath.Join("cmd", "http", "templates", "*.html")
	tmpl = template.Must(template.ParseGlob(tmplPath))
}

func main() {
	var err error
	db, err = fridge.InitDB()
	if err != nil {
		log.Fatalf("error initializing db: %v", err)
	}
	defer db.Close()

	http.HandleFunc("/", listAndAddHandler)
	http.HandleFunc("POST /remove", removeHandler)

	log.Print("Starting server on :8080...")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatal("Server failed: ", err)
	}
}

func listAndAddHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		name := r.FormValue("name")
		quantity := r.FormValue("quantity")
		expiration := r.FormValue("expiration")

		// Convert quantity from string to int
		qty, err := strconv.ParseFloat(quantity, 64)
		if err != nil {
			http.Error(w, "Invalid quantity", http.StatusBadRequest)
			return
		}

		// Add the item to the fridge
		expirationDate, err := time.Parse(fridge.DateFormat, expiration)
		if err != nil {
			http.Error(w, "Invalid expiraiton time", http.StatusBadRequest)
			return
		}

		item := fridge.Item{
			Name:           name,
			Quantity:       qty,
			ExpirationDate: expirationDate,
		}

		myFridge.Lock()
		id, err := myFridge.AddItem(db, item.Name, item.Quantity, item.ExpirationDate)
		if err != nil {
			http.Error(w, "Error with storing item", http.StatusBadRequest)
			return
		}

		log.Println("added item to fridge:")
		item, err = myFridge.GetItemByID(db, id)
		if err != nil {
			http.Error(w, "Error with getting new item", http.StatusBadRequest)
			return
		}
		log.Printf("\n%s", item)
		myFridge.Unlock()
	}

	// Render the template with the current items
	items, err := myFridge.ListItems(db)
	if err != nil {
		http.Error(w, "Unable to get items", http.StatusInternalServerError)
		log.Println("Error getting items", err)
	}

	myFridge.Lock()
	data := RenderFridge{Items: toRenderItems(items)}
	myFridge.Unlock()

	if err := tmpl.ExecuteTemplate(w, "index.html", data); err != nil {
		http.Error(w, "Unable to render template", http.StatusInternalServerError)
		log.Println("Error rendering template:", err)
	}
}

func removeHandler(w http.ResponseWriter, r *http.Request) {
	log.Print(r.Method)
	idStr := r.FormValue("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "Unable to get item to remove", http.StatusInternalServerError)
		log.Println("Unable to remove item", err)
	}

	log.Printf("trying to remove id = %03d\n", id)

	// Remove the item from the fridge
	myFridge.Lock()
	// get ID first?
	myFridge.RemoveItem(db, id)
	log.Printf("removed item %03d\n", id)
	myFridge.Unlock()

	// Redirect back to the list
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
