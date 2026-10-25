package store

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

func TestListPersonal(t *testing.T) {
	s, c := open(t)
	_, err := s.CreateBridge("laptop")
	must(t, err)
	add := func(id, title, project string, labels ...string) string {
		key, _, err := s.AddPersonal(api.PersonalTodo{ID: id, Title: title, Labels: labels}, "laptop", project)
		must(t, err)
		_, _, item, _ := SplitKey(key)
		c.t = c.t.Add(time.Minute)
		return item
	}
	shop := addPersonal(t, s, id1, "Shopping list", nil).BeadID
	c.t = c.t.Add(time.Minute)
	milk := add(id2, "Buy milk", "", "errands")
	plants := add(id3, "Water the plants", "", "home", "Errands")
	add(id4, "Plant the beans", "projects/garden")
	_, _, err = s.ChangePersonal("laptop", "", plants, api.PersonalChange{Kind: api.KindDone})
	must(t, err)

	ids := func(f api.PersonalFilter, project string) []string {
		t.Helper()
		items, err := s.ListPersonal(project, f)
		must(t, err)
		out := []string{}
		for _, it := range items {
			out = append(out, it.ID)
		}
		return out
	}
	for name, tc := range map[string]struct {
		f       api.PersonalFilter
		project string
		want    []string
	}{
		"open by default":    {api.PersonalFilter{}, "", []string{shop, milk}},
		"closed":             {api.PersonalFilter{Status: FilterClosed}, "", []string{plants}},
		"all":                {api.PersonalFilter{Status: FilterAll}, "", []string{shop, milk, plants}},
		"title without case": {api.PersonalFilter{Query: "  SHOP"}, "", []string{shop}},
		"tag without case":   {api.PersonalFilter{Status: FilterAll, Tags: []string{"ERRANDS"}}, "", []string{milk, plants}},
		"every tag":          {api.PersonalFilter{Status: FilterAll, Tags: []string{"errands", "home"}}, "", []string{plants}},
		"a project":          {api.PersonalFilter{}, "projects/garden", []string{"mm-4"}},
		"a project not made": {api.PersonalFilter{}, "projects/other", []string{}},
		"no match":           {api.PersonalFilter{Query: "passports"}, "", []string{}},
	} {
		if got := ids(tc.f, tc.project); !slices.Equal(got, tc.want) {
			t.Errorf("%s: %v, want %v", name, got, tc.want)
		}
	}
	if _, err := s.ListPersonal("", api.PersonalFilter{Status: "done"}); !errors.Is(err, ErrInvalid) {
		t.Errorf("bad status: %v", err)
	}
	if _, err := s.ListPersonal("personal", api.PersonalFilter{}); !errors.Is(err, ErrInvalid) {
		t.Errorf("bad project: %v", err)
	}
	// A closed item leaves the list with the week the app shows it.
	c.t = c.t.Add(8 * 24 * time.Hour)
	if got := ids(api.PersonalFilter{Status: FilterClosed}, ""); len(got) != 0 {
		t.Errorf("closed past the week: %v", got)
	}

	ps, err := s.Projects()
	must(t, err)
	if len(ps) != 1 || ps[0] != (api.Project{Name: "projects/garden", Open: 1}) {
		t.Fatalf("projects %+v", ps)
	}
}
