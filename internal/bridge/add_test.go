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
	if got := strings.TrimSpace(out.String()); got != "mm-1" {
		t.Fatalf("printed %q, want the id", got)
	}
	// Retrying with the same id names the same item and adds nothing.
	out.Reset()
	if err := Add(context.Background(), args, env); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(out.String()); got != "mm-1" {
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

// A machine in UTC adds a date and it lands at 9:00 in the hub's zone.
func TestAddReadsDatesInTheHubsTimeZone(t *testing.T) {
	saved := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = saved })

	hub := fakehub.New(map[string]string{token: us})
	defer hub.Close()
	hub.SetTimeZone("America/New_York")
	env, _, _ := testEnv(t, "")
	if err := (Config{HubURL: hub.URL, Token: token}).Save(env.Paths.Config); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, args := range [][]string{
		{"Renew the library card", "--due", "2026-03-02"},
		{"Water the plants", "--due", "2026-03-02 09:00"},
		{"Buy stamps", "--due", "2026-03-02T14:00:00Z"}, // an explicit offset is kept
	} {
		if err := Add(ctx, args, env); err != nil {
			t.Fatal(err)
		}
	}
	// 9:00 in New York on 2 March 2026 (UTC-5) is 14:00 UTC.
	want := time.Date(2026, 3, 2, 14, 0, 0, 0, time.UTC)
	for _, p := range hub.Personals() {
		if p.Due == nil || !p.Due.Equal(want) {
			t.Errorf("%s due %v, want %v", p.Title, p.Due, want)
		}
	}
	if n := len(hub.Hellos()); n != 0 {
		t.Errorf("add said hello %d times; that would show the machine as connected", n)
	}

	// Until the hub has a zone, this machine's stands in.
	hub.SetTimeZone("")
	if err := Add(ctx, []string{"Call Sam", "--due", "2026-03-03"}, env); err != nil {
		t.Fatal(err)
	}
	ps := hub.Personals()
	if got, want := ps[len(ps)-1].Due, time.Date(2026, 3, 3, 9, 0, 0, 0, time.UTC); got == nil || !got.Equal(want) {
		t.Errorf("without a hub zone: %v, want %v", got, want)
	}
}

// A hub older than the bridge has no settings route; this machine's zone
// stands in, as it does while the hub has none.
func TestAddWithDueOnAHubWithoutSettings(t *testing.T) {
	saved := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = saved })

	hub := fakehub.New(map[string]string{token: us})
	defer hub.Close()
	hub.SetTimeZone("America/New_York")
	hub.SetNoSettings(true)
	env, _, _ := testEnv(t, "")
	if err := (Config{HubURL: hub.URL, Token: token}).Save(env.Paths.Config); err != nil {
		t.Fatal(err)
	}
	if err := Add(context.Background(), []string{"Water the plants", "--due", "2026-03-02"}, env); err != nil {
		t.Fatal(err)
	}
	ps := hub.Personals()
	if want := time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC); len(ps) != 1 || ps[0].Due == nil || !ps[0].Due.Equal(want) {
		t.Fatalf("got %+v, want one due %v", ps, want)
	}
}
