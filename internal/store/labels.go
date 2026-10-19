package store

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

// cleanLabels applies the tag rules (docs/api.md, Tags) to the tags of an
// item on a list the hub keeps: each trimmed, empty ones dropped, then 1 to
// api.MaxTagLen characters with no whitespace, control character or comma;
// a repeat without case dropped, the first spelling kept; api.HumanLabel
// dropped; at most api.MaxTags. The result is never nil, so a cleared list
// is an empty one.
func cleanLabels(in []string) ([]string, error) {
	out := []string{}
	seen := map[string]bool{}
	for _, l := range in {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		if utf8.RuneCountInString(l) > api.MaxTagLen {
			return nil, invalidf("the tag %q is longer than %d characters", l, api.MaxTagLen)
		}
		if strings.ContainsFunc(l, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) || r == ',' }) {
			return nil, invalidf("the tag %q has a space, comma or control character; a tag is one word", l)
		}
		fold := strings.ToLower(l)
		if fold == api.HumanLabel || seen[fold] {
			continue
		}
		seen[fold] = true
		out = append(out, l)
	}
	if len(out) > api.MaxTags {
		return nil, invalidf("an item has at most %d tags", api.MaxTags)
	}
	return out, nil
}
