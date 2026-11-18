package main

import (
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/George-Anagnostou/fridge/fridge"
)

//go:embed templates/*.html
var templateFS embed.FS

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

var tmpl = template.Must(template.ParseFS(templateFS, "templates/*.html"))

type server struct {
	db     *sql.DB
	fridge fridge.Fridge
	tmpl   *template.Template
}

func newMux(db *sql.DB) http.Handler {
	s := &server{db: db, tmpl: tmpl}
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.listAndAddHandler)
	mux.HandleFunc("POST /remove", s.removeHandler)
	mux.HandleFunc("GET /items", s.getItemsHandler)
	mux.HandleFunc("POST /items", s.postItemsHandler)
	return mux
}

func listenAddr() string {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	port = strings.TrimPrefix(port, ":")
	return ":" + port
}

func main() {
	db, err := fridge.InitDB(fridge.DBPath())
	if err != nil {
		log.Fatalf("error initializing db: %v", err)
	}
	defer db.Close()

	addr := listenAddr()
	log.Printf("Starting server on %s...", addr)
	if err := http.ListenAndServe(addr, newMux(db)); err != nil {
		log.Fatal("Server failed: ", err)
	}
}

func (s *server) listAndAddHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		name := r.FormValue("name")
		quantity := r.FormValue("quantity")
		expiration := r.FormValue("expiration")

		qty, err := strconv.ParseFloat(quantity, 64)
		if err != nil {
			http.Error(w, "Invalid quantity", http.StatusBadRequest)
			return
		}

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

		id, err := s.fridge.AddItem(s.db, item.Name, item.Quantity, item.ExpirationDate)
		if err != nil {
			http.Error(w, "Error with storing item", http.StatusBadRequest)
			return
		}

		log.Println("added item to fridge:")
		item, err = s.fridge.GetItemByID(s.db, id)
		if err != nil {
			http.Error(w, "Error with getting new item", http.StatusBadRequest)
			return
		}
		log.Print(item.LogString())
	}

	items, err := s.fridge.ListItems(s.db)
	if err != nil {
		http.Error(w, "Unable to get items", http.StatusInternalServerError)
		log.Println("Error getting items", err)
		return
	}

	data := RenderFridge{Items: toRenderItems(items)}

	if err := s.tmpl.ExecuteTemplate(w, "index.html", data); err != nil {
		http.Error(w, "Unable to render template", http.StatusInternalServerError)
		log.Println("Error rendering template:", err)
	}
}

func (s *server) removeHandler(w http.ResponseWriter, r *http.Request) {
	idStr := r.FormValue("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "Unable to get item to remove", http.StatusBadRequest)
		log.Println("Unable to remove item", err)
		return
	}

	if err := s.fridge.RemoveItem(s.db, id); err != nil {
		http.Error(w, "Unable to remove item", http.StatusInternalServerError)
		log.Println("Unable to remove item", err)
		return
	}
	log.Printf("removed item %03d\n", id)

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *server) getItemsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	items, err := s.fridge.ListItems(s.db)
	if err != nil {
		http.Error(w, "Unable to get items", http.StatusInternalServerError)
		log.Println("Error getting items", err)
		return
	}

	if err := json.NewEncoder(w).Encode(items); err != nil {
		http.Error(w, "Failed to encode items to JSON", http.StatusInternalServerError)
	}
}

func (s *server) postItemsHandler(w http.ResponseWriter, r *http.Request) {
	var item fridge.Item
	if err := json.NewDecoder(r.Body).Decode(&item); err != nil {
		http.Error(w, "Invalid JSON data", http.StatusBadRequest)
		return
	}

	id, err := s.fridge.AddItem(s.db, item.Name, item.Quantity, item.ExpirationDate)
	if err != nil {
		http.Error(w, "Error adding item", http.StatusInternalServerError)
		log.Println("Error adding item", err)
		return
	}

	log.Println("added item to fridge")
	dbItem, err := s.fridge.GetItemByID(s.db, id)
	if err != nil {
		log.Println("Error getting items", err)
		http.Error(w, "Unable to get items", http.StatusInternalServerError)
		return
	}
	log.Println(dbItem.LogString())

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `{"message": "Item added successfully"}`)
}
