package hub

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/api"
	"github.com/yourfriendaaron/meat-mode/internal/store"
)

const (
	cid1 = "7a1c2b8e-3d4a-4e5f-9a0b-1c2d3e4f5a61"
	cid2 = "7a1c2b8e-3d4a-4e5f-9a0b-1c2d3e4f5a62"
	cid3 = "7a1c2b8e-3d4a-4e5f-9a0b-1c2d3e4f5a63"
)

func TestAgentChangesAnyPersonalTodo(t *testing.T) {
	h := newHarness(t, nil, false)
	h.pair()
	w := h.do("POST", "/api/bridges", map[string]string{"name": "desk"}, "")
	h.expect(w, http.StatusCreated)
	other := decode[api.NewBridgeReply](t, w).Token
	lastSeen := func() time.Time {
		for _, b := range decode[[]api.Bridge](t, h.do("GET", "/api/bridges", nil, "")) {
			if b.Name == "laptop" {
				return *b.LastSeen
			}
		}
		t.Fatal("no laptop")
		return time.Time{}
	}
	seen := lastSeen()
	time.Sleep(2 * time.Millisecond)

	w = h.do("POST", "/bridge/v1/personal", api.PersonalTodo{Title: "Renew the library card", Author: "planner"}, h.token)
	h.expect(w, http.StatusCreated)
	id := decode[api.PersonalTodoReply](t, w).ID
	// One added in the app belongs to no machine.
	w = h.do("POST", "/api/actions", api.NewAction{ID: aid1, Kind: api.KindAdd, Bridge: store.HubBridge, Repo: store.PersonalRepo,
		Payload: api.Payload{Title: "Water the plants"}}, "")
	h.expect(w, http.StatusCreated)
	appID := decode[api.Action](t, w).BeadID

	w = h.do("GET", "/bridge/v1/personal/"+id, nil, h.token)
	h.expect(w, http.StatusOK)
	if it := decode[api.Item](t, w); it.Title != "Renew the library card" || it.CreatedBy != "planner" {
		t.Fatalf("item %+v", it)
	}

	// Any machine reads and changes another machine's item and one the
	// user added in the app; the change carries the agent and its machine.
	for _, tc := range []struct{ token, id string }{{other, id}, {h.token, appID}} {
		h.expect(h.do("GET", "/bridge/v1/personal/"+tc.id, nil, tc.token), http.StatusOK)
	}
	w = h.do("POST", "/bridge/v1/personal/"+appID+"/actions", api.PersonalChange{ID: cid3, Kind: api.KindEdit,
		Payload: api.Payload{Set: &api.Edit{Title: new("Water the ferns")}}, Author: "gardener"}, other)
	h.expect(w, http.StatusCreated)
	if a := decode[api.Action](t, w); a.Author != "gardener" || a.Machine != "desk" || a.Payload.Was == nil {
		t.Fatalf("change to the app's item %+v", a)
	}
	if as := decode[[]api.Action](t, h.do("GET", "/api/actions?limit=1", nil, "")); len(as) != 1 || as[0].Machine != "desk" || as[0].Author != "gardener" {
		t.Fatalf("the app's action log %+v", as)
	}
	w = h.do("GET", "/bridge/v1/personal?q=FERNS", nil, other)
	h.expect(w, http.StatusOK)
	if items := decode[[]api.Item](t, w); len(items) != 1 || items[0].ID != appID || items[0].Title != "Water the ferns" {
		t.Fatalf("search %+v", items)
	}
	w = h.do("GET", "/bridge/v1/personal", nil, other)
	h.expect(w, http.StatusOK)
	if items := decode[[]api.Item](t, w); len(items) != 2 {
		t.Fatalf("list %+v", items)
	}
	h.expect(h.do("GET", "/bridge/v1/personal?status=done", nil, other), http.StatusBadRequest)

	// An unknown id answers alike for reading and changing.
	var body string
	for _, bad := range []string{"mm-99", "x..y"} {
		for _, w := range []*httptest.ResponseRecorder{
			h.do("GET", "/bridge/v1/personal/"+bad, nil, h.token),
			h.do("POST", "/bridge/v1/personal/"+bad+"/actions", api.PersonalChange{Kind: api.KindDone}, h.token),
		} {
			h.expect(w, http.StatusNotFound)
			got := strings.ReplaceAll(w.Body.String(), bad, "ID")
			if body == "" {
				body = got
			} else if got != body {
				t.Errorf("404 body %s differs from %s", got, body)
			}
		}
	}

	due := time.Date(2026, 3, 9, 9, 0, 0, 0, time.UTC)
	change := api.PersonalChange{ID: cid1, Kind: api.KindEdit, Payload: api.Payload{Set: &api.Edit{Due: &due, Priority: new(int)}}, Author: "planner"}
	w = h.do("POST", "/bridge/v1/personal/"+id+"/actions", change, h.token)
	h.expect(w, http.StatusCreated)
	a := decode[api.Action](t, w)
	if a.Kind != api.KindEdit || a.Status != api.StatusApplied || a.Author != "planner" || a.Payload.Was == nil || !a.Payload.Was.ClearDue {
		t.Fatalf("action %+v", a)
	}
	w = h.do("POST", "/bridge/v1/personal/"+id+"/actions", change, h.token)
	h.expect(w, http.StatusOK)
	if decode[api.Action](t, w).Seq != a.Seq {
		t.Fatal("a repeat made another action")
	}
	w = h.do("POST", "/bridge/v1/personal/"+id+"/actions", api.PersonalChange{ID: cid2, Kind: api.KindNote, Payload: api.Payload{Text: "Ask at the desk"}}, h.token)
	h.expect(w, http.StatusCreated)
	if a := decode[api.Action](t, w); a.Author != "laptop" {
		t.Errorf("author %q, want the machine name", a.Author)
	}
	for _, c := range []api.PersonalChange{
		{Kind: api.KindSnooze},
		{Kind: api.KindEdit},
		{Kind: api.KindEdit, Payload: api.Payload{Set: &api.Edit{Priority: new(int), ClearDue: true, Due: &due}}},
	} {
		h.expect(h.do("POST", "/bridge/v1/personal/"+id+"/actions", c, h.token), http.StatusBadRequest)
	}
	h.expect(h.do("POST", "/bridge/v1/personal/"+id+"/actions", "{", h.token), http.StatusBadRequest)

	it := decode[api.Item](t, h.do("GET", "/bridge/v1/personal/"+id, nil, h.token))
	if it.DueAt == nil || !it.DueAt.Equal(due) || it.Priority != 0 || len(it.Comments) != 1 {
		t.Fatalf("item after changes %+v", it)
	}
	h.expect(h.do("POST", "/bridge/v1/personal/"+id+"/actions", api.PersonalChange{Kind: api.KindDone}, h.token), http.StatusCreated)
	if it := decode[api.Item](t, h.do("GET", "/bridge/v1/personal/"+id, nil, h.token)); it.Status != "closed" {
		t.Fatalf("done item %+v", it)
	}
	if got := lastSeen(); !got.Equal(seen) {
		t.Errorf("last-seen moved from %v to %v", seen, got)
	}
	for _, rt := range [][2]string{{"GET", "/bridge/v1/personal/" + id}, {"POST", "/bridge/v1/personal/" + id + "/actions"},
		{"GET", "/bridge/v1/personal"}, {"GET", "/bridge/v1/projects"}} {
		h.expect(h.do(rt[0], rt[1], "{}", ""), http.StatusUnauthorized)
	}
}

