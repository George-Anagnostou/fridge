package main

import (
	"database/sql"
	"embed"
	"html/template"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/George-Anagnostou/fridge/auth"
	"github.com/George-Anagnostou/fridge/fridge"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static/*
var staticFS embed.FS

type RenderItem struct {
	ID             int
	Name           string
	Quantity       float64
	ExpirationDate string
	Barcode        string
	Brand          string
	ServingSize    string
	Calories       string
	ProteinG       string
	CarbsG         string
	FatG           string
	Expired        bool
}

func toRenderItems(items []fridge.Item) []RenderItem {
	out := make([]RenderItem, 0, len(items))
	now := time.Now()
	for _, item := range items {
		out = append(out, toRenderItemAt(item, now))
	}
	return out
}

func toRenderItem(item fridge.Item) RenderItem {
	return toRenderItemAt(item, time.Now())
}

func toRenderItemAt(item fridge.Item, now time.Time) RenderItem {
	return RenderItem{
		ID:             item.ID,
		Name:           item.Name,
		Quantity:       item.Quantity,
		ExpirationDate: item.ExpirationDate.Format(fridge.DateFormat),
		Barcode:        item.Barcode,
		Brand:          item.Brand,
		ServingSize:    item.ServingSize,
		Calories:       formatOptionalFloat(item.Calories),
		ProteinG:       formatOptionalFloat(item.ProteinG),
		CarbsG:         formatOptionalFloat(item.CarbsG),
		FatG:           formatOptionalFloat(item.FatG),
		Expired:        fridge.IsExpired(item.ExpirationDate, now),
	}
}

func formatOptionalFloat(v *float64) string {
	if v == nil {
		return ""
	}
	return strconv.FormatFloat(*v, 'f', -1, 64)
}

var tmpl = template.Must(template.ParseFS(templateFS, "templates/*.html"))

type server struct {
	db           *sql.DB
	fridge       fridge.Fridge
	tmpl         *template.Template
	cookieSecure bool
	sessionTTL   time.Duration
}

func newMux(db *sql.DB) http.Handler {
	s := &server{
		db:           db,
		tmpl:         tmpl,
		cookieSecure: cookieSecureFromEnv(),
		sessionTTL:   auth.DefaultSessionTTL,
	}
	mux := http.NewServeMux()
	mux.Handle("GET /static/", http.FileServer(http.FS(staticFS)))
	mux.HandleFunc("GET /login", s.ensureCSRF(s.loginForm))
	mux.HandleFunc("POST /login", s.ensureCSRF(s.requireCSRFOnPost(s.loginSubmit)))
	mux.HandleFunc("GET /signup", s.ensureCSRF(s.signupForm))
	mux.HandleFunc("POST /signup", s.ensureCSRF(s.requireCSRFOnPost(s.signupSubmit)))
	mux.HandleFunc("POST /logout", s.ensureCSRF(s.requireCSRFOnPost(s.logout)))
	mux.HandleFunc("GET /admin/invites", s.requireAdmin(s.adminInvitesForm))
	mux.HandleFunc("POST /admin/invites", s.requireAdmin(s.adminInvitesSubmit))
	mux.HandleFunc("GET /lookup", s.protectAPI(s.lookupHandler))
	mux.HandleFunc("GET /{$}", s.protectHTML(s.listStoresHandler))
	mux.HandleFunc("POST /stores", s.protectHTML(s.createStoreHandler))
	mux.HandleFunc("GET /stores/{id}", s.protectHTML(s.storePageHandler))
	mux.HandleFunc("POST /stores/{id}", s.protectHTML(s.storePageHandler))
	mux.HandleFunc("POST /stores/{id}/rename", s.protectHTML(s.renameStoreHandler))
	mux.HandleFunc("POST /stores/{id}/delete", s.protectHTML(s.deleteStoreHandler))
	mux.HandleFunc("POST /stores/{id}/items/remove", s.protectHTML(s.removeStoreItemHandler))
	mux.HandleFunc("GET /stores/{id}/items", s.protectAPI(s.getStoreItemsAPI))
	mux.HandleFunc("POST /stores/{id}/items", s.protectAPI(s.postStoreItemsAPI))
	mux.HandleFunc("GET /stores/{id}/items/{itemID}", s.protectAPI(s.getStoreItemAPI))
	mux.HandleFunc("POST /stores/{id}/items/{itemID}", s.protectAPI(s.postStoreItemAPI))
	mux.HandleFunc("GET /stores/{id}/items/{itemID}/edit", s.protectHTML(s.editItemForm))
	mux.HandleFunc("POST /stores/{id}/items/{itemID}/edit", s.protectHTML(s.editItemSubmit))
	mux.HandleFunc("GET /stores/{id}/members", s.protectHTML(s.requireOwner(s.membersForm)))
	mux.HandleFunc("POST /stores/{id}/members", s.protectHTML(s.requireOwner(s.membersShare)))
	mux.HandleFunc("POST /stores/{id}/members/update", s.protectHTML(s.requireOwner(s.membersUpdate)))
	mux.HandleFunc("POST /stores/{id}/members/remove", s.protectHTML(s.requireOwner(s.membersRemove)))
	return mux
}

// listenAddr is 127.0.0.1:8080 by default so the process is not reachable
// around nginx. FRIDGE_ADDR, if set, is the full listen address. PORT still
// changes the port, but keeps the loopback host.
func listenAddr() string {
	if addr := strings.TrimSpace(os.Getenv("FRIDGE_ADDR")); addr != "" {
		return addr
	}
	port := strings.TrimSpace(os.Getenv("PORT"))
	if port == "" {
		port = "8080"
	}
	port = strings.TrimPrefix(port, ":")
	return "127.0.0.1:" + port
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
