package view

import (
	"strings"
	"unicode"

	"charm.land/lipgloss/v2"
	"github.com/rivo/uniseg"
)

// The screen is built as lines of styled segments, cut and padded to an
// exact cell width, then rendered with Lip Gloss. Widths are measured with
// lipgloss.Width, so a wide character counts as two cells.

// Palette is the app's dark (Dracula) palette, web/src/styles.css.
const (
	cBg2    = "#21222c"
	cRaise  = "#343746"
	cLine   = "#44475a"
	cFg     = "#f8f8f2"
	cDim    = "#6272a4"
	cSoft   = "#a9b1d6"
	cPurple = "#bd93f9"
	cPink   = "#ff79c6"
	cCyan   = "#8be9fd"
	cGreen  = "#50fa7b"
	cOrange = "#ffb86c"
	cRed    = "#ff5555"
	cYellow = "#f1fa8c"
	cBlue   = "#82aaff"
	cRose   = "#f4a6c8"
	cOnAcc  = "#282a36"
)

// repoColours are --role-repo-0..5: purple, cyan, green, orange, blue, rose.
var repoColours = [listColours]string{cPurple, cCyan, cGreen, cOrange, cBlue, cRose}

// style is a segment's look. The zero style is the terminal's own.
type style struct {
	fg, bg       string
	bold, strike bool
}

var (
	sPlain  = style{}
	sDim    = style{fg: cDim}
	sSoft   = style{fg: cSoft}
	sBold   = style{bold: true}
	sHead   = style{fg: cPurple, bold: true}
	sAccent = style{fg: cPurple}
	sGreen  = style{fg: cGreen}
	sOrange = style{fg: cOrange}
	sRed    = style{fg: cRed}
	sYellow = style{fg: cYellow}
	sCyan   = style{fg: cCyan}
	sBar    = style{bg: cBg2}
	sTab    = style{fg: cOnAcc, bg: cPurple, bold: true}
)

func (s style) lipgloss() lipgloss.Style {
	st := lipgloss.NewStyle()
	if s.fg != "" {
		st = st.Foreground(lipgloss.Color(s.fg))
	}
	if s.bg != "" {
		st = st.Background(lipgloss.Color(s.bg))
	}
	if s.bold {
		st = st.Bold(true)
	}
	if s.strike {
		st = st.Strikethrough(true)
	}
	return st
}

// seg is a run of text in one style. Its text holds no control characters.
type seg struct {
	text string
	st   style
}

// line is one screen line, as segments.
type line []seg

func sg(text string, st style) seg { return seg{text, st} }

// width is the line's width in cells.
func (l line) width() int {
	n := 0
	for _, s := range l {
		n += lipgloss.Width(s.text)
	}
	return n
}

// plain is the line's text without styles.
func (l line) plain() string {
	var b strings.Builder
	for _, s := range l {
		b.WriteString(s.text)
	}
	return b.String()
}

// render is the line styled for the terminal.
func (l line) render() string {
	var b strings.Builder
	for _, s := range l {
		if s.st == (style{}) {
			b.WriteString(s.text)
			continue
		}
		b.WriteString(s.st.lipgloss().Render(s.text))
	}
	return b.String()
}

// ellipsis marks text cut short. It is one cell wide in every terminal,
// East Asian ambiguous-width setting included.
const ellipsis = "⋯"

// cut is the longest prefix of s at most n cells wide.
func cut(s string, n int) string {
	if lipgloss.Width(s) <= n {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		next := b.String() + string(r)
		if lipgloss.Width(next) > n {
			break
		}
		b.WriteRune(r)
	}
	return b.String()
}

// fit cuts or pads l to exactly n cells. Cut text ends in an ellipsis;
// padding takes fill's style.
func fit(l line, n int, fill style) line {
	out := make(line, 0, len(l)+1)
	used := 0
	for _, s := range l {
		if used >= n {
			break
		}
		w := lipgloss.Width(s.text)
		if used+w > n {
			room := n - used
			head := cut(s.text, room-1)
			text := head + ellipsis
			if room < 1 {
				text = ""
			}
			if pad := room - lipgloss.Width(text); pad > 0 {
				text += strings.Repeat(" ", pad)
			}
			out = append(out, sg(text, s.st))
			used = n
			break
		}
		out = append(out, s)
		used += w
	}
	if used < n {
		out = append(out, sg(strings.Repeat(" ", n-used), fill))
	}
	return out
}