// The app may edit a personal item but not an agent's.
func TestEditOverHTTP(t *testing.T) {
	h := newHarness(t, nil, false)
	h.pair()
	title := "Water the ferns"
	w := h.do("POST", "/api/actions", api.NewAction{ID: aid1, Kind: api.KindEdit, Key: "laptop/garden/gd-1",
		Payload: api.Payload{Set: &api.Edit{Title: &title}}}, "")
	h.expect(w, http.StatusBadRequest)
	if !strings.Contains(w.Body.String(), "note") {
		t.Errorf("message %s", w.Body)
	}
	w = h.do("POST", "/api/actions", api.NewAction{ID: aid2, Kind: api.KindAdd, Bridge: store.HubBridge, Repo: store.PersonalRepo,
		Payload: api.Payload{Title: "Water the plants"}}, "")
	h.expect(w, http.StatusCreated)
	key := store.ItemKey(store.HubBridge, store.PersonalRepo, decode[api.Action](t, w).BeadID)
	w = h.do("POST", "/api/actions", api.NewAction{ID: cid3, Kind: api.KindEdit, Key: key,
		Payload: api.Payload{Set: &api.Edit{Title: &title}}}, "")
	h.expect(w, http.StatusCreated)
	if a := decode[api.Action](t, w); a.Payload.Was == nil || a.Payload.Was.Title == nil || *a.Payload.Was.Title != "Water the plants" {
		t.Fatalf("edit %+v", a.Payload)
	}
	if as := decode[[]api.Action](t, h.do("GET", "/bridge/v1/actions", nil, h.token)); len(as) != 0 {
		t.Fatalf("the bridge pulled %+v", as)
	}
}

