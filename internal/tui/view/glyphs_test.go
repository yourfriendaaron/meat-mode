package view

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
)

// ambiguous are the East Asian Width "A" (ambiguous) ranges of
// EastAsianWidth.txt from U+0080 to U+2BFF, where a terminal set to draw
// ambiguous characters wide gives them two cells.
var ambiguous = [][2]rune{
	{0x00A1, 0x00A1}, {0x00A4, 0x00A4}, {0x00A7, 0x00A8}, {0x00AA, 0x00AA}, {0x00AD, 0x00AE}, {0x00B0, 0x00B4},
	{0x00B6, 0x00BA}, {0x00BC, 0x00BF}, {0x00C6, 0x00C6}, {0x00D0, 0x00D0}, {0x00D7, 0x00D8}, {0x00DE, 0x00E1},
	{0x00E6, 0x00E6}, {0x00E8, 0x00EA}, {0x00EC, 0x00ED}, {0x00F0, 0x00F0}, {0x00F2, 0x00F3}, {0x00F7, 0x00FA},
	{0x00FC, 0x00FC}, {0x00FE, 0x00FE},
	{0x2010, 0x2010}, {0x2013, 0x2016}, {0x2018, 0x2019}, {0x201C, 0x201D}, {0x2020, 0x2022}, {0x2024, 0x2027},
	{0x2030, 0x2030}, {0x2032, 0x2033}, {0x2035, 0x2035}, {0x203B, 0x203B}, {0x203E, 0x203E}, {0x2074, 0x2074},
	{0x207F, 0x207F}, {0x2081, 0x2084}, {0x20AC, 0x20AC}, {0x2103, 0x2103}, {0x2105, 0x2105}, {0x2109, 0x2109},
	{0x2113, 0x2113}, {0x2116, 0x2116}, {0x2121, 0x2122}, {0x2126, 0x2126}, {0x212B, 0x212B}, {0x2153, 0x2154},
	{0x215B, 0x215E}, {0x2160, 0x216B}, {0x2170, 0x2179}, {0x2189, 0x2189}, {0x2190, 0x2199}, {0x21B8, 0x21B9},
	{0x21D2, 0x21D2}, {0x21D4, 0x21D4}, {0x21E7, 0x21E7}, {0x2200, 0x2200}, {0x2202, 0x2203}, {0x2207, 0x2208},
	{0x220B, 0x220B}, {0x220F, 0x220F}, {0x2211, 0x2211}, {0x2215, 0x2215}, {0x221A, 0x221A}, {0x221D, 0x2220},
	{0x2223, 0x2223}, {0x2225, 0x2225}, {0x2227, 0x222C}, {0x222E, 0x222E}, {0x2234, 0x2237}, {0x223C, 0x223D},
	{0x2248, 0x2248}, {0x224C, 0x224C}, {0x2252, 0x2252}, {0x2260, 0x2261}, {0x2264, 0x2267}, {0x226A, 0x226B},
	{0x226E, 0x226F}, {0x2282, 0x2283}, {0x2286, 0x2287}, {0x2295, 0x2295}, {0x2299, 0x2299}, {0x22A5, 0x22A5},
	{0x22BF, 0x22BF}, {0x2312, 0x2312}, {0x2460, 0x24E9}, {0x24EB, 0x254B}, {0x2550, 0x2573}, {0x2580, 0x258F},
	{0x2592, 0x2595}, {0x25A0, 0x25A1}, {0x25A3, 0x25A9}, {0x25B2, 0x25B3}, {0x25B6, 0x25B7}, {0x25BC, 0x25BD},
	{0x25C0, 0x25C1}, {0x25C6, 0x25C8}, {0x25CB, 0x25CB}, {0x25CE, 0x25D1}, {0x25E2, 0x25E5}, {0x25EF, 0x25EF},
	{0x2605, 0x2606}, {0x2609, 0x2609}, {0x260E, 0x260F}, {0x261C, 0x261C}, {0x261E, 0x261E}, {0x2640, 0x2640},
	{0x2642, 0x2642}, {0x2660, 0x2661}, {0x2663, 0x2665}, {0x2667, 0x266A}, {0x266C, 0x266D}, {0x266F, 0x266F},
	{0x269E, 0x269F}, {0x26BF, 0x26BF}, {0x26C6, 0x26CD}, {0x26CF, 0x26D3}, {0x26D5, 0x26E1}, {0x26E3, 0x26E3},
	{0x26E8, 0x26E9}, {0x26EB, 0x26F1}, {0x26F4, 0x26F4}, {0x26F6, 0x26F9}, {0x26FB, 0x26FC}, {0x26FE, 0x26FF},
	{0x273D, 0x273D}, {0x2776, 0x277F}, {0x2B56, 0x2B59},
}

func isAmbiguous(r rune) bool {
	for _, a := range ambiguous {
		if r >= a[0] && r <= a[1] {
			return true
		}
	}
	return false
}

