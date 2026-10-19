package hub

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

func TestOccurrences(t *testing.T) {
	h := newHarness(t, nil, false)
	get := func(from, to string) *httptest.ResponseRecorder {
		q := url.Values{}
		if from != "" {
			q.Set("from", from)
		}
		if to != "" {
			q.Set("to", to)
		}
		return h.do("GET", "/api/occurrences?"+q.Encode(), nil, "")
	}

	if got := decode[api.Occurrences](t, get("2030-03-01T00:00:00Z", "2030-03-08T00:00:00Z")); got.Occurrences == nil || len(got.Occurrences) != 0 {
		t.Fatalf("an empty hub lists %+v, want []", got.Occurrences)
	}
	if body := get("2030-03-01T00:00:00Z", "2030-03-08T00:00:00Z").Body.String(); body != "{\"occurrences\":[]}\n" {
		t.Fatalf("empty body %q", body)
	}

	due := time.Date(2030, 3, 2, 18, 30, 0, 0, time.UTC)
	w := h.do("POST", "/api/actions", api.NewAction{ID: uuid.NewString(), Kind: api.KindAdd, Bridge: "hub", Repo: "personal",
		Payload: api.Payload{Title: "Water the plants", Due: &due, Repeat: &api.Repeat{Every: api.EveryDay, TimeZone: "UTC"}}}, "")
	h.expect(w, http.StatusCreated)
	key := decode[api.Action](t, w)
	got := decode[api.Occurrences](t, get("2030-03-02T00:00:00Z", "2030-03-05T00:00:00+00:00"))
	if len(got.Occurrences) != 2 || got.Occurrences[0].Key != "hub/personal/"+key.BeadID ||
		!got.Occurrences[0].At.Equal(due.AddDate(0, 0, 1)) || !got.Occurrences[1].At.Equal(due.AddDate(0, 0, 2)) {
		t.Fatalf("occurrences = %+v", got.Occurrences)
	}
	// Times in another offset are read as the same moments.
	if got := decode[api.Occurrences](t, get("2030-03-02T12:00:00-05:00", "2030-03-04T14:00:00-05:00")); len(got.Occurrences) != 2 {
		t.Fatalf("offset window = %+v", got.Occurrences)
	}

	for name, c := range map[string][2]string{
		"no from":        {"", "2030-03-08T00:00:00Z"},
		"no to":          {"2030-03-01T00:00:00Z", ""},
		"not a time":     {"yesterday", "2030-03-08T00:00:00Z"},
		"a date":         {"2030-03-01", "2030-03-08"},
		"to equals from": {"2030-03-01T00:00:00Z", "2030-03-01T00:00:00Z"},
		"to before from": {"2030-03-08T00:00:00Z", "2030-03-01T00:00:00Z"},
		"over 62 days":   {"2030-03-01T00:00:00Z", "2030-05-03T00:00:01Z"},
	} {
		if w := get(c[0], c[1]); w.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s, want 400", name, w.Code, w.Body)
		}
	}
	h.expect(get("2030-03-01T00:00:00Z", "2030-05-02T00:00:00Z"), http.StatusOK)
}

func TestOccurrencesNeedLogin(t *testing.T) {
	h := newLoginHarness(t, Config{PasswordHash: cheapHash("water the plants")})
	path := "/api/occurrences?from=2030-03-01T00:00:00Z&to=2030-03-08T00:00:00Z"
	if w := h.do("GET", path, "", ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("without a session: %d", w.Code)
	}
	cookie := h.login("water the plants")
	if w := h.do("GET", path, "", cookie.Value); w.Code != http.StatusOK {
		t.Fatalf("with a session: %d %s", w.Code, w.Body)
	}
}
