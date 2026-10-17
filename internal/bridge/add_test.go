package bridge

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/bridge/fakehub"
)

const todoID = "6f1c2b8e-3d4a-4e5f-9a0b-1c2d3e4f5a6b"

func TestAddUsesBridgeConfig(t *testing.T) {
	hub := fakehub.New(map[string]string{token: us})
	defer hub.Close()
	env, _, out := testEnv(t, "")
	if err := (Config{HubURL: hub.URL, Token: token}).Save(env.Paths.Config); err != nil {
		t.Fatal(err)
	}
	args := []string{"Renew the library card", "--due", "2026-03-02T09:00:00Z",
		"--note", "It expires on Friday.", "--author", "planner", "--id", todoID}
	if err := Add(context.Background(), args, env); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if got := strings.TrimSpace(out.String()); got != "hub/personal/mm-1" {
		t.Fatalf("printed %q, want the key", got)
	}
	// Retrying with the same id names the same item and adds nothing.
	out.Reset()
	if err := Add(context.Background(), args, env); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(out.String()); got != "hub/personal/mm-1" {
		t.Fatalf("retry printed %q", got)
	}
	ps := hub.Personals()
	if len(ps) != 1 {
		t.Fatalf("personal = %+v", ps)
	}
	p := ps[0]
	due := time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC)
	if p.Bridge != us || p.Title != "Renew the library card" || p.Due == nil || !p.Due.Equal(due) ||
		p.Note != "It expires on Friday." || p.Author != "planner" || p.ID != todoID {
		t.Fatalf("todo = %+v", p)
	}
	// Flags may come first, and the title may be several words unquoted.
	out.Reset()
	if err := Add(context.Background(), []string{"--note", "Two", "Buy", "stamps"}, env); err != nil {
		t.Fatal(err)
	}
	if ps := hub.Personals(); len(ps) != 2 || ps[1].Title != "Buy stamps" || ps[1].Due != nil || ps[1].Author != "" {
		t.Fatalf("personal = %+v", ps)
	}
}

func TestAddRefuses(t *testing.T) {
	hub := fakehub.New(map[string]string{token: us})
	defer hub.Close()
	env, _, _ := testEnv(t, "")
	ctx := context.Background()
	if err := Add(ctx, []string{"Water the plants"}, env); err == nil || !strings.Contains(err.Error(), "not paired") {
		t.Errorf("unpaired: %v", err)
	}
	if err := (Config{HubURL: hub.URL, Token: "wrong"}).Save(env.Paths.Config); err != nil {
		t.Fatal(err)
	}
	if err := Add(ctx, []string{"Water the plants"}, env); err == nil || !strings.Contains(err.Error(), "token") {
		t.Errorf("bad token: %v", err)
	}
	if err := Add(ctx, []string{}, env); err == nil || !strings.Contains(err.Error(), "title") {
		t.Errorf("no title: %v", err)
	}
	if err := Add(ctx, []string{"Water the plants", "--due", "soon"}, env); err == nil || !strings.Contains(err.Error(), "--due") {
		t.Errorf("bad due: %v", err)
	}
	if len(hub.Personals()) != 0 {
		t.Error("a refused add reached the hub")
	}
}

func TestParseDue(t *testing.T) {
	loc := time.FixedZone("test", -5*3600)
	for in, want := range map[string]time.Time{
		"2026-03-02T09:00:00Z":      time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC),
		"2026-03-02T09:00:00+02:00": time.Date(2026, 3, 2, 7, 0, 0, 0, time.UTC),
		"2026-03-02 18:30":          time.Date(2026, 3, 2, 23, 30, 0, 0, time.UTC),
		"2026-03-02T18:30":          time.Date(2026, 3, 2, 23, 30, 0, 0, time.UTC),
		"2026-03-02":                time.Date(2026, 3, 2, 14, 0, 0, 0, time.UTC),
	} {
		got, err := ParseDue(in, loc)
		if err != nil || !got.Equal(want) {
			t.Errorf("ParseDue(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"", "tomorrow", "03/02/2026"} {
		if _, err := ParseDue(in, loc); err == nil {
			t.Errorf("ParseDue(%q) took it", in)
		}
	}
}
