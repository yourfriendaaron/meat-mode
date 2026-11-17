package bridge

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"unicode/utf8"

	"github.com/yourfriendaaron/meat-mode/internal/netguard"
)

// TestHubDoesNotFollowRedirects: a redirect must not carry the bearer token
// to another address.
func TestHubDoesNotFollowRedirects(t *testing.T) {
	var leaked atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			leaked.Add(1)
		}
		w.Write([]byte("[]"))
	}))
	defer other.Close()
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/collect", http.StatusTemporaryRedirect)
	}))
	defer hub.Close()

	for _, client := range []*http.Client{nil, {}} {
		h := &Hub{URL: hub.URL, Token: "token-alex", HTTP: client}
		_, err := h.Actions(context.Background())
		if err == nil || !strings.Contains(err.Error(), other.URL+"/collect") {
			t.Errorf("Actions through a redirect = %v, want an error naming the Location", err)
		}
	}
	if n := leaked.Load(); n != 0 {
		t.Errorf("the token followed the redirect %d times", n)
	}
}

// TestHubErrorPrintsSafely: the hub's error text and a redirect's Location
// reach the log without control or bidi characters, and a huge body is cut.
func TestHubErrorPrintsSafely(t *testing.T) {
	t.Parallel()
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("repo") {
		case "hostile":
			http.Error(w, `{"error": "\u001b]0;x\u0007owned\u202ereply"}`, http.StatusBadRequest)
		case "huge":
			http.Error(w, strings.Repeat("a", 1<<20), http.StatusInternalServerError)
		default:
			w.Header().Set("Location", "/elsewhere\x1b[2J")
			w.WriteHeader(http.StatusFound)
		}
	}))
	defer hub.Close()
	h := &Hub{URL: hub.URL, Token: "token-alex"}
	ctx := context.Background()

	err := h.PutItems(ctx, "hostile", nil)
	if err == nil || err.Error() != "hub replied 400: ]0;xownedreply" {
		t.Errorf("hostile body = %v", err)
	}
	err = h.PutItems(ctx, "huge", nil)
	if err == nil || utf8.RuneCountInString(err.Error()) > netguard.MaxMessageRunes+len("hub replied 500: ") {
		t.Errorf("a 1 MiB body was not cut")
	}
	err = h.PutItems(ctx, "redirect", nil)
	if err == nil || strings.ContainsRune(err.Error(), '\x1b') {
		t.Errorf("redirect = %q, want no escape character", err)
	}
}
