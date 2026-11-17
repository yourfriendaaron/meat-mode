package netguard

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestHubMessageSafeToPrint(t *testing.T) {
	msg := HubMessage([]byte(`{"error": "\u001b]0;x\u0007owned\u202ereply"}`))
	if strings.ContainsAny(msg, "\x1b\a\u202e") {
		t.Errorf("HubMessage kept a hostile character: %q", msg)
	}
	if msg != "]0;xownedreply" {
		t.Errorf("HubMessage = %q", msg)
	}
	if msg := HubMessage([]byte(`{"error": "wait\u200b\u2060\ufeff\udb40\udc41 here"}`)); msg != "wait here" {
		t.Errorf("HubMessage kept invisible format characters: %q", msg)
	}
	msg = HubMessage([]byte(strings.Repeat("a", 1<<20)))
	if n := utf8.RuneCountInString(msg); n != MaxMessageRunes {
		t.Errorf("HubMessage kept %d runes of a 1 MiB body, want %d", n, MaxMessageRunes)
	}
}