// lr fits left into n cells with right kept whole at the end, or left alone
// when right would leave it too little room.
func lr(left, right line, n int) line {
	rw := right.width()
	if rw >= n-8 {
		return fit(left, n, sPlain)
	}
	return append(fit(left, n-rw, sPlain), right...)
}

// withBg gives every segment without a background of its own bg.
func withBg(l line, bg string) line {
	out := make(line, len(l))
	for i, s := range l {
		if s.st.bg == "" {
			s.st.bg = bg
		}
		out[i] = s
	}
	return out
}

// joiner is the zero width joiner: the one format character kept, since
// emoji sequences need it to stay one cluster.
const joiner = '‍'

// badRune reports whether r is a character that must never reach the
// terminal as itself: a control character, one of the Unicode line and
// paragraph separators, a bidi control, or an invisible format character
// other than the zero width joiner.
func badRune(r rune) bool {
	return unicode.IsControl(r) || r == ' ' || r == ' ' ||
		unicode.Is(unicode.Bidi_Control, r) || (unicode.Is(unicode.Cf, r) && r != joiner)
}

// clean makes one line of text safe to draw: every bad rune, line feed
// and tab among them, becomes a space, so no text from the hub can move
// the cursor or change the terminal's state, and runs of space shrink
// to one.
func clean(s string) string {
	var b strings.Builder
	space := false
	for _, r := range s {
		if badRune(r) {
			r = ' '
		}
		if r == ' ' {
			if space {
				continue
			}
			space = true
		} else {
			space = false
		}
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}

// blockClean makes a block of hub text safe to hand on, as the text Y
// copies: CR and CRLF become LF, line feed and tab stay, every other bad
// rune is dropped, and indentation and blank lines are kept as they are.
func blockClean(s string) string {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r != '\n' && r != '\t' && badRune(r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// paragraphs splits multi-line text into cleaned lines, keeping blank lines.
func paragraphs(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	var out []string
	for p := range strings.SplitSeq(s, "\n") {
		out = append(out, clean(strings.ReplaceAll(p, "\t", "    ")))
	}
	return out
}

// wrap breaks text into lines at most w cells wide, at spaces where it can
// and inside a word too long for a line.
func wrap(text string, w int) []string {
	w = max(w, 1)
	var out []string
	for _, para := range paragraphs(text) {
		if para == "" {
			out = append(out, "")
			continue
		}
		cur := ""
		for word := range strings.SplitSeq(para, " ") {
			switch {
			case cur == "":
				cur = word
			case lipgloss.Width(cur)+1+lipgloss.Width(word) <= w:
				cur += " " + word
			default:
				out = append(out, cur)
				cur = word
			}
			if lipgloss.Width(cur) > w {
				ls := splitLong(cur, w)
				out = append(out, ls[:len(ls)-1]...)
				cur = ls[len(ls)-1]
			}
		}
		out = append(out, cur)
	}
	return out
}

// splitLong breaks one word too wide for a line into pieces at most w
// cells wide, in a single pass over its grapheme clusters, so a very long
// word costs one walk rather than one measurement per line and an emoji
// sequence is never split. A cluster wider than w (a wide one on a
// one-cell line) goes out alone.
func splitLong(word string, w int) []string {
	var out []string
	start, n, state := 0, 0, -1
	rest := word
	pos := 0
	for len(rest) > 0 {
		var cluster string
		var cw int
		cluster, rest, cw, state = uniseg.FirstGraphemeClusterInString(rest, state)
		if n > 0 && n+cw > w {
			out = append(out, word[start:pos])
			start, n = pos, 0
		}
		pos += len(cluster)
		n += cw
	}
	return append(out, word[start:])
}

// padRight pads s with spaces to n cells; longer text is left as it is.
func padRight(s string, n int) string {
	if w := lipgloss.Width(s); w < n {
		return s + strings.Repeat(" ", n-w)
	}
	return s
}

// padLeft pads s with spaces on the left to n cells.
func padLeft(s string, n int) string {
	if w := lipgloss.Width(s); w < n {
		return strings.Repeat(" ", n-w) + s
	}
	return s
}
