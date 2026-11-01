package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
)

// List identity (docs/api.md, List identity).
const (
	ListColours  = 6        // colour slots a list can have, 0 to ListColours-1
	MaxIconBytes = 64 << 10 // the largest repo icon a bridge sends or the hub takes
)

// Icon content types, the only three a repo icon may be.
const (
	IconSVG = "image/svg+xml"
	IconPNG = "image/png"
	IconICO = "image/x-icon"
)

// SniffIcon returns the content type of a repo icon from its bytes, never
// from a file name or a header: PNG and ICO by their magic numbers, SVG as
// text whose first element, after an optional XML declaration, comments and
// a doctype, is <svg. ok is false for anything else, for an empty icon and
// for one past MaxIconBytes.
func SniffIcon(b []byte) (contentType string, ok bool) {
	if len(b) == 0 || len(b) > MaxIconBytes {
		return "", false
	}
	switch {
	case bytes.HasPrefix(b, []byte("\x89PNG\r\n\x1a\n")):
		return IconPNG, true
	case len(b) >= 6 && bytes.HasPrefix(b, []byte{0, 0, 1, 0}) && (b[4] != 0 || b[5] != 0):
		return IconICO, true
	}
	s := bytes.TrimPrefix(b, []byte("\xef\xbb\xbf"))
	for {
		s = bytes.TrimLeft(s, " \t\r\n")
		switch {
		case bytes.HasPrefix(s, []byte("<?xml")):
			i := bytes.Index(s, []byte("?>"))
			if i < 0 {
				return "", false
			}
			s = s[i+2:]
		case bytes.HasPrefix(s, []byte("<!--")):
			i := bytes.Index(s, []byte("-->"))
			if i < 0 {
				return "", false
			}
			s = s[i+3:]
		case bytes.HasPrefix(s, []byte("<!DOCTYPE")), bytes.HasPrefix(s, []byte("<!doctype")):
			i := bytes.IndexByte(s, '>')
			if i < 0 {
				return "", false
			}
			s = s[i+1:]
		case bytes.HasPrefix(s, []byte("<svg")) && len(s) > 4 && (s[4] == ' ' || s[4] == '>' || s[4] == '\t' || s[4] == '\n' || s[4] == '\r'):
			return IconSVG, true
		default:
			return "", false
		}
	}
}

// IconHash names a repo icon by its bytes: the first 32 hex digits of their
// SHA-256. The same icon has the same name on every bridge and hub.
func IconHash(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:16])
}

// ValidIconHash reports whether s has IconHash's form.
func ValidIconHash(s string) bool {
	if len(s) != 32 {
		return false
	}
	for _, c := range s {
		if !('0' <= c && c <= '9' || 'a' <= c && c <= 'f') {
			return false
		}
	}
	return true
}
