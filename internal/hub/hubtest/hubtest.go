// Package hubtest runs a real hub for other packages' tests: a hub.Server on
// a fresh store behind httptest, with a password, and invented items and
// epic rows to read. It uses only the hub's and the store's exported APIs.
package hubtest

import (
	"encoding/base64"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"golang.org/x/crypto/argon2"

	"github.com/yourfriendaaron/meat-mode/internal/api"
	"github.com/yourfriendaaron/meat-mode/internal/hub"
	"github.com/yourfriendaaron/meat-mode/internal/store"
)

// Password is the hub's password unless Options.NoPassword.
const Password = "water the plants"

// The invented data Seed adds.
const (
	Bridge        = "laptop" // the paired bridge
	Repo          = "garden" // a repo with its epics on
	OtherRepo     = "chores" // a repo that is not synced
	HumanKey      = Bridge + "/" + Repo + "/gd-1"
	PlainKey      = Bridge + "/" + Repo + "/gd-2"
	HumanTitle    = "Water the plants"
	PlainTitle    = "Fix the fence"
	PersonalTitle = "Feed the cat"
	EpicID        = "gd-77"
)

// Options changes the hub New starts.
type Options struct {
	// NoPassword starts a hub that has no password yet.
	NoPassword bool
	// PublicURL is MEAT_MODE_PUBLIC_URL; an https:// one makes the hub
	// name its cookie __Host-meat_mode_session. The hub still answers on
	// 127.0.0.1.
	PublicURL string
}

// Hub is a running test hub. Its URL is http://127.0.0.1:<port>.
type Hub struct {
	URL    string
	Store  *store.Store
	Server *hub.Server
	HTTP   *httptest.Server

	// PersonalKey is the key of the personal to-do Seed adds.
	PersonalKey string
}

// New starts a hub that stops when the test ends.
func New(t testing.TB, o Options) *Hub {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg := hub.Config{User: "alex", PublicURL: o.PublicURL}
	if !o.NoPassword {
		cfg.PasswordHash = CheapHash(Password)
	}
	srv := hub.NewServer(cfg, st, nil)
	ts := httptest.NewServer(srv)
	// Cleanups run last first: streams end before the listener closes.
	t.Cleanup(func() { st.Close() })
	t.Cleanup(ts.Close)
	t.Cleanup(srv.Close)
	return &Hub{URL: ts.URL, Store: st, Server: srv, HTTP: ts}
}

// CheapHash is a PHC Argon2id hash of password with tiny parameters, so
// tests stay fast; the hub takes any parameters within its bounds.
func CheapHash(password string) string {
	salt := []byte("saltsaltsaltsalt")
	key := argon2.IDKey([]byte(password), salt, 1, 64, 1, 32)
	return fmt.Sprintf("$argon2id$v=19$m=64,t=1,p=1$%s$%s",
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key))
}

// Seed pairs Bridge with Repo (synced, epics on) and OtherRepo (not
// synced), gives Repo two items, HumanKey with the human label and
// PlainKey without, adds a personal to-do, and sets Repo's epic rows.
func (h *Hub) Seed(t testing.TB) {
	t.Helper()
	if _, err := h.Store.CreateBridge(Bridge); err != nil {
		t.Fatal(err)
	}
	if err := h.Store.ReplaceRepos(Bridge, []api.Repo{
		{Name: Repo, OptedIn: true, Epics: true, EpicsOn: true},
		{Name: OtherRepo},
	}); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 2, 1, 9, 0, 0, 0, time.UTC)
	if err := h.Store.ReplaceItems(Bridge, Repo, []api.Item{
		{ID: "gd-1", Title: HumanTitle, Status: "open", Priority: 2, Labels: []string{api.HumanLabel}, CreatedAt: at, UpdatedAt: at},
		{ID: "gd-2", Title: PlainTitle, Status: "in_progress", Priority: 1, CreatedAt: at, UpdatedAt: at},
	}); err != nil {
		t.Fatal(err)
	}
	key, _, err := h.Store.AddPersonal(api.PersonalTodo{Title: PersonalTitle, Note: "The bowl by the door."}, Bridge, "")
	if err != nil {
		t.Fatal(err)
	}
	h.PersonalKey = key
	h.SetEpics(t, EpicRows())
}

// EpicRows is an invented epic set for Repo: an epic with a sub-task and
// the human item, and a row on its own.
func EpicRows() []api.EpicRow {
	at := time.Date(2026, 2, 1, 9, 0, 0, 0, time.UTC)
	return []api.EpicRow{
		{ID: EpicID, Title: "Plan the beds", Type: "epic", Status: "open", Priority: 1, Epic: true, Description: "Spring beds.", UpdatedAt: at},
		{ID: EpicID + ".1", Title: "Dig the beds", Status: "open", Priority: 2, Parent: EpicID, Assignee: "sam", UpdatedAt: at},
		{ID: "gd-1", Title: HumanTitle, Status: "open", Priority: 2, Parent: EpicID, Human: true, UpdatedAt: at},
		{ID: "gd-78", Title: "Mend the gate", Status: "in_progress", Priority: 3, UpdatedAt: at},
	}
}

// SetEpics replaces Repo's epic rows, which sends `event: epics`.
func (h *Hub) SetEpics(t testing.TB, rows []api.EpicRow) {
	t.Helper()
	if err := h.Store.ReplaceEpics(Bridge, Repo, rows); err != nil {
		t.Fatal(err)
	}
}

// Touch changes the state, which sends `event: change`: it adds a
// personal to-do titled title.
func (h *Hub) Touch(t testing.TB, title string) {
	t.Helper()
	if _, _, err := h.Store.AddPersonal(api.PersonalTodo{Title: title}, Bridge, ""); err != nil {
		t.Fatal(err)
	}
}

// EndSession ends the session whose cookie value is cookie, as a logout
// elsewhere or a password change would. An open event stream of it ends at
// the hub's next heartbeat; DropStreams ends it at once.
func (h *Hub) EndSession(t testing.TB, cookie string) {
	t.Helper()
	if err := h.Store.DeleteSession(cookie); err != nil {
		t.Fatal(err)
	}
}

// DropStreams closes every open connection, event streams included, as a
// network drop would. The hub keeps answering new ones.
func (h *Hub) DropStreams() { h.HTTP.CloseClientConnections() }
