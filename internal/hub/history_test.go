package hub

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/yourfriendaaron/meat-mode/internal/api"
	"github.com/yourfriendaaron/meat-mode/internal/store"
)

func TestItemHistory(t *testing.T) {
	h := newHarness(t, nil, false)
	h.pair()
	hist := func(key, query string) []api.Action {
		t.Helper()
		w := h.do("GET", "/api/items/"+url.PathEscape(key)+"/history"+query, nil, "")
		h.expect(w, http.StatusOK)
		return decode[[]api.Action](t, w)
	}

	// The user adds a to-do; an agent renames it and adds a sub-task under it.
	w := h.do("POST", "/api/actions", api.NewAction{ID: aid1, Kind: api.KindAdd, Bridge: store.HubBridge, Repo: store.PersonalRepo,
		Payload: api.Payload{Title: "Water the plants"}}, "")
	h.expect(w, http.StatusCreated)
	id := decode[api.Action](t, w).BeadID
	key := store.HubBridge + "/" + store.PersonalRepo + "/" + id
	h.expect(h.do("POST", "/bridge/v1/personal/"+id+"/actions", api.PersonalChange{ID: cid1, Kind: api.KindEdit,
		Payload: api.Payload{Set: &api.Edit{Title: new("Water the ferns")}}, Author: "gardener"}, h.token), http.StatusCreated)
	w = h.do("POST", "/bridge/v1/personal", api.PersonalTodo{Title: "Buy soil", Parent: id, Author: "gardener"}, h.token)
	h.expect(w, http.StatusCreated)
	sub := decode[api.PersonalTodoReply](t, w).ID
	// Another item's change stays out.
	h.expect(h.do("POST", "/api/actions", api.NewAction{ID: aid2, Kind: api.KindAdd, Bridge: store.HubBridge, Repo: store.PersonalRepo,
		Payload: api.Payload{Title: "Take the bins out"}}, ""), http.StatusCreated)

	as := hist(key, "")
	if len(as) != 3 {
		t.Fatalf("history %+v", as)
	}
	if a := as[0]; a.Kind != api.KindAdd || a.BeadID != sub || a.Payload.Parent != id || a.Machine != "laptop" {
		t.Errorf("newest %+v, want the sub-task's add", a)
	}
	if a := as[1]; a.Kind != api.KindEdit || a.Author != "gardener" || a.Machine != "laptop" || a.Payload.Was == nil || *a.Payload.Was.Title != "Water the plants" {
		t.Errorf("edit %+v", a)
	}
	if a := as[2]; a.Kind != api.KindAdd || a.ID != aid1 || a.Machine != "" {
		t.Errorf("oldest %+v, want the app's add", a)
	}
	if got := hist(key, "?limit=1"); len(got) != 1 || got[0].Seq != as[0].Seq {
		t.Errorf("limit 1: %+v", got)
	}
	// The sub-task's own history holds its add only.
	if got := hist(store.HubBridge+"/"+store.PersonalRepo+"/"+sub, ""); len(got) != 1 || got[0].Kind != api.KindAdd {
		t.Errorf("sub-task history %+v", got)
	}
	// A tracker's item has a history too: its pending actions.
	h.expect(h.do("POST", "/api/actions", api.NewAction{ID: cid2, Kind: api.KindDone, Key: "laptop/garden/gd-1"}, ""), http.StatusCreated)
	if got := hist("laptop/garden/gd-1", ""); len(got) != 1 || got[0].Status != api.StatusPending {
		t.Errorf("tracker item history %+v", got)
	}

	h.expect(h.do("GET", "/api/items/"+url.PathEscape(store.HubBridge+"/"+store.PersonalRepo+"/mm-99")+"/history", nil, ""), http.StatusNotFound)
	h.expect(h.do("GET", "/api/items/not-a-key/history", nil, ""), http.StatusBadRequest)
	h.expect(h.do("GET", "/api/items/"+url.PathEscape(key)+"/history?limit=0", nil, ""), http.StatusBadRequest)
	// It needs a login, as every app route does.
	h.cookie = ""
	h.expect(h.do("GET", "/api/items/"+url.PathEscape(key)+"/history", nil, ""), http.StatusUnauthorized)
}
