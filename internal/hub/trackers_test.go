package hub

import (
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

// A repo's tracker and actions from the bridge reach the app's state, and an
// action the tracker cannot apply is refused with 400 and its reason.
func TestTrackerOverHTTP(t *testing.T) {
	h := newHarness(t, nil, false)
	h.pair()
	h.expect(h.do("POST", "/bridge/v1/hello", api.Hello{Version: "1.0.0",
		Trackers: []api.TrackerVersion{{Name: "ticket", Version: "0.3.2"}}}, h.token), http.StatusOK)
	h.expect(h.do("PUT", "/bridge/v1/repos", []api.Repo{
		{Name: "garden", OptedIn: true, Tracker: "ticket", Actions: []string{api.KindDone, api.KindAdd}},
	}, h.token), http.StatusNoContent)

	st := decode[api.State](t, h.do("GET", "/api/state", nil, ""))
	var garden api.Repo
	for _, r := range st.Repos {
		if r.Bridge == "laptop" && r.Name == "garden" {
			garden = r
		}
	}
	if garden.Tracker != "ticket" || !slices.Equal(garden.Actions, []string{"done", "add"}) {
		t.Fatalf("garden %+v", garden)
	}
	if len(st.Bridges) != 1 || !slices.Equal(st.Bridges[0].Trackers, []api.TrackerVersion{{Name: "ticket", Version: "0.3.2"}}) {
		t.Fatalf("bridges %+v", st.Bridges)
	}

	until := time.Now().Add(time.Hour)
	w := h.do("POST", "/api/actions", api.NewAction{ID: aid1, Kind: api.KindSnooze, Key: "laptop/garden/gd-1",
		Payload: api.Payload{Until: &until}}, "")
	h.expect(w, http.StatusBadRequest)
	if e := decode[map[string]string](t, w); e["error"] != "ticket cannot snooze" {
		t.Errorf("error %q", e["error"])
	}
	h.expect(h.do("POST", "/api/actions", api.NewAction{ID: aid2, Kind: api.KindDone, Key: "laptop/garden/gd-1"}, ""), http.StatusCreated)
}