// glyphs are every non-ASCII character the view draws of its own.
var glyphs = []string{gOpen, gProgress, gBlocked, gClosed, gDeferred, gSnoozed, gHuman, gRepeat, gFolded, gUnfolded,
	gBarFull, gBarEmpty, gSplit, gEdge, gBullet, gDot, gBack, gCrumb, gLive, gSelected, ellipsis}

// TestGlyphsOneCell checks the view's own glyphs: one rune each, one cell
// wide, and not ambiguous, so no terminal setting widens them.
func TestGlyphsOneCell(t *testing.T) {
	for _, g := range glyphs {
		r, _ := utf8.DecodeRuneInString(g)
		if utf8.RuneCountInString(g) != 1 || lipgloss.Width(g) != 1 || isAmbiguous(r) {
			t.Errorf("glyph %q (U+%04X) is not one narrow, unambiguous cell", g, r)
		}
	}
}

// TestFramesUseOnlyKnownGlyphs checks every frame of every scenario: each
// non-ASCII character is one of the view's glyphs or came from the data.
func TestFramesUseOnlyKnownGlyphs(t *testing.T) {
	known := map[rune]bool{}
	for _, g := range glyphs {
		r, _ := utf8.DecodeRuneInString(g)
		known[r] = true
	}
	for _, r := range "🌱種" { // fixture text
		known[r] = true
	}
	for _, sc := range scenarios {
		for _, h := range heights {
			for _, w := range widths {
				for _, l := range plain(sc.build(t, w, h)) {
					for _, r := range l {
						if r >= 0x80 && !known[r] {
							t.Errorf("%s %dx%d draws %q (U+%04X), which is not a vetted glyph", sc.name, w, h, r, r)
						}
					}
				}
			}
		}
	}
}

// TestFitWide checks that cutting and padding never splits a wide
// character or leaves a line off its width.
func TestFitWide(t *testing.T) {
	for _, text := range []string{"種種種種", "a種種種", "🌱🌱🌱", "ab🌱c種d"} {
		for n := 0; n <= 9; n++ {
			l := fit(line{sg(text, sPlain)}, n, sPlain)
			if w := lipgloss.Width(l.render()); w != n {
				t.Errorf("fit(%q, %d) is %d cells: %q", text, n, w, l.plain())
			}
			if n < lipgloss.Width(text) && n > 0 && !strings.Contains(l.plain(), ellipsis) {
				t.Errorf("fit(%q, %d) = %q, cut without an ellipsis", text, n, l.plain())
			}
		}
	}
}

// TestCleanDropsControls keeps text from the hub off the terminal's
// control path.
func TestCleanDropsControls(t *testing.T) {
	got := clean("Water\x1b[2J the\tplants\r\n‮now\u0085")
	if strings.ContainsAny(got, "\x1b\t\r\n‮\u0085") {
		t.Fatalf("clean kept a control character: %q", got)
	}
	if got != "Water [2J the plants now" {
		t.Fatalf("clean = %q", got)
	}
}

// TestCleanDropsFormat keeps invisible format characters out too: tag
// characters, zero width spaces, word joiners and the BOM all go, while
// the zero width joiner stays, since emoji sequences need it.
func TestCleanDropsFormat(t *testing.T) {
	got := clean("Wa\U000E0041ter\u200b the plan\u2060ts\ufeff")
	if strings.ContainsAny(got, "\U000E0041\u200b\u2060\ufeff\u00ad") {
		t.Fatalf("clean kept a format character: %q", got)
	}
	if got != "Wa ter the plan ts" {
		t.Fatalf("clean = %q", got)
	}
	if got := clean("a\u200db"); got != "a\u200db" {
		t.Fatalf("clean dropped the zero width joiner: %q", got)
	}
}

// TestWrapLongWordFast wraps a word far longer than a line: every line
// stays within the width, no text is lost, and the work stays a single
// pass, not one measurement per line.
func TestWrapLongWordFast(t *testing.T) {
	for _, c := range []struct {
		in   string
		want int
	}{
		{strings.Repeat("x", 20000), 500},
		{strings.Repeat("種", 20000), 1000},
	} {
		start := time.Now()
		ls := wrap(c.in, 40)
		if d := time.Since(start); d > 200*time.Millisecond {
			t.Errorf("wrap of %d runes took %s, want under 200 ms", utf8.RuneCountInString(c.in), d)
		}
		if len(ls) != c.want {
			t.Errorf("wrap gave %d lines, want %d", len(ls), c.want)
		}
		for i, l := range ls {
			if w := lipgloss.Width(l); w > 40 {
				t.Errorf("line %d is %d cells, want at most 40: %q", i, w, l)
			}
		}
		if strings.Join(ls, "") != c.in {
			t.Error("wrapping lost text")
		}
	}
}
