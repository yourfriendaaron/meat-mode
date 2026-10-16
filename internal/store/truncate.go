package store

import "unicode/utf8"

// Length limits on the text a bridge sends, so one huge bead cannot bloat
// the database or the state the app downloads. Longer text is truncated,
// not rejected: one long bead must not stop a repo syncing.
const (
	maxVersionLen       = 64
	maxTitleLen         = 500
	maxDescriptionLen   = 20000
	maxCommentLen       = 20000
	maxCommentAuthorLen = 200
	maxResultErrorLen   = 2000
)

// truncRunes cuts s to at most limit runes (never mid-rune) and appends an
// ellipsis when it cut anything.
func truncRunes(s string, limit int) string {
	if utf8.RuneCountInString(s) <= limit {
		return s
	}
	return string([]rune(s)[:limit]) + "…"
}
