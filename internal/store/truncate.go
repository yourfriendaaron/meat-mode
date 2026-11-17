package store

import "github.com/yourfriendaaron/meat-mode/internal/api"

// Length limits on the text a bridge sends; see api.ItemDescriptionLimit.
// Longer text is truncated, not rejected: one long bead must not stop a
// repo syncing. A title is cut at MaxTitleLen.
const (
	maxVersionLen       = 64
	maxDescriptionLen   = api.ItemDescriptionLimit
	maxCommentLen       = api.CommentLimit
	maxCommentAuthorLen = api.CommentAuthorLimit
	maxResultErrorLen   = 2000
)

// truncRunes cuts s to at most limit runes; see api.TruncRunes.
func truncRunes(s string, limit int) string { return api.TruncRunes(s, limit) }