// A repeating to-do from an agent or the app stays open when done.
func TestRepeatOverHTTP(t *testing.T) {
	h := newHarness(t, nil, false)
	h.pair()
	due := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	daily := &api.Repeat{Every: api.EveryDay, TimeZone: "Europe/Paris"}
	h.expect(h.do("POST", "/bridge/v1/personal", api.PersonalTodo{Title: "Water the plants", Repeat: daily}, h.token), http.StatusBadRequest)
	h.expect(h.do("POST", "/api/actions", api.NewAction{ID: aid1, Kind: api.KindAdd, Bridge: "laptop", Repo: "garden",
		Payload: api.Payload{Title: "Water the plants", Due: &due, Repeat: daily}}, ""), http.StatusBadRequest)

	w := h.do("POST", "/bridge/v1/personal", api.PersonalTodo{Title: "Water the plants", Due: &due, Repeat: daily}, h.token)
	h.expect(w, http.StatusCreated)
	id := decode[api.PersonalTodoReply](t, w).ID
	w = h.do("POST", "/bridge/v1/personal/"+id+"/actions", api.PersonalChange{Kind: api.KindDone}, h.token)
	h.expect(w, http.StatusCreated)
	if !strings.Contains(w.Body.String(), `"was":{"due":"`) {
		t.Errorf("done %s", w.Body)
	}
	it := decode[api.Item](t, h.do("GET", "/bridge/v1/personal/"+id, nil, h.token))
	if it.Status != "open" || it.Repeat == nil || !it.Repeat.Start.Equal(due) || !it.DueAt.Equal(due.Add(24*time.Hour)) {
		t.Fatalf("after done %+v", it)
	}

	w = h.do("POST", "/api/actions", api.NewAction{ID: aid2, Kind: api.KindEdit, Key: it.Key,
		Payload: api.Payload{Set: &api.Edit{RemindBefore: new(int), ClearRepeat: true}}}, "")
	h.expect(w, http.StatusCreated)
	if a := decode[api.Action](t, w); a.Payload.Was == nil || a.Payload.Was.Repeat == nil || !a.Payload.Was.ClearRemindBefore {
		t.Fatalf("edit %+v", a.Payload.Was)
	}

	// An end date before the due date is refused, and so is a move past it.
	ended := func(until time.Time) *api.Repeat {
		return &api.Repeat{Every: api.EveryDay, TimeZone: "UTC", Until: until.Format(time.DateOnly)}
	}
	h.expect(h.do("POST", "/bridge/v1/personal", api.PersonalTodo{Title: "Call Sam", Due: &due, Repeat: ended(due.AddDate(0, 0, -1))}, h.token), http.StatusBadRequest)
	w = h.do("POST", "/bridge/v1/personal", api.PersonalTodo{Title: "Call Sam", Due: &due, Repeat: ended(due)}, h.token)
	h.expect(w, http.StatusCreated)
	id = decode[api.PersonalTodoReply](t, w).ID
	if it := decode[api.Item](t, h.do("GET", "/bridge/v1/personal/"+id, nil, h.token)); it.Repeat == nil || it.Repeat.Until != due.Format(time.DateOnly) {
		t.Fatalf("added %+v", it.Repeat)
	}
	later := due.AddDate(0, 0, 2)
	h.expect(h.do("POST", "/bridge/v1/personal/"+id+"/actions", api.PersonalChange{Kind: api.KindEdit,
		Payload: api.Payload{Set: &api.Edit{Due: &later}}}, h.token), http.StatusBadRequest)
	// Its one date done, it closes.
	w = h.do("POST", "/bridge/v1/personal/"+id+"/actions", api.PersonalChange{Kind: api.KindDone}, h.token)
	h.expect(w, http.StatusCreated)
	if a := decode[api.Action](t, w); a.Payload.Was != nil {
		t.Fatalf("closing done %+v", a)
	}
	if it := decode[api.Item](t, h.do("GET", "/bridge/v1/personal/"+id, nil, h.token)); it.Status != "closed" {
		t.Fatalf("after the closing done %+v", it)
	}
}

// Checklists became sub-tasks: an add or edit that still sends one, from
// the app or an agent, is refused with the reason.
func TestChecklistIsRefused(t *testing.T) {
	h := newHarness(t, nil, false)
	h.pair()
	w := h.do("POST", "/api/actions", api.NewAction{ID: aid2, Kind: api.KindAdd, Bridge: store.HubBridge, Repo: store.PersonalRepo,
		Payload: api.Payload{Title: "Water the plants"}}, "")
	h.expect(w, http.StatusCreated)
	key := store.ItemKey(store.HubBridge, store.PersonalRepo, decode[api.Action](t, w).BeadID)
	checks := []any{map[string]any{"text": "Ferns", "done": false}}
	for name, tc := range map[string]struct {
		path, token string
		body        any
	}{
		"app edit": {"/api/actions", "", map[string]any{"id": cid3, "kind": "edit", "key": key,
			"payload": map[string]any{"set": map[string]any{"checklist": checks}}}},
		"app add": {"/api/actions", "", map[string]any{"id": cid3, "kind": "add", "bridge": "hub", "repo": "personal",
			"payload": map[string]any{"title": "Mow the lawn", "checklist": checks}}},
		"agent add": {"/bridge/v1/personal", h.token, map[string]any{"title": "Mow the lawn", "checklist": checks}},
		"agent edit": {"/bridge/v1/personal/mm-1/actions", h.token, map[string]any{"kind": "edit",
			"payload": map[string]any{"set": map[string]any{"checklist": []any{}}}}},
	} {
		w := h.do("POST", tc.path, tc.body, tc.token)
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "Checklists are now sub-tasks") {
			t.Errorf("%s: %d %s", name, w.Code, w.Body)
		}
	}
}
