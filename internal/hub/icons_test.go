package hub

import (
	"net/http"
	"strings"
	"testing"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

const testSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1 1"><rect width="1" height="1"/></svg>`

// repoIcon returns the icon hash State gives a repo of laptop's.
func (h *harness) repoIcon(name string) string {
	h.t.Helper()
	for _, r := range decode[api.State](h.t, h.do("GET", "/api/state", nil, "")).Repos {
		if r.Bridge == "laptop" && r.Name == name {
			return r.Icon
		}
	}
	h.t.Fatalf("no repo %q in State", name)
	return ""
}

func TestIconRoutes(t *testing.T) {
	h := newHarness(t, nil, false)
	h.pair()
	hash := api.IconHash([]byte(testSVG))

	rev := h.rev()
	h.expect(h.do("PUT", "/bridge/v1/icons?repo=garden", testSVG, h.token), http.StatusNoContent)
	if h.rev() == rev {
		t.Error("a new icon did not change State")
	}
	if got := h.repoIcon("garden"); got != hash {
		t.Fatalf("garden's icon = %q, want %q", got, hash)
	}
	rev = h.rev()
	h.expect(h.do("PUT", "/bridge/v1/icons?repo=garden", testSVG, h.token), http.StatusNoContent)
	if h.rev() != rev {
		t.Error("the same icon again changed State")
	}

	w := h.do("GET", "/api/icons/"+hash, nil, "")
	h.expect(w, http.StatusOK)
	if w.Body.String() != testSVG {
		t.Errorf("icon bytes %q", w.Body)
	}
	for k, v := range map[string]string{
		"Content-Type":                 api.IconSVG,
		"Cache-Control":                "private, max-age=31536000, immutable",
		"X-Content-Type-Options":       "nosniff",
		"Cross-Origin-Resource-Policy": "same-origin",
		"Content-Security-Policy":      "default-src 'none'; style-src 'unsafe-inline'; sandbox",
	} {
		if got := w.Header().Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
	h.expect(h.do("GET", "/api/icons/"+strings.Repeat("0", 32), nil, ""), http.StatusNotFound)
	h.expect(h.do("GET", "/api/icons/"+strings.ToUpper(hash), nil, ""), http.StatusBadRequest)
	h.expect(h.do("GET", "/api/icons/abc", nil, ""), http.StatusBadRequest)

	// Refusals, in the contract's order: size, bytes, then the repo.
	h.expect(h.do("PUT", "/bridge/v1/icons?repo=garden", strings.Repeat(" ", api.MaxIconBytes+1), h.token), http.StatusRequestEntityTooLarge)
	h.expect(h.do("PUT", "/bridge/v1/icons?repo=nowhere", "<html></html>", h.token), http.StatusBadRequest)
	h.expect(h.do("PUT", "/bridge/v1/icons?repo=nowhere", testSVG, h.token), http.StatusConflict)
	h.expect(h.do("PUT", "/bridge/v1/icons?repo=..", testSVG, h.token), http.StatusBadRequest)
	h.expect(h.do("PUT", "/bridge/v1/icons?repo=garden", testSVG, ""), http.StatusUnauthorized)
	if got := h.repoIcon("garden"); got != hash {
		t.Errorf("a refused icon moved garden's to %q", got)
	}

	// An empty body clears it, and is 204 when there is none.
	h.expect(h.do("PUT", "/bridge/v1/icons?repo=garden", "", h.token), http.StatusNoContent)
	h.expect(h.do("PUT", "/bridge/v1/icons?repo=garden", "", h.token), http.StatusNoContent)
	if got := h.repoIcon("garden"); got != "" {
		t.Errorf("garden's icon after a clear = %q", got)
	}
	h.expect(h.do("GET", "/api/icons/"+hash, nil, ""), http.StatusNotFound)

	// The icon goes with the repo dropped from a report, and with a revoke.
	h.expect(h.do("PUT", "/bridge/v1/icons?repo=chores", testSVG, h.token), http.StatusNoContent)
	h.expect(h.do("PUT", "/bridge/v1/repos", []api.Repo{{Name: "garden", OptedIn: true}}, h.token), http.StatusNoContent)
	h.expect(h.do("GET", "/api/icons/"+hash, nil, ""), http.StatusNotFound)
	h.expect(h.do("PUT", "/bridge/v1/icons?repo=garden", testSVG, h.token), http.StatusNoContent)
	h.expect(h.do("DELETE", "/api/bridges/laptop", nil, ""), http.StatusNoContent)
	h.expect(h.do("GET", "/api/icons/"+hash, nil, ""), http.StatusNotFound)
}

func TestReposCarryColours(t *testing.T) {
	h := newHarness(t, nil, false)
	h.pair()
	colours := map[string]int{}
	for _, r := range decode[api.State](t, h.do("GET", "/api/state", nil, "")).Repos {
		if r.Colour == nil {
			t.Fatalf("repo %s/%s has no colour", r.Bridge, r.Name)
		}
		colours[r.Name] = *r.Colour
	}
	// The personal list holds 0; chores and garden follow in name order.
	if colours["personal"] != 0 || colours["chores"] != 1 || colours["garden"] != 2 {
		t.Errorf("colours %v", colours)
	}
}
