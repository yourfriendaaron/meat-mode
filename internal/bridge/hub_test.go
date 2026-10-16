package bridge

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
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
