package main

import (
	"database/sql"
	"embed"
	"html/template"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/George-Anagnostou/fridge/auth"
	"github.com/George-Anagnostou/fridge/fridge"
)

//go:embed templates/*.html
var templateFS embed.FS

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
	mux.HandleFunc("GET /login", s.ensureCSRF(s.loginForm))
	mux.HandleFunc("POST /login", s.ensureCSRF(s.requireCSRFOnPost(s.loginSubmit)))
	mux.HandleFunc("GET /signup", s.ensureCSRF(s.signupForm))
	mux.HandleFunc("POST /signup", s.ensureCSRF(s.requireCSRFOnPost(s.signupSubmit)))
	mux.HandleFunc("POST /logout", s.ensureCSRF(s.requireCSRFOnPost(s.logout)))
	mux.HandleFunc("GET /admin/invites", s.requireAdmin(s.adminInvitesForm))
	mux.HandleFunc("POST /admin/invites", s.requireAdmin(s.adminInvitesSubmit))
	mux.HandleFunc("GET /{$}", s.protectHTML(s.listStoresHandler))
	mux.HandleFunc("POST /stores", s.protectHTML(s.createStoreHandler))
	mux.HandleFunc("GET /stores/{id}", s.protectHTML(s.storePageHandler))
	mux.HandleFunc("POST /stores/{id}", s.protectHTML(s.storePageHandler))
	mux.HandleFunc("POST /stores/{id}/rename", s.protectHTML(s.renameStoreHandler))
	mux.HandleFunc("POST /stores/{id}/delete", s.protectHTML(s.deleteStoreHandler))
	mux.HandleFunc("POST /stores/{id}/items/remove", s.protectHTML(s.removeStoreItemHandler))
	mux.HandleFunc("GET /stores/{id}/items", s.protectAPI(s.getStoreItemsAPI))
	mux.HandleFunc("POST /stores/{id}/items", s.protectAPI(s.postStoreItemsAPI))
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
