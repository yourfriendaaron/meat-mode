// Package fakehub is an in-memory hub serving the bridge API from docs/api.md,
// for bridge and end-to-end tests.
package fakehub

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

// Hub is a fake hub. Its fields are guarded by its methods.
type Hub struct {
	*httptest.Server

	mu       sync.Mutex
	tokens   map[string]string // token -> bridge name
	hellos   []Hello
	repos    map[string][]api.Repo
	items    map[string][]api.Item // "<bridge>/<repo>" -> items
	itemPuts map[string]int
	queue    map[string][]api.Action // bridge -> actions it was sent
	results  map[string][]api.ActionResult
	down     bool
}

// Hello is one greeting the hub received.
type Hello struct {
	Bridge string
	api.Hello
}

// New starts a fake hub that accepts the given tokens (token -> bridge name).
func New(tokens map[string]string) *Hub {
	h := &Hub{
		tokens: tokens, repos: map[string][]api.Repo{}, items: map[string][]api.Item{},
		itemPuts: map[string]int{}, queue: map[string][]api.Action{}, results: map[string][]api.ActionResult{},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /bridge/v1/hello", h.auth(h.hello))
	mux.HandleFunc("PUT /bridge/v1/repos", h.auth(h.putRepos))
	mux.HandleFunc("PUT /bridge/v1/items", h.auth(h.putItems))
	mux.HandleFunc("GET /bridge/v1/actions", h.auth(h.actions))
	mux.HandleFunc("POST /bridge/v1/actions/{id}/result", h.auth(h.result))
	h.Server = httptest.NewServer(mux)
	return h
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (h *Hub) auth(next func(http.ResponseWriter, *http.Request, string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h.mu.Lock()
		down := h.down
		name, ok := h.tokens[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
		h.mu.Unlock()
		if down {
			writeErr(w, http.StatusServiceUnavailable, "hub is down")
			return
		}
		if !ok {
			writeErr(w, http.StatusUnauthorized, "unknown token")
			return
		}
		next(w, r, name)
	}
}

func (h *Hub) hello(w http.ResponseWriter, r *http.Request, bridge string) {
	var hi api.Hello
	if err := json.NewDecoder(r.Body).Decode(&hi); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	h.mu.Lock()
	h.hellos = append(h.hellos, Hello{Bridge: bridge, Hello: hi})
	h.mu.Unlock()
	writeJSON(w, api.HelloReply{Bridge: bridge, ServerTime: time.Now().UTC()})
}

func (h *Hub) putRepos(w http.ResponseWriter, r *http.Request, bridge string) {
	var repos []api.Repo
	if err := json.NewDecoder(r.Body).Decode(&repos); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	for _, rp := range repos {
		if rp.Bridge != "" || rp.Name == "" {
			writeErr(w, http.StatusBadRequest, "repo must have a name and no bridge")
			return
		}
	}
	h.mu.Lock()
	h.repos[bridge] = repos
	h.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func (h *Hub) putItems(w http.ResponseWriter, r *http.Request, bridge string) {
	var items []api.Item
	if err := json.NewDecoder(r.Body).Decode(&items); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	for _, it := range items {
		if it.Key != "" || it.Bridge != "" || it.Repo != "" || it.Pending || it.ID == "" || it.Comments == nil {
			writeErr(w, http.StatusBadRequest, "item must have an id and comments, and no key, bridge, repo or pending")
			return
		}
	}
	k := bridge + "/" + r.URL.Query().Get("repo")
	h.mu.Lock()
	h.items[k] = items
	h.itemPuts[k]++
	h.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func (h *Hub) actions(w http.ResponseWriter, _ *http.Request, bridge string) {
	writeJSON(w, h.Pending(bridge))
}

func (h *Hub) result(w http.ResponseWriter, r *http.Request, bridge string) {
	var res api.ActionResult
	if err := json.NewDecoder(r.Body).Decode(&res); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	id := r.PathValue("id")
	h.mu.Lock()
	defer h.mu.Unlock()
	if !slices.ContainsFunc(h.queue[bridge], func(a api.Action) bool { return a.ID == id }) {
		writeErr(w, http.StatusNotFound, "no such action for this bridge")
		return
	}
	h.results[id] = append(h.results[id], res)
	w.WriteHeader(http.StatusNoContent)
}

// Enqueue queues an action for the named bridge. Its Bridge field is left as
// given, so a test can misroute one on purpose.
func (h *Hub) Enqueue(bridge string, a api.Action) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if a.Status == "" {
		a.Status = api.StatusPending
	}
	if a.CreatedAt.IsZero() {
		a.CreatedAt = time.Now().UTC()
	}
	a.Seq = int64(len(h.queue[bridge]) + 1)
	h.queue[bridge] = append(h.queue[bridge], a)
}

// Pending returns the bridge's actions that have no result yet, oldest first.
func (h *Hub) Pending(bridge string) []api.Action {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := []api.Action{}
	for _, a := range h.queue[bridge] {
		if len(h.results[a.ID]) == 0 {
			out = append(out, a)
		}
	}
	return out
}

// Results returns every result posted for an action, in order.
func (h *Hub) Results(id string) []api.ActionResult {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.results[id])
}

// SetDown makes every request fail with 503 while down is set.
func (h *Hub) SetDown(down bool) {
	h.mu.Lock()
	h.down = down
	h.mu.Unlock()
}

// Hellos returns the greetings received.
func (h *Hub) Hellos() []Hello {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.hellos)
}

// Repos returns the bridge's last reported repo list.
func (h *Hub) Repos(bridge string) []api.Repo {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.repos[bridge])
}

// Items returns the last item set for a repo, and whether one was sent.
func (h *Hub) Items(bridge, repo string) ([]api.Item, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	it, ok := h.items[bridge+"/"+repo]
	return slices.Clone(it), ok
}

// ItemPuts counts the item sets received for a repo.
func (h *Hub) ItemPuts(bridge, repo string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.itemPuts[bridge+"/"+repo]
}
