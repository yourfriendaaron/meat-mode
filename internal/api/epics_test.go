package api

import (
	"errors"
	"strings"
	"testing"
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
