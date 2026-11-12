package api

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestCheckEpicRowsCleans(t *testing.T) {
	rows := []EpicRow{
		{ID: "gd-1", Title: strings.Repeat("t", ItemTitleLimit+9), Status: "open", Epic: true, Description: "Plan the beds.", Priority: 9},
		{ID: "gd-1.1", Title: "Water the plants", Status: "pinned", Parent: "gd-1", Description: "not a card", Priority: -2},
		{ID: "gd-2", Title: "Mow the lawn", Status: "open", Parent: "gd-1.1"},
		{ID: "gd-3", Title: "Fence", Status: "open", Parent: "gd-3"},
	}
	if err := CheckEpicRows(rows); err != nil {
		t.Fatal(err)
	}
	if len([]rune(rows[0].Title)) != ItemTitleLimit+1 || rows[0].Priority != 4 || rows[0].Description == "" {
		t.Errorf("card not cleaned as expected: %+v", rows[0])
	}
	if rows[1].Status != "open" || rows[1].Priority != 0 || rows[1].Description != "not a card" || rows[1].Parent != "gd-1" {
		t.Errorf("child not cleaned as expected: %+v", rows[1])
	}
	if rows[2].Parent != "" || rows[3].Parent != "" {
		t.Errorf("parent not naming a card kept: %q %q", rows[2].Parent, rows[3].Parent)
	}
}

// TestCheckEpicRowsCutsComments: a comment's text is cut to CommentLimit and
// its id and author to CommentAuthorLimit, and its text counts against the
// set's budget with descriptions and notes.
func TestCheckEpicRowsCutsComments(t *testing.T) {
	rows := []EpicRow{{ID: "gd-1", Status: "open", CommentCount: 1, Comments: []Comment{{
		ID:     strings.Repeat("i", CommentAuthorLimit+9),
		Author: strings.Repeat("a", CommentAuthorLimit+9),
		Text:   strings.Repeat("é", CommentLimit+9),
	}}}}
	if err := CheckEpicRows(rows); err != nil {
		t.Fatal(err)
	}
	c := rows[0].Comments[0]
	if n := len([]rune(c.Text)); n != CommentLimit+1 {
		t.Errorf("text is %d runes", n)
	}
	if len([]rune(c.ID)) != CommentAuthorLimit+1 || len([]rune(c.Author)) != CommentAuthorLimit+1 {
		t.Errorf("id and author are %d and %d runes", len([]rune(c.ID)), len([]rune(c.Author)))
	}

	// Descriptions fill the budget but for less than a comment; the text of
	// one more row's comment tips it over.
	big := strings.Repeat("a", ItemDescriptionLimit)
	var set []EpicRow
	for i := range MaxEpicDescriptions / ItemDescriptionLimit {
		set = append(set, EpicRow{ID: "gd-" + strings.Repeat("x", 1) + string(rune('a'+i%26)) + string(rune('a'+i/26)), Status: "open", Description: big})
	}
	if err := CheckEpicRows(set); err != nil {
		t.Fatalf("descriptions alone: %v", err)
	}
	set = append(set, EpicRow{ID: "gd-last", Status: "open", Comments: []Comment{{Author: "sam", Text: strings.Repeat("a", MaxEpicDescriptions)}}})
	if err := CheckEpicRows(set); !errors.Is(err, ErrInvalid) {
		t.Errorf("comment text past the budget: got %v, want ErrInvalid", err)
	}
}

func TestCheckEpicRowsRefuses(t *testing.T) {
	for name, rows := range map[string][]EpicRow{
		"bad id":      {{ID: "-x", Status: "open"}},
		"provisional": {{ID: ProvisionalPrefix + "1", Status: "open"}},
		"twice":       {{ID: "gd-1", Status: "open"}, {ID: "gd-1", Status: "open"}},
		"too many":    make([]EpicRow, MaxEpicRows+1),
	} {
		if err := CheckEpicRows(rows); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: got %v, want ErrInvalid", name, err)
		}
	}
}

func TestCheckEpicRowsBreaksParentLoops(t *testing.T) {
	rows := []EpicRow{
		{ID: "gd-1", Status: "open", Epic: true, Parent: "gd-3"},
		{ID: "gd-2", Status: "open", Epic: true, Parent: "gd-1"},
		{ID: "gd-3", Status: "open", Epic: true, Parent: "gd-2"},
		{ID: "gd-4", Status: "open", Parent: "gd-3"},
	}
	if err := CheckEpicRows(rows); err != nil {
		t.Fatal(err)
	}
	if rows[0].Parent != "" || rows[1].Parent != "gd-1" || rows[2].Parent != "gd-2" || rows[3].Parent != "gd-3" {
		t.Errorf("parents %q %q %q %q, want the loop broken at gd-1 only", rows[0].Parent, rows[1].Parent, rows[2].Parent, rows[3].Parent)
	}
}

func TestCheckEpicRowsCleansWaitsAndDates(t *testing.T) {
	wake := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	many := make([]string, MaxEpicBlockedBy+5)
	for i := range many {
		many[i] = "gd-" + strings.Repeat("9", i+1)
	}
	rows := []EpicRow{
		{ID: "gd-1", Status: "open", BlockedBy: []string{"gd-2", "gd-2", "-x", "gd-1", "gd-3"}, CommentCount: -1},
		{ID: "gd-2", Status: "closed", BlockedBy: []string{"gd-3"}, DeferUntil: &wake},
		{ID: "gd-3", Status: "deferred", DeferUntil: &wake, CommentCount: 4},
		{ID: "gd-4", Status: "open", BlockedBy: many},
	}
	if err := CheckEpicRows(rows); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(rows[0].BlockedBy, ","); got != "gd-2,gd-3" || rows[0].CommentCount != 0 {
		t.Errorf("open row: blockedBy %q, comments %d", got, rows[0].CommentCount)
	}
	if rows[1].BlockedBy != nil || rows[1].DeferUntil != nil {
		t.Errorf("closed row kept a wait or a date: %+v", rows[1])
	}
	if rows[2].DeferUntil == nil || !rows[2].DeferUntil.Equal(wake) || rows[2].CommentCount != 4 {
		t.Errorf("deferred row lost its date or count: %+v", rows[2])
	}
	if len(rows[3].BlockedBy) != MaxEpicBlockedBy {
		t.Errorf("blockedBy kept %d ids, want %d", len(rows[3].BlockedBy), MaxEpicBlockedBy)
	}
}
