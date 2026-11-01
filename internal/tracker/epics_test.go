package tracker

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

func TestOutline(t *testing.T) {
	rows := []api.EpicRow{
		{ID: "gd-1", Title: "Garden", Type: "epic", Status: StatusOpen, Description: "Plan the beds."},
		{ID: "gd-1.1", Title: "Buy soil", Type: "task", Status: StatusClosed, Parent: "gd-1", Description: "Kept on a child."},
		{ID: "gd-1.2", Title: "Water the plants", Type: "task", Status: StatusOpen, Parent: "gd-1", Human: true},
		{ID: "gd-1.3", Title: "Shed", Type: "epic", Status: StatusDeferred, Parent: "gd-1"},
		{ID: "gd-1.3.1", Title: "Paint the shed", Type: "task", Status: StatusOpen, Parent: "gd-1.3"},
		{ID: "gd-2", Title: "Old epic", Type: "Epic", Status: StatusClosed},
		{ID: "gd-2.1", Title: "Old child", Status: StatusClosed, Parent: "gd-2"},
		{ID: "gd-2.2", Title: "Left open", Status: StatusOpen, Parent: "gd-2"},
		{ID: "pending-x", Title: "Bad id", Status: StatusOpen},
		{ID: "gd-4", Title: "Duplicate", Type: "task", Status: StatusOpen},
		{ID: "gd-3", Title: "Fence", Type: "feature", Status: StatusOpen},
		{ID: "gd-3.1", Title: "Measure the fence", Status: StatusClosed, Parent: "gd-3"},
		{ID: "gd-4", Title: "Mow the lawn", Type: "task", Status: StatusOpen},
		{ID: "gd-5", Title: "Done top-level", Type: "task", Status: StatusClosed},
		{ID: "gd-6", Title: "Orphan", Type: "task", Status: StatusOpen, Parent: "gd-missing"},
		{ID: "gd-7", Title: "Empty epic", Type: "epic", Status: StatusOpen},
	}
	got := Outline(rows)
	want := map[string]struct {
		epic   bool
		parent string
		desc   string
	}{
		"gd-1":     {true, "", "Plan the beds."},
		"gd-1.1":   {false, "gd-1", "Kept on a child."},
		"gd-1.2":   {false, "gd-1", ""},
		"gd-1.3":   {true, "gd-1", ""},
		"gd-1.3.1": {false, "gd-1.3", ""},
		"gd-2.2":   {false, "", ""},
		"gd-3":     {true, "", ""},
		"gd-3.1":   {false, "gd-3", ""},
		"gd-4":     {false, "", ""},
		"gd-6":     {false, "", ""},
		"gd-7":     {true, "", ""},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d rows, want %d: %+v", len(got), len(want), got)
	}
	for i, r := range got {
		if i > 0 && got[i-1].ID >= r.ID {
			t.Errorf("rows not sorted by id at %s", r.ID)
		}
		w, ok := want[r.ID]
		if !ok {
			t.Errorf("unexpected row %s", r.ID)
			continue
		}
		if r.Epic != w.epic || r.Parent != w.parent || r.Description != w.desc {
			t.Errorf("%s: epic=%v parent=%q desc=%q, want %v %q %q", r.ID, r.Epic, r.Parent, r.Description, w.epic, w.parent, w.desc)
		}
		if r.ID == "gd-4" && r.Title != "Duplicate" { // the first of the two in rows
			t.Errorf("gd-4: kept %q, want the first row", r.Title)
		}
		if r.ID == "gd-1.2" && !r.Human {
			t.Errorf("gd-1.2 lost Human")
		}
	}
}

func TestOutlineCutsDescriptions(t *testing.T) {
	long := strings.Repeat("é", api.ItemDescriptionLimit+5)
	got := Outline([]api.EpicRow{{ID: "gd-1", Type: "epic", Status: StatusOpen, Description: long}})
	if n := utf8.RuneCountInString(got[0].Description); n != api.ItemDescriptionLimit+1 { // TruncRunes adds an ellipsis
		t.Fatalf("description is %d runes", n)
	}
	var rows []api.EpicRow
	big := strings.Repeat("a", api.ItemDescriptionLimit)
	for i := range api.MaxEpicDescriptions/api.ItemDescriptionLimit + 3 {
		rows = append(rows, api.EpicRow{ID: fmt.Sprintf("gd-%04d", i), Type: "epic", Status: StatusOpen, Description: big})
	}
	total := 0
	for _, r := range Outline(rows) {
		total += len(r.Description)
	}
	if total > api.MaxEpicDescriptions || total < api.MaxEpicDescriptions-api.ItemDescriptionLimit {
		t.Fatalf("descriptions total %d bytes", total)
	}
}

// TestOutlineSpendsDescriptionsCardsFirst: past the budget, a card keeps
// its description over an open task, and an open task over a closed one,
// whatever their ids.
func TestOutlineSpendsDescriptionsCardsFirst(t *testing.T) {
	big := strings.Repeat("a", api.ItemDescriptionLimit)
	n := api.MaxEpicDescriptions / api.ItemDescriptionLimit
	var rows []api.EpicRow
	// Closed children first by id, then open top-level tasks, then cards.
	for i := range n {
		rows = append(rows, api.EpicRow{ID: fmt.Sprintf("ga-%04d", i), Status: StatusClosed, Parent: "gz-0000", Description: big})
	}
	rows = append(rows, api.EpicRow{ID: "gm-0000", Status: StatusOpen, Description: big})
	for i := range n - 1 {
		rows = append(rows, api.EpicRow{ID: fmt.Sprintf("gz-%04d", i), Type: "epic", Status: StatusOpen, Description: big})
	}
	for _, r := range Outline(rows) {
		switch {
		case r.Epic && r.Description == "":
			t.Errorf("card %s lost its description", r.ID)
		case r.ID == "gm-0000" && r.Description == "":
			t.Errorf("open task lost its description before a closed one")
		case r.Status == StatusClosed && r.Description != "":
			t.Errorf("closed %s kept its description past the budget", r.ID)
		}
	}
}

func TestOutlineCapsRowsClosedFirst(t *testing.T) {
	rows := []api.EpicRow{{ID: "e", Type: "epic", Status: StatusOpen}}
	for i := range api.MaxEpicRows + 10 {
		st := StatusClosed
		if i < 5 {
			st = StatusOpen
		}
		rows = append(rows, api.EpicRow{ID: fmt.Sprintf("c%06d", i), Status: st, Parent: "e"})
	}
	got := Outline(rows)
	if len(got) != api.MaxEpicRows {
		t.Fatalf("got %d rows", len(got))
	}
	open := 0
	for _, r := range got {
		if r.Status != StatusClosed {
			open++
		}
	}
	if open != 6 {
		t.Fatalf("kept %d open rows, want 6", open)
	}
}
