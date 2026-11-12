package main

import (
	"flag"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/George-Anagnostou/fridge/auth"
	"github.com/George-Anagnostou/fridge/fridge"
)

func main() {
	ttl := flag.Duration("ttl", auth.DefaultInviteTTL, "how long the invite stays valid")
	base := flag.String("base-url", "http://localhost:8080", "public base URL used in the signup link")
	flag.Parse()

	db, err := fridge.InitDB(fridge.DBPath())
	if err != nil {
		fmt.Fprintf(os.Stderr, "error opening database: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	token, expiresAt, err := auth.CreateInvite(db, *ttl)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error creating invite: %v\n", err)
		os.Exit(1)
	}

	baseURL := strings.TrimRight(*base, "/")
	link := baseURL + "/signup?token=" + url.QueryEscape(token)
	fmt.Printf("Invite created (expires %s).\n", expiresAt.UTC().Format(time.RFC3339))
	fmt.Printf("Sign up: %s\n", link)
}
