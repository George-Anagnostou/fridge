package main

import (
	"log"
	"net/http"
	"net/url"
	"time"

	"github.com/George-Anagnostou/fridge/auth"
)

type adminInvitesPage struct {
	CSRF      string
	Email     string
	Error     string
	InviteURL string
	Expires   string
}

func (s *server) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return s.protectHTML(func(w http.ResponseWriter, r *http.Request) {
		u, ok := userFrom(r)
		if !ok || !u.IsAdmin {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		next(w, r)
	})
}

func (s *server) adminInvitesForm(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r)
	s.renderAdminInvites(w, adminInvitesPage{
		CSRF:  s.csrfToken(r),
		Email: u.Email,
	})
}

func (s *server) adminInvitesSubmit(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r)
	id := u.ID
	token, expiresAt, err := auth.CreateInviteSpec(s.db, auth.InviteSpec{
		TTL:       auth.DefaultInviteTTL,
		CreatedBy: &id,
	})
	if err != nil {
		log.Println("create sign-up invite:", err)
		w.WriteHeader(http.StatusInternalServerError)
		s.renderAdminInvites(w, adminInvitesPage{
			CSRF:  s.csrfToken(r),
			Email: u.Email,
			Error: "Unable to create an invite.",
		})
		return
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	inviteURL := scheme + "://" + r.Host + "/signup?token=" + url.QueryEscape(token)
	s.renderAdminInvites(w, adminInvitesPage{
		CSRF:      s.csrfToken(r),
		Email:     u.Email,
		InviteURL: inviteURL,
		Expires:   expiresAt.UTC().Format(time.RFC3339),
	})
}

func (s *server) renderAdminInvites(w http.ResponseWriter, data adminInvitesPage) {
	if err := s.tmpl.ExecuteTemplate(w, "admin_invites.html", data); err != nil {
		http.Error(w, "Unable to render template", http.StatusInternalServerError)
		log.Println("Error rendering template:", err)
	}
}
