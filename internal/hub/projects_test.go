package hub

import (
	"net/http"
	"slices"
	"testing"

	"github.com/yourfriendaaron/meat-mode/internal/api"
	"github.com/yourfriendaaron/meat-mode/internal/store"
)

// An agent's first add to a project makes it; only its machine's token
// reads and changes what it added there, and the app shows it as a list
// of the hub's own that takes no ask.
func TestProjectOverHTTP(t *testing.T) {
	h := newHarness(t, nil, false)
	h.pair()
	w := h.do("POST", "/api/bridges", map[string]string{"name": "desk"}, "")
	h.expect(w, http.StatusCreated)
	other := decode[api.NewBridgeReply](t, w).Token

	const q = "?project=projects/garden"
	w = h.do("POST", "/bridge/v1/personal"+q, api.PersonalTodo{Title: "Approve the footer text", Author: "planner"}, h.token)
	h.expect(w, http.StatusCreated)
	reply := decode[api.PersonalTodoReply](t, w)
	if reply.Key != store.ItemKey(store.HubBridge, "projects/garden", reply.ID) {
		t.Fatalf("reply %+v", reply)
	}
	st := decode[api.State](t, h.do("GET", "/api/state", nil, ""))
	if !slices.ContainsFunc(st.Repos, func(r api.Repo) bool {
		return r.Bridge == store.HubBridge && r.Name == "projects/garden" && !r.Personal
	}) {
		t.Fatalf("repos %+v", st.Repos)
	}

	h.expect(h.do("GET", "/bridge/v1/personal/"+reply.ID+q, nil, h.token), http.StatusOK)
	h.expect(h.do("GET", "/bridge/v1/personal/"+reply.ID+q, nil, other), http.StatusNotFound)
	h.expect(h.do("GET", "/bridge/v1/personal/"+reply.ID, nil, h.token), http.StatusNotFound)
	h.expect(h.do("POST", "/bridge/v1/personal/"+reply.ID+"/actions"+q, api.PersonalChange{Kind: api.KindDone}, other), http.StatusNotFound)
	h.expect(h.do("POST", "/bridge/v1/personal/"+reply.ID+"/actions"+q, api.PersonalChange{Kind: api.KindDone}, h.token), http.StatusCreated)
	if it := decode[api.Item](t, h.do("GET", "/bridge/v1/personal/"+reply.ID+q, nil, h.token)); it.Status != "closed" {
		t.Errorf("item after done %+v", it)
	}

	// The personal list's name is kept for it.
	h.expect(h.do("POST", "/bridge/v1/personal?project=personal", api.PersonalTodo{Title: "Water the plants"}, h.token), http.StatusBadRequest)
	h.expect(h.do("POST", "/bridge/v1/personal?project=../x", api.PersonalTodo{Title: "Water the plants"}, h.token), http.StatusBadRequest)

	h.expect(h.do("POST", "/api/actions", api.NewAction{ID: aid1, Kind: api.KindAsk, Key: reply.Key}, ""), http.StatusBadRequest)
}

// A machine's trackers keep their title and raise command from hello to
// the app's machine list.
func TestHelloKeepsTrackerTitleAndRaise(t *testing.T) {
	h := newHarness(t, nil, false)
	h.pair()
	sent := []api.TrackerVersion{{Name: "backlog", Title: "Backlog.md", Version: "1.20.0",
		Raise: `backlog task create "Approve the footer text" -l human`}}
	h.expect(h.do("POST", "/bridge/v1/hello", api.Hello{Version: "1.0.0", Trackers: sent}, h.token), http.StatusOK)
	bs := decode[[]api.Bridge](t, h.do("GET", "/api/bridges", nil, ""))
	if len(bs) != 1 || !slices.Equal(bs[0].Trackers, sent) {
		t.Fatalf("bridges %+v", bs)
	}
}

// An agent adds a sub-task only under an open top-level item its machine
// added to the same list; any other parent is one it cannot see.
func TestAgentAddsSubtaskOverHTTP(t *testing.T) {
	h := newHarness(t, nil, false)
	h.pair()
	w := h.do("POST", "/api/bridges", map[string]string{"name": "desk"}, "")
	h.expect(w, http.StatusCreated)
	other := decode[api.NewBridgeReply](t, w).Token
	add := func(token, project string, todo api.PersonalTodo, want int) string {
		t.Helper()
		q := ""
		if project != "" {
			q = "?project=" + project
		}
		w := h.do("POST", "/bridge/v1/personal"+q, todo, token)
		h.expect(w, want)
		if want != http.StatusCreated {
			return ""
		}
		return decode[api.PersonalTodoReply](t, w).ID
	}
	trip := add(h.token, "", api.PersonalTodo{Title: "Plan the trip"}, http.StatusCreated)
	tickets := add(h.token, "", api.PersonalTodo{Title: "Book the tickets", Parent: trip}, http.StatusCreated)
	it := decode[api.Item](t, h.do("GET", "/bridge/v1/personal/"+tickets, nil, h.token))
	if it.Parent != trip {
		t.Fatalf("sub-task %+v", it)
	}

	theirs := add(other, "", api.PersonalTodo{Title: "Pack the bags"}, http.StatusCreated)
	garden := add(h.token, "projects/garden", api.PersonalTodo{Title: "Plant the beans"}, http.StatusCreated)
	add(h.token, "", api.PersonalTodo{Title: "Find the passports", Parent: theirs}, http.StatusNotFound)
	add(h.token, "", api.PersonalTodo{Title: "Find the passports", Parent: garden}, http.StatusNotFound)
	add(h.token, "", api.PersonalTodo{Title: "Find the passports", Parent: "mm-404"}, http.StatusNotFound)
	add(h.token, "", api.PersonalTodo{Title: "Find the passports", Parent: tickets}, http.StatusBadRequest)
	h.expect(h.do("POST", "/bridge/v1/personal/"+tickets+"/actions", api.PersonalChange{Kind: api.KindDone}, h.token), http.StatusCreated)
	h.expect(h.do("POST", "/bridge/v1/personal/"+trip+"/actions", api.PersonalChange{Kind: api.KindDone}, h.token), http.StatusCreated)
	add(h.token, "", api.PersonalTodo{Title: "Find the passports", Parent: trip}, http.StatusBadRequest)
	add(h.token, "projects/garden", api.PersonalTodo{Title: "Water the beans", Parent: garden}, http.StatusCreated)
}
