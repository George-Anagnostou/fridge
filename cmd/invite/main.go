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
	admin := flag.Bool("admin", false, "grant admin on sign-up (use this to bootstrap the first admin)")
	flag.Parse()

	db, err := fridge.InitDB(fridge.DBPath())
	if err != nil {
		fmt.Fprintf(os.Stderr, "error opening database: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	token, expiresAt, err := auth.CreateInviteSpec(db, auth.InviteSpec{
		TTL:         *ttl,
		GrantsAdmin: *admin,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error creating invite: %v\n", err)
		os.Exit(1)
	}

	baseURL := strings.TrimRight(*base, "/")
	link := baseURL + "/signup?token=" + url.QueryEscape(token)
	if *admin {
		fmt.Printf("Admin invite created (expires %s).\n", expiresAt.UTC().Format(time.RFC3339))
	} else {
		fmt.Printf("Invite created (expires %s).\n", expiresAt.UTC().Format(time.RFC3339))
	}
	fmt.Printf("Sign up: %s\n", link)
}
