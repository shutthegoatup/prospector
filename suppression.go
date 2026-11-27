package main

import (
	"log"
	"net/http"
	"strings"

	"gorm.io/gorm"
)

// forgetAddresses drops what is held against addresses being erased. The erasure's own
// hash of each keeps them from being mailed again.
func forgetAddresses(tx *gorm.DB, org int64, addresses []string) error {
	if len(addresses) == 0 {
		return nil
	}
	// As the erasure's own hash does: a dot at either end is the sentence's, not the address's.
	for _, a := range addresses {
		addresses = append(addresses, strings.Trim(a, "."))
	}
	if err := tx.Where("org_id = ? and address in ?", org, addresses).Delete(&Suppression{}).Error; err != nil {
		return err
	}
	return tx.Where("org_id = ? and address in ?", org, addresses).Delete(&UnsubscribeLink{}).Error
}

// UnsubscribeView is everything the page at an unsubscribe link shows: nothing about
// whose link it is, since mail gets forwarded.
type UnsubscribeView struct {
	Page string `json:"page"`
	// Token is set only when it has the shape of a link we make, so the page's button
	// can't be aimed at another route.
	Token string `json:"token,omitempty"`
	Done  bool   `json:"done"`
}

func (a *App) unsubscribePage(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	// Only something shaped like a link we make goes back into the page's form: the path
	// arrives decoded, so "..%2Flogout" would otherwise aim the button at another route.
	if !linkToken(token) {
		token = ""
	}
	a.unsubscribeRender(w, r, UnsubscribeView{Token: token})
}

// linkToken is whether a path segment has the shape randomToken(32) gives.
func linkToken(t string) bool {
	if len(t) != 43 {
		return false
	}
	for _, c := range t {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') { //nolint:staticcheck // not in the alphabet
			return false
		}
	}
	return true
}

// unsubscribe takes the button on that page, and the one-click request a mail provider
// sends for the List-Unsubscribe-Post header. RFC 8058 has it answered without a
// redirect, and it comes with no cookie and no origin.
func (a *App) unsubscribe(w http.ResponseWriter, r *http.Request) {
	if err := a.s.UnsubscribeByToken(r.PathValue("token")); err != nil {
		log.Printf("an unsubscribe wasn't recorded: %v", err)
		http.Error(w, "that couldn't be saved; try the link again", http.StatusInternalServerError)
		return
	}
	a.unsubscribeRender(w, r, UnsubscribeView{Done: true})
}

func (a *App) unsubscribeRender(w http.ResponseWriter, r *http.Request, v UnsubscribeView) {
	customerHeaders(w)
	v.Page = "unsubscribe"
	writeShell(w, r, http.StatusOK, v, "This page needs a browser with JavaScript.\n")
}
