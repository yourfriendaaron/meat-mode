package notify

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/yourfriendaaron/meat-mode/internal/api"
	"github.com/yourfriendaaron/meat-mode/internal/store"
)

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func newSub(t *testing.T, endpoint string) api.PushSubscription {
	t.Helper()
	k, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	auth := make([]byte, 16)
	rand.Read(auth)
	var s api.PushSubscription
	s.Endpoint = endpoint
	s.Keys.P256dh = base64.RawURLEncoding.EncodeToString(k.PublicKey().Bytes())
	s.Keys.Auth = base64.RawURLEncoding.EncodeToString(auth)
	return s
}

func count(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM push_subscriptions`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestVAPIDKeysPersist(t *testing.T) {
	dir := t.TempDir()
	r1, err := NewRegistry(dir, testDB(t), "mailto:alex@example.com")
	if err != nil {
		t.Fatal(err)
	}
	r2, err := NewRegistry(dir, testDB(t), "mailto:alex@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if r1.PublicKey() == "" || r1.PublicKey() != r2.PublicKey() {
		t.Fatalf("keys differ: %q %q", r1.PublicKey(), r2.PublicKey())
	}
	fi, err := os.Stat(filepath.Join(dir, "vapid.json"))
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("vapid.json: %v %v", fi, err)
	}
	// Generation left no temp file behind, and a read-only-open existing file
	// is tightened rather than rewritten.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "vapid.json" {
		t.Fatalf("data dir holds %v", entries)
	}
	if err := os.Chmod(filepath.Join(dir, "vapid.json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewRegistry(dir, testDB(t), "mailto:alex@example.com"); err != nil {
		t.Fatal(err)
	}
	fi, err = os.Stat(filepath.Join(dir, "vapid.json"))
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("vapid.json after reopen: %v %v", fi, err)
	}
}

func TestNewRegistrySubject(t *testing.T) {
	for _, s := range []string{"mailto:alex@example.com", "https://example.com/contact"} {
		if _, err := NewRegistry(t.TempDir(), testDB(t), s); err != nil {
			t.Errorf("NewRegistry(%q) = %v", s, err)
		}
	}
	for _, s := range []string{"", "mailto:", "mailto:no-at-sign", "alex@example.com", "https://", "ftp://example.com/"} {
		_, err := NewRegistry(t.TempDir(), testDB(t), s)
		if err == nil || !strings.Contains(err.Error(), "MEAT_MODE_VAPID_SUBJECT") {
			t.Errorf("NewRegistry(%q) = %v, want an error naming MEAT_MODE_VAPID_SUBJECT", s, err)
		}
	}
}

func TestSubscribeUpsertAndUnsubscribe(t *testing.T) {
	db := testDB(t)
	r, _ := NewRegistry(t.TempDir(), db, "mailto:alex@example.com")
	ctx := context.Background()
	s := newSub(t, "https://updates.push.services.mozilla.com/wpush/v2/a")
	if err := r.Subscribe(ctx, "", s); err != nil {
		t.Fatal(err)
	}
	s2 := newSub(t, "https://updates.push.services.mozilla.com/wpush/v2/a")
	if err := r.Subscribe(ctx, "", s2); err != nil {
		t.Fatal(err)
	}
	if n := count(t, db); n != 1 {
		t.Fatalf("count %d", n)
	}
	var p string
	db.QueryRow(`SELECT p256dh FROM push_subscriptions`).Scan(&p)
	if p != s2.Keys.P256dh {
		t.Fatal("not upserted")
	}
	if err := r.Unsubscribe(ctx, s.Endpoint); err != nil || count(t, db) != 0 {
		t.Fatal("unsubscribe failed")
	}
}

func TestNotifyPrunesGoneAndContinues(t *testing.T) {
	for _, code := range []int{404, 410} {
		db := testDB(t)
		r, _ := NewRegistry(t.TempDir(), db, "mailto:alex@example.com")
		var okHits atomic.Int32
		var ttl, urg, auth string
		ok := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
			okHits.Add(1)
			ttl, urg, auth = q.Header.Get("TTL"), q.Header.Get("Urgency"), q.Header.Get("Authorization")
			w.WriteHeader(201)
		}))
		gone := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) { w.WriteHeader(code) }))
		bad := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) { w.WriteHeader(500) }))
		for _, srv := range []*httptest.Server{ok, gone, bad} {
			r.hosts = append(r.hosts, srv.Listener.Addr().String())
		}
		r.Client = ok.Client() // every httptest TLS server shares one certificate
		ctx := context.Background()
		for _, u := range []string{gone.URL, bad.URL, ok.URL} {
			if err := r.Subscribe(ctx, "", newSub(t, u)); err != nil {
				t.Fatal(err)
			}
		}
		err := r.Notify(ctx, Message{Title: "T", Body: "B", URL: "https://x.example.com", Tag: "k"})
		if err == nil {
			t.Fatal("want error for the 500")
		}
		if okHits.Load() != 1 || ttl != "86400" || urg != "high" || auth == "" {
			t.Fatalf("ok push: hits=%d ttl=%q urgency=%q auth=%q", okHits.Load(), ttl, urg, auth)
		}
		if n := count(t, db); n != 2 {
			t.Fatalf("code %d: %d subscriptions left, want 2", code, n)
		}
		var left int
		db.QueryRow(`SELECT count(*) FROM push_subscriptions WHERE endpoint = ?`, gone.URL).Scan(&left)
		if left != 0 {
			t.Fatalf("code %d: gone subscription kept", code)
		}
		ok.Close()
		gone.Close()
		bad.Close()
	}
}

func TestSubscribeOnlyKnownPushServices(t *testing.T) {
	db := testDB(t)
	r, _ := NewRegistry(t.TempDir(), db, "mailto:alex@example.com")
	ctx := context.Background()
	for u, ok := range map[string]bool{
		"https://updates.push.services.mozilla.com/wpush/v2/a": true,
		"https://fcm.googleapis.com/fcm/send/a":                true,
		"https://web.push.apple.com/a":                         true,
		"https://api.push.apple.com/a":                         true,
		"https://wns2-by3p.notify.windows.com/w/?token=a":      true,
		"https://FCM.googleapis.com:443/fcm/send/b":            true,
		"https://fcm.googleapis.com:8443/fcm/send/a":           false,
		"http://fcm.googleapis.com/fcm/send/a":                 false,
		"https://attacker.example.net/collect":                 false,
		"https://fcm.googleapis.com.example.net/a":             false,
		"https://notfcm.googleapis.com/a":                      false,
		"https://192.0.2.10/a":                                 false,
		"https://127.0.0.1:8080/api/actions":                   false,
		"fcm.googleapis.com/a":                                 false,
	} {
		err := r.Subscribe(ctx, "", newSub(t, u))
		if ok && err != nil {
			t.Errorf("Subscribe(%q) = %v", u, err)
		}
		if !ok && (err == nil || !errors.Is(err, store.ErrInvalid)) {
			t.Errorf("Subscribe(%q) = %v, want an invalid-request error", u, err)
		}
	}

	// A hostile row already in the table is never sent to.
	if _, err := db.Exec(`INSERT INTO push_subscriptions (endpoint, p256dh, auth) VALUES ('https://attacker.example.net/x', 'k', 'a')`); err != nil {
		t.Fatal(err)
	}
	r.Client = &http.Client{Transport: roundTrip(func(q *http.Request) (*http.Response, error) {
		if q.URL.Host == "attacker.example.net" {
			t.Errorf("sent to %s", q.URL)
		}
		return &http.Response{StatusCode: 201, Body: http.NoBody}, nil
	})}
	r.Notify(ctx, Message{Title: "Water the plants"})
}

func TestSubscribeKeepsTheNewest(t *testing.T) {
	db := testDB(t)
	r, _ := NewRegistry(t.TempDir(), db, "mailto:alex@example.com")
	ctx := context.Background()
	u := func(i int) string { return fmt.Sprintf("https://fcm.googleapis.com/fcm/send/%d", i) }
	for i := range MaxSubscriptions + 5 {
		if err := r.Subscribe(ctx, "", newSub(t, u(i))); err != nil {
			t.Fatal(err)
		}
		if i == 10 {
			// Renewing the first one makes it new again.
			if err := r.Subscribe(ctx, "", newSub(t, u(0))); err != nil {
				t.Fatal(err)
			}
		}
	}
	if n := count(t, db); n != MaxSubscriptions {
		t.Fatalf("%d subscriptions, want %d", n, MaxSubscriptions)
	}
	var n int
	db.QueryRow(`SELECT count(*) FROM push_subscriptions WHERE endpoint IN (?, ?)`, u(1), u(5)).Scan(&n)
	if n != 0 {
		t.Errorf("the oldest subscriptions were kept")
	}
	db.QueryRow(`SELECT count(*) FROM push_subscriptions WHERE endpoint = ?`, u(0)).Scan(&n)
	if n != 1 {
		t.Errorf("a renewed subscription was dropped")
	}
	db.QueryRow(`SELECT count(*) FROM push_subscriptions WHERE endpoint = ?`, u(MaxSubscriptions+4)).Scan(&n)
	if n != 1 {
		t.Errorf("the newest subscription was dropped")
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(q *http.Request) (*http.Response, error) { return f(q) }

func TestNtfyRequest(t *testing.T) {
	var got *http.Request
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
		got = q
		body, _ = io.ReadAll(q.Body)
	}))
	defer srv.Close()
	n := &Ntfy{URL: srv.URL + "/", Topic: "plants", Token: "tk_example"}
	err := n.Notify(context.Background(), Message{Title: "Water the plants", Body: "Due now", URL: "https://app.example.com/?item=a", Tag: "a/b/c"})
	if err != nil {
		t.Fatal(err)
	}
	h := got.Header
	if got.Method != "POST" || got.URL.Path != "/plants" || string(body) != "Due now" ||
		h.Get("Title") != "Water the plants" || h.Get("Click") != "https://app.example.com/?item=a" ||
		h.Get("Priority") != "4" || h.Get("Tags") != "a/b/c" || h.Get("Authorization") != "Bearer tk_example" {
		t.Fatalf("bad request: %v %s %v", got.URL, body, h)
	}
	n.Token = ""
	if err := n.Notify(context.Background(), Message{Title: "x"}); err != nil {
		t.Fatal(err)
	}
	if got.Header.Get("Authorization") != "" {
		t.Fatal("auth header sent without token")
	}

	// A bead title with control characters still sends, as one header line.
	err = n.Notify(context.Background(), Message{Title: "Water\r\nX-Injected: 1\tthe\x00plants\x7f", Body: "b",
		URL: "https://app.example.com/\n?item=a", Tag: "a\rb"})
	if err != nil {
		t.Fatal(err)
	}
	h = got.Header
	if h.Get("Title") != "Water  X-Injected: 1 the plants" || h.Get("X-Injected") != "" ||
		h.Get("Click") != "https://app.example.com/ ?item=a" || h.Get("Tags") != "a b" {
		t.Fatalf("control characters reached the headers: %q", h)
	}
}

type fake struct {
	err error
	n   int
}

func (f *fake) Notify(context.Context, Message) error { f.n++; return f.err }

func TestMultiTriesAll(t *testing.T) {
	a, b := &fake{err: errors.New("a")}, &fake{}
	err := Multi{a, b}.Notify(context.Background(), Message{})
	if err == nil || a.n != 1 || b.n != 1 {
		t.Fatal("multi")
	}
}

func TestFromEnv(t *testing.T) {
	t.Setenv("MEAT_MODE_VAPID_SUBJECT", "mailto:alex@example.com")
	t.Setenv("MEAT_MODE_NTFY_URL", "")
	t.Setenv("MEAT_MODE_NTFY_TOPIC", "")
	reg, n, err := FromEnv(t.TempDir(), testDB(t))
	if err != nil || reg == nil || len(n.(Multi)) != 1 {
		t.Fatal(err)
	}
	if reg.Client == nil || reg.Client.Timeout <= 0 {
		t.Fatal("the registry client has no timeout")
	}
	t.Setenv("MEAT_MODE_NTFY_URL", "https://ntfy.example.com")
	_, _, err = FromEnv(t.TempDir(), testDB(t))
	if err == nil || !strings.Contains(err.Error(), "MEAT_MODE_NTFY_TOPIC") {
		t.Fatalf("err = %v, want an error naming MEAT_MODE_NTFY_TOPIC", err)
	}
	t.Setenv("MEAT_MODE_NTFY_TOPIC", "plants")
	reg, n, err = FromEnv(t.TempDir(), testDB(t))
	if err != nil || len(n.(Multi)) != 2 {
		t.Fatal(err)
	}
	nt := n.(Multi)[1].(*Ntfy)
	if nt.Topic != "plants" || nt.Client == nil || nt.Client.Timeout <= 0 {
		t.Fatalf("ntfy = %+v; its client has no timeout", nt)
	}
}

// pushServer is a push service that counts deliveries per path.
func pushServer(t *testing.T, r *Registry) (*httptest.Server, func(path string) int32) {
	t.Helper()
	var mu sync.Mutex
	hits := map[string]int32{}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
		mu.Lock()
		hits[q.URL.Path]++
		mu.Unlock()
		w.WriteHeader(201)
	}))
	t.Cleanup(srv.Close)
	r.hosts = append(r.hosts, srv.Listener.Addr().String())
	r.Client = srv.Client()
	return srv, func(path string) int32 {
		mu.Lock()
		defer mu.Unlock()
		return hits[path]
	}
}

func endpoints(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.Query(`SELECT endpoint FROM push_subscriptions ORDER BY endpoint`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var e string
		rows.Scan(&e)
		out = append(out, e[strings.LastIndex(e, "/")+1:])
	}
	return out
}

func TestOldSubscriptionTableGainsColumns(t *testing.T) {
	db := testDB(t)
	if _, err := db.Exec(`CREATE TABLE push_subscriptions (
		endpoint TEXT PRIMARY KEY,
		p256dh   TEXT NOT NULL,
		auth     TEXT NOT NULL
	)`); err != nil {
		t.Fatal(err)
	}
	old := newSub(t, "https://updates.push.services.mozilla.com/wpush/v2/old")
	db.Exec(`INSERT INTO push_subscriptions VALUES (?, ?, ?)`, old.Endpoint, old.Keys.P256dh, old.Keys.Auth)
	before := time.Now().Unix()
	if _, err := NewRegistry(t.TempDir(), db, "mailto:alex@example.com"); err != nil {
		t.Fatal(err)
	}
	var session sql.NullString
	var renewed int64
	if err := db.QueryRow(`SELECT session_id, renewed_at FROM push_subscriptions`).Scan(&session, &renewed); err != nil {
		t.Fatal(err)
	}
	if session.Valid || renewed < before {
		t.Errorf("old row: session %v, renewed_at %d; want no session and renewed now (>= %d)", session, renewed, before)
	}
	// Opening again changes nothing.
	if _, err := NewRegistry(t.TempDir(), db, "mailto:alex@example.com"); err != nil {
		t.Fatal(err)
	}
	if n := count(t, db); n != 1 {
		t.Errorf("count %d after reopening", n)
	}
}

func TestSubscriptionsExpireWithoutRenewal(t *testing.T) {
	db := testDB(t)
	r, _ := NewRegistry(t.TempDir(), db, "mailto:alex@example.com")
	srv, hits := pushServer(t, r)
	now := time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC)
	r.now = func() time.Time { return now }
	ctx := context.Background()
	for _, p := range []string{"/stale", "/fresh"} {
		if err := r.Subscribe(ctx, "", newSub(t, srv.URL+p)); err != nil {
			t.Fatal(err)
		}
	}
	now = now.Add(SubscriptionTTL - time.Hour)
	r.Subscribe(ctx, "", newSub(t, srv.URL+"/fresh")) // the app opened again
	now = now.Add(2 * time.Hour)
	if err := r.Notify(ctx, Message{Title: "T"}); err != nil {
		t.Fatal(err)
	}
	if hits("/stale") != 0 || hits("/fresh") != 1 {
		t.Errorf("hits stale %d, fresh %d; want 0, 1", hits("/stale"), hits("/fresh"))
	}
	if got := endpoints(t, db); len(got) != 1 || got[0] != "fresh" {
		t.Errorf("left %v, want [fresh]", got)
	}
}

func TestSubscriptionsFollowTheirSession(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	db := st.DB()
	r, _ := NewRegistry(t.TempDir(), db, "mailto:alex@example.com")
	srv, hits := pushServer(t, r)
	now := time.Now()
	r.now = func() time.Time { return now }
	ctx := context.Background()

	// Registered before the login was turned on.
	if err := r.Subscribe(ctx, "", newSub(t, srv.URL+"/before")); err != nil {
		t.Fatal(err)
	}
	r.RequireSession = func() bool { return true }
	if err := r.Subscribe(ctx, "", newSub(t, srv.URL+"/anon")); !errors.Is(err, store.ErrUnauthorized) {
		t.Fatalf("subscribe without a session: %v, want ErrUnauthorized", err)
	}
	session := func(ttl time.Duration) (string, string) {
		tok, s, err := st.CreateSession("tag", now, ttl)
		if err != nil {
			t.Fatal(err)
		}
		return tok, s.ID
	}
	_, live := session(time.Hour)
	_, short := session(time.Minute)
	outTok, out := session(time.Hour)
	_, dropped := session(time.Hour)
	for path, id := range map[string]string{"/live": live, "/short": short, "/out": out, "/dropped": dropped, "/made-up": "00ff"} {
		if err := r.Subscribe(ctx, id, newSub(t, srv.URL+path)); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.DeleteSession(outTok); err != nil { // logged out elsewhere
		t.Fatal(err)
	}
	if err := r.DropSession(ctx, dropped); err != nil {
		t.Fatal(err)
	}
	if got := endpoints(t, db); strings.Join(got, ",") != "before,live,made-up,out,short" {
		t.Fatalf("after DropSession: %v", got)
	}
	now = now.Add(2 * time.Minute) // the short session has expired
	if err := r.Notify(ctx, Message{Title: "T"}); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"/before", "/short", "/out", "/dropped", "/made-up"} {
		if hits(p) != 0 {
			t.Errorf("%s got a push", p)
		}
	}
	if hits("/live") != 1 {
		t.Errorf("/live got %d pushes, want 1", hits("/live"))
	}
	if got := endpoints(t, db); len(got) != 1 || got[0] != "live" {
		t.Errorf("left %v, want [live]", got)
	}
}

func TestTestReminderReachesOnlyTheSessionAndNtfy(t *testing.T) {
	db := testDB(t)
	r, _ := NewRegistry(t.TempDir(), db, "mailto:alex@example.com")
	_, hits := pushServer(t, r)
	ctx := context.Background()
	mine := newSub(t, "https://"+r.hosts[len(r.hosts)-1]+"/mine")
	other := newSub(t, "https://"+r.hosts[len(r.hosts)-1]+"/other")
	if err := r.Subscribe(ctx, "", mine); err != nil {
		t.Fatal(err)
	}
	r.RequireSession = nil
	if got := r.TestReminder(ctx, "", "https://meat.example.com"); got.Push != "sent" || got.Ntfy != "off" {
		t.Fatalf("got %+v", got)
	}
	if hits("/mine") != 1 {
		t.Fatalf("own subscription got %d, want 1", hits("/mine"))
	}

	// A subscription of another session is left alone.
	if _, err := db.Exec(`UPDATE push_subscriptions SET session_id = 'aa'`); err != nil {
		t.Fatal(err)
	}
	if err := r.Subscribe(ctx, "", other); err != nil {
		t.Fatal(err)
	}
	ntfy := &fake{}
	r.Ntfy = ntfy
	if got := r.TestReminder(ctx, "", ""); got.Push != "sent" || got.Ntfy != "sent" {
		t.Fatalf("got %+v", got)
	}
	if hits("/mine") != 1 || hits("/other") != 1 || ntfy.n != 1 {
		t.Fatalf("mine %d other %d ntfy %d, want 1 1 1", hits("/mine"), hits("/other"), ntfy.n)
	}
	if got := r.TestReminder(ctx, "bb", ""); got.Push != "none" {
		t.Fatalf("session with no subscription: %+v", got)
	}

	ntfy.err = errors.New("down")
	if got := r.TestReminder(ctx, "", ""); got.Ntfy != "failed" {
		t.Fatalf("got %+v", got)
	}
}

func TestTestReminderToAGoneSubscription(t *testing.T) {
	for _, code := range []int{404, 410} {
		db := testDB(t)
		r, _ := NewRegistry(t.TempDir(), db, "mailto:alex@example.com")
		var hits atomic.Int32
		gone := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
			hits.Add(1)
			w.WriteHeader(code)
		}))
		r.hosts = append(r.hosts, gone.Listener.Addr().String())
		r.Client = gone.Client()
		ctx := context.Background()
		if err := r.Subscribe(ctx, "", newSub(t, gone.URL+"/stale")); err != nil {
			t.Fatal(err)
		}
		if got := r.TestReminder(ctx, "", ""); got.Push != "expired" {
			t.Fatalf("code %d: got %+v, want push expired", code, got)
		}
		if hits.Load() != 1 || count(t, db) != 0 {
			t.Fatalf("code %d: %d pushes, %d subscriptions left; want 1, 0", code, hits.Load(), count(t, db))
		}
		gone.Close()
	}
}

func TestTestReminderToADisallowedEndpoint(t *testing.T) {
	db := testDB(t)
	r, _ := NewRegistry(t.TempDir(), db, "mailto:alex@example.com")
	// Stored before endpoints were checked.
	old := newSub(t, "https://push.example.com/send/old")
	if _, err := db.Exec(`INSERT INTO push_subscriptions (endpoint, p256dh, auth, renewed_at) VALUES (?, ?, ?, ?)`,
		old.Endpoint, old.Keys.P256dh, old.Keys.Auth, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	if got := r.TestReminder(context.Background(), "", ""); got.Push != "expired" {
		t.Fatalf("got %+v, want push expired", got)
	}
	if n := count(t, db); n != 0 {
		t.Fatalf("%d subscriptions left, want 0", n)
	}
}
