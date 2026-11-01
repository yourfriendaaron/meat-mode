package store

import (
	"bytes"
	"database/sql"
	"errors"
	"testing"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

const testSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1 1"><rect width="1" height="1"/></svg>`

// colourOf is a repo row's colour slot, -1 for none.
func colourOf(t *testing.T, s *Store, bridge, name string) int {
	t.Helper()
	var c sql.NullInt64
	must(t, s.db.QueryRow(`SELECT colour FROM repos WHERE bridge = ? AND name = ?`, bridge, name).Scan(&c))
	if !c.Valid {
		return -1
	}
	return int(c.Int64)
}

// iconOf is the icon hash State gives a repo, "" for none.
func iconOf(t *testing.T, s *Store, bridge, name string) string {
	t.Helper()
	for _, r := range mustState(t, s).Repos {
		if r.Bridge == bridge && r.Name == name {
			return r.Icon
		}
	}
	return ""
}

func TestMigrationEighteenGivesColours(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	must(t, err)
	for _, b := range []string{"laptop", "desk"} {
		_, err := s.CreateBridge(b)
		must(t, err)
	}
	must(t, s.ReplaceRepos("laptop", []api.Repo{
		{Name: "projects/Zebra"}, {Name: "b/apple"}, {Name: "chores"}, {Name: PersonalRepo, Personal: true},
	}))
	must(t, s.ReplaceRepos("desk", []api.Repo{{Name: "garden"}}))
	must(t, s.RevokeBridge("desk"))
	_, err = s.db.Exec(`INSERT INTO repos (bridge, name, personal, opted_in, hidden) VALUES (?, 'shed', 0, 1, 1)`, HubBridge)
	must(t, err)
	_, err = s.db.Exec(`DROP TABLE repo_icons; ALTER TABLE repos DROP COLUMN colour; PRAGMA user_version = 17`)
	must(t, err)
	must(t, s.Close())

	s, err = Open(dir)
	must(t, err)
	defer s.Close()
	// The personal list first, then by the name's last segment without
	// regard to case. A revoked bridge's repo and a hidden project get a
	// slot but hold none; a bridge's own personal repo gets none.
	for _, c := range []struct {
		bridge, name string
		want         int
	}{
		{HubBridge, PersonalRepo, 0},
		{"laptop", "b/apple", 1},
		{"laptop", "chores", 2},
		{"desk", "garden", 3},
		{HubBridge, "shed", 3},
		{"laptop", "projects/Zebra", 3},
		{"laptop", PersonalRepo, -1},
	} {
		if got := colourOf(t, s, c.bridge, c.name); got != c.want {
			t.Errorf("%s/%s colour %d, want %d", c.bridge, c.name, got, c.want)
		}
	}
	if _, err := s.db.Exec(`INSERT INTO repo_icons (bridge, repo, hash, type, data) VALUES ('laptop', 'chores', 'h', 't', x'00')`); err != nil {
		t.Errorf("repo_icons after the migration: %v", err)
	}
}

func TestReposKeepTheirColours(t *testing.T) {
	s, _ := open(t)
	_, err := s.CreateBridge("laptop")
	must(t, err)
	five := 5
	// New repos get slots in name order; a report's colour and icon are
	// ignored.
	must(t, s.ReplaceRepos("laptop", []api.Repo{{Name: "garden", Colour: &five, Icon: "0123456789abcdef0123456789abcdef"}, {Name: "chores"}}))
	if c, g := colourOf(t, s, "laptop", "chores"), colourOf(t, s, "laptop", "garden"); c != 1 || g != 2 {
		t.Fatalf("chores %d, garden %d, want 1 and 2", c, g)
	}
	if got := iconOf(t, s, "laptop", "garden"); got != "" {
		t.Errorf("a report set garden's icon to %q", got)
	}
	for _, r := range mustState(t, s).Repos {
		if r.Colour == nil {
			t.Errorf("State's %s/%s has no colour", r.Bridge, r.Name)
		}
	}

	must(t, s.SetIcon("laptop", "garden", []byte(testSVG)))
	hash := api.IconHash([]byte(testSVG))
	// A new report keeps both, whatever it says of them.
	zero := 0
	must(t, s.ReplaceRepos("laptop", []api.Repo{{Name: "chores", Colour: &zero}, {Name: "garden", OptedIn: true, Icon: "x"}}))
	if c, g := colourOf(t, s, "laptop", "chores"), colourOf(t, s, "laptop", "garden"); c != 1 || g != 2 {
		t.Fatalf("after a report: chores %d, garden %d", c, g)
	}
	if got := iconOf(t, s, "laptop", "garden"); got != hash {
		t.Fatalf("after a report garden's icon is %q", got)
	}

	// A dropped repo loses its slot, which the next new repo takes.
	must(t, s.ReplaceRepos("laptop", []api.Repo{{Name: "garden", OptedIn: true}, {Name: "beans"}}))
	if got := colourOf(t, s, "laptop", "beans"); got != 1 {
		t.Errorf("beans %d, want chores' old 1", got)
	}

	// A project gets the least-held slot; a hidden one holds none.
	must(t, s.addProject("shed"))
	if got := colourOf(t, s, HubBridge, "shed"); got != 3 {
		t.Errorf("shed %d, want 3", got)
	}
	_, err = s.db.Exec(`UPDATE repos SET hidden = 1 WHERE bridge = ? AND name = 'shed'`, HubBridge)
	must(t, err)
	must(t, s.addProject("tools"))
	if got := colourOf(t, s, HubBridge, "tools"); got != 3 {
		t.Errorf("tools %d, want 3 beside the hidden shed", got)
	}

	// A move to a path key keeps the colour; the icon goes with the old
	// row, for the bridge to send again.
	must(t, s.ReplaceRepos("laptop", []api.Repo{{Name: "projects/garden", OptedIn: true}, {Name: "beans"}}))
	if got := colourOf(t, s, "laptop", "projects/garden"); got != 2 {
		t.Errorf("projects/garden %d, want garden's 2", got)
	}
	if got := iconOf(t, s, "laptop", "projects/garden"); got != "" {
		t.Errorf("projects/garden kept the icon %q", got)
	}
	if _, _, err := s.Icon(hash); err == nil {
		t.Error("the moved repo's old icon is still served")
	}
}

func TestSetIcon(t *testing.T) {
	s, _ := open(t)
	seed(t, s)
	ch := watch(s)
	must(t, s.SetIcon("laptop", "garden", []byte(testSVG)))
	if k := ch.kinds(); k != "repos" {
		t.Errorf("changes %q, want repos", k)
	}
	must(t, s.SetIcon("laptop", "garden", []byte(testSVG)))
	if k := ch.kinds(); k != "" {
		t.Errorf("the same icon again: changes %q", k)
	}
	// The same icon on two repos is kept for each.
	must(t, s.SetIcon("laptop", "chores", []byte(testSVG)))
	ch.kinds()
	must(t, s.SetIcon("laptop", "garden", nil))
	typ, data, err := s.Icon(api.IconHash([]byte(testSVG)))
	if err != nil || typ != api.IconSVG || !bytes.Equal(data, []byte(testSVG)) {
		t.Errorf("icon %q %q %v", typ, data, err)
	}
	if k := ch.kinds(); k != "repos" {
		t.Errorf("a clear: changes %q", k)
	}
	must(t, s.SetIcon("laptop", "garden", nil))
	if k := ch.kinds(); k != "" {
		t.Errorf("a clear with none: changes %q", k)
	}
	if err := s.SetIcon("laptop", "nowhere", []byte(testSVG)); !errors.Is(err, ErrConflict) {
		t.Errorf("a repo not reported: %v", err)
	}
	if err := s.SetIcon("laptop", "garden", []byte("<html></html>")); !errors.Is(err, ErrInvalid) {
		t.Errorf("not an icon: %v", err)
	}
	if _, _, err := s.Icon("0123456789abcdef0123456789abcdef"); !errors.Is(err, ErrNotFound) {
		t.Errorf("an unknown hash: %v", err)
	}
}

func TestRestoreGivesColours(t *testing.T) {
	s, _ := open(t)
	_, err := s.Restore(bytes.NewReader(exportFile(t)))
	must(t, err)
	names, err := queryStrings(s.db, `SELECT name FROM repos WHERE bridge = ? AND colour IS NULL`, HubBridge)
	must(t, err)
	if len(names) > 0 {
		t.Errorf("restored lists without a colour: %v", names)
	}
	var n int
	must(t, s.db.QueryRow(`SELECT COUNT(*) FROM repos WHERE bridge = ?`, HubBridge).Scan(&n))
	if n < 2 {
		t.Errorf("the export restored %d hub lists; the test needs a project", n)
	}
}
