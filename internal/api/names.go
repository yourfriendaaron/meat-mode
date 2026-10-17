package api

import (
	"strings"
	"unicode/utf8"
)

// MaxRepoNameLen caps a repo name, which is a path of segments.
const MaxRepoNameLen = 512

// ValidSegment reports whether s can be one segment of a key: 1 to 128
// characters, no slash or control character, not "." or "..", and no
// leading "-" (which a command line would read as a flag).
func ValidSegment(s string) bool {
	if s == "" || len(s) > 128 || s == "." || s == ".." || s[0] == '-' || !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if r == '/' || r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

// ValidRepoName reports whether name can be a repo name: a slash-separated
// path of valid segments, such as projects/garden, at most MaxRepoNameLen
// bytes.
func ValidRepoName(name string) bool {
	if name == "" || len(name) > MaxRepoNameLen {
		return false
	}
	for seg := range strings.SplitSeq(name, "/") {
		if !ValidSegment(seg) {
			return false
		}
	}
	return true
}
