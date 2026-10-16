package notify

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/yourfriendaaron/meat-mode/internal/api"
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
}

func TestSubscribeUpsertAndUnsubscribe(t *testing.T) {
	db := testDB(t)
	r, _ := NewRegistry(t.TempDir(), db, "mailto:alex@example.com")
	ctx := context.Background()
	s := newSub(t, "https://push.example.com/a")
	if err := r.Subscribe(ctx, s); err != nil {
		t.Fatal(err)
	}
	s2 := newSub(t, "https://push.example.com/a")
	if err := r.Subscribe(ctx, s2); err != nil {
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
		ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
			okHits.Add(1)
			ttl, urg, auth = q.Header.Get("TTL"), q.Header.Get("Urgency"), q.Header.Get("Authorization")
			w.WriteHeader(201)
		}))
		gone := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) { w.WriteHeader(code) }))
		bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) { w.WriteHeader(500) }))
		ctx := context.Background()
		for _, u := range []string{gone.URL, bad.URL, ok.URL} {
			if err := r.Subscribe(ctx, newSub(t, u)); err != nil {
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
	t.Setenv("MEAT_MODE_NTFY_URL", "")
	reg, n, err := FromEnv(t.TempDir(), testDB(t))
	if err != nil || reg == nil || len(n.(Multi)) != 1 {
		t.Fatal(err)
	}
	t.Setenv("MEAT_MODE_NTFY_URL", "https://ntfy.example.com")
	_, n, _ = FromEnv(t.TempDir(), testDB(t))
	if len(n.(Multi)) != 2 {
		t.Fatal("ntfy not enabled")
	}
}
