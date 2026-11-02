package view

import (
	"strings"
	"testing"
	"unicode"
)

// hostile is what a compromised hub could put in a description: an
// escape that rewrites a bracketed paste, a BEL and a bidi override,
// around an indented line that must come through as it is.
const hostile = "a\x1b[201~b\x07\n  c\u202e"

// checkNoBadRunes fails when text holds a control character other than
// line feed and tab, or any bidi control.
func checkNoBadRunes(t *testing.T, name, text string) {
	t.Helper()
	for _, r := range text {
		if (unicode.IsControl(r) && r != '\n' && r != '\t') || unicode.Is(unicode.Bidi_Control, r) {
			t.Errorf("%s keeps %q (U+%04X)", name, r, r)
		}
	}
}

// TestRefFullCleansBlocks checks the text Y copies of an item and of an
// epic row: a hostile description loses its control and bidi characters
// and keeps its indentation, and a hostile status loses its escape.
func TestRefFullCleansBlocks(t *testing.T) {
	s, e := load(t)
	for i := range s.Items {
		if s.Items[i].ID == "mm-28" {
			s.Items[i].Description = hostile
			s.Items[i].Status = "in_progress\x1b]0;x\a"
		}
	}
	for k := range e.Repos {
		for j := range e.Repos[k].Rows {
			if e.Repos[k].Rows[j].ID == "sf-c1x" {
				e.Repos[k].Rows[j].Description = hostile
			}
		}
	}
	m := selectKey(t, New(hub).Resize(90, 30).SetConn(ConnLive).SetData(s, e, now), "hub/personal/mm-28")
	item := m.d.refFull(m.target())
	checkNoBadRunes(t, "item refFull", item)
	if !strings.Contains(item, "\n  c") {
		t.Errorf("item refFull lost the indented line: %q", item)
	}

	r, _ := New(hub).Resize(90, 30).SetConn(ConnLive).SetData(s, e, now).Key("3")
	r = selectKey(t, r, "laptop/acme/storefront/sf-c1x")
	row := r.d.refFull(r.target())
	checkNoBadRunes(t, "row refFull", row)
	if !strings.Contains(row, "\n  c") {
		t.Errorf("row refFull lost the indented line: %q", row)
	}
}

// TestRefLineKeepsEmoji checks the line y copies: invisible format
// characters go, and an emoji sequence joined by the zero width joiner
// survives, copied and drawn.
func TestRefLineKeepsEmoji(t *testing.T) {
	s, e := load(t)
	for i := range s.Items {
		if s.Items[i].ID == "mm-4" {
			s.Items[i].Title = "Order\U000E0041 bark\u200b for \U0001F469\u200d\U0001F33E"
		}
	}
	m := selectKey(t, New(hub).Resize(90, 30).SetConn(ConnLive).SetData(s, e, now), "hub/projects/garden/mm-4")
	line := m.d.refLine(m.target())
	if strings.ContainsAny(line, "\U000E0041\u200b") {
		t.Errorf("refLine kept a format character: %q", line)
	}
	if !strings.Contains(line, "\U0001F469\u200d\U0001F33E") {
		t.Errorf("refLine lost the emoji sequence: %q", line)
	}
	checkFrame(t, "emoji title", m)
	for _, l := range plain(m) {
		if strings.ContainsAny(l, "\U000E0041\u200b") {
			t.Errorf("frame kept a format character: %q", l)
		}
	}
}
