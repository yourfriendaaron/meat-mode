package hub

import (
	"net/http"
	"testing"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

func TestRunsOverHTTP(t *testing.T) {
	h := newHarness(t, nil, false)
	h.pair()
	key := "laptop/garden/gd-1"
	ask := api.NewAction{ID: "0b6f4a8e-1c2d-4e5f-8a9b-0c1d2e3f4a61", Kind: api.KindAsk, Key: key, Payload: api.Payload{Text: "Use the can"}}

	// Agents are off until the bridge reports them on.
	h.expect(h.do("POST", "/api/actions", ask, ""), http.StatusBadRequest)
	h.expect(h.do("PUT", "/bridge/v1/repos", []api.Repo{{Name: "garden", OptedIn: true, AgentsAllowed: true}}, h.token), http.StatusNoContent)
	st := decode[api.State](t, h.do("GET", "/api/state", nil, ""))
	for _, r := range st.Repos {
		if r.Name == "garden" && !r.AgentsAllowed {
			t.Fatalf("repo %+v", r)
		}
	}
	h.expect(h.do("POST", "/api/actions", ask, ""), http.StatusCreated)

	// The bridge's run list shows on the item; a bad one is refused.
	started := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)
	h.expect(h.do("PUT", "/bridge/v1/runs", []api.RunReport{{Repo: "garden", BeadID: "gd-1",
		Run: api.Run{ID: "r1", State: "idle", StartedAt: started}}}, h.token), http.StatusBadRequest)
	h.expect(h.do("PUT", "/bridge/v1/runs", []api.RunReport{{Repo: "garden", BeadID: "gd-1"}}, ""), http.StatusUnauthorized)
	h.expect(h.do("PUT", "/bridge/v1/runs", []api.RunReport{
		{Repo: "garden", BeadID: "gd-1", Run: api.Run{ID: "r1", State: api.RunWorking, Agent: "planner", Activity: "Filling the can", StartedAt: started, Count: 1}},
	}, h.token), http.StatusNoContent)
	st = decode[api.State](t, h.do("GET", "/api/state", nil, ""))
	r := st.Items[0].Run
	// The pending ask's note joins the live run as queued.
	if r == nil || r.ID != "r1" || r.State != api.RunWorking || r.Activity != "Filling the can" || r.Queued != 1 {
		t.Fatalf("run %+v", r)
	}
	// The route records the bridge as seen, which keeps the run from
	// showing offline.
	bs := decode[[]api.Bridge](t, h.do("GET", "/api/bridges", nil, ""))
	if len(bs) != 1 || bs[0].LastSeen == nil || time.Since(*bs[0].LastSeen) > time.Minute {
		t.Fatalf("bridges %+v", bs)
	}

	// Stop takes the item; ask on the personal list is refused.
	h.expect(h.do("POST", "/api/actions", api.NewAction{ID: "0b6f4a8e-1c2d-4e5f-8a9b-0c1d2e3f4a62", Kind: api.KindStop, Key: key}, ""), http.StatusCreated)
	st = decode[api.State](t, h.do("GET", "/api/state", nil, ""))
	if r := st.Items[0].Run; r.State != api.RunStopped || r.Reason != api.ReasonStopped {
		t.Fatalf("run after stop %+v", r)
	}
	w := h.do("POST", "/api/actions", api.NewAction{ID: "0b6f4a8e-1c2d-4e5f-8a9b-0c1d2e3f4a63", Kind: api.KindAdd,
		Bridge: "hub", Repo: "personal", Payload: api.Payload{Title: "Call Sam"}}, "")
	h.expect(w, http.StatusCreated)
	added := decode[api.Action](t, w)
	h.expect(h.do("POST", "/api/actions", api.NewAction{ID: "0b6f4a8e-1c2d-4e5f-8a9b-0c1d2e3f4a64", Kind: api.KindAsk,
		Key: "hub/personal/" + added.BeadID}, ""), http.StatusBadRequest)
}
