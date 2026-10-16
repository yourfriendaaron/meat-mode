package bridge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

func TestJournalAllowsOneBridge(t *testing.T) {
	dir := t.TempDir()
	j, err := OpenJournal(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := OpenJournal(dir); err == nil || !strings.Contains(err.Error(), "already running") {
		t.Fatalf("second open = %v, want the already-running error", err)
	}
	j.Close()
	j2, err := OpenJournal(dir)
	if err != nil {
		t.Fatalf("open after close: %v", err)
	}
	j2.Close()
}

func TestJournalKeepsFileWhenReadFails(t *testing.T) {
	dir := t.TempDir()
	j, err := OpenJournal(dir)
	if err != nil {
		t.Fatal(err)
	}
	a := api.Action{ID: "11111111-2222-3333-4444-555555555555", Kind: api.KindDone, Repo: "garden", BeadID: "g-1"}
	if err := j.Intent(a, time.Now()); err != nil {
		t.Fatal(err)
	}
	j.Close()
	path := filepath.Join(dir, "replay.jsonl")
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	// A line longer than the reader accepts, then one more entry after it.
	f.WriteString(strings.Repeat("x", 5<<20) + "\n" + `{"id":"later","phase":"intent"}` + "\n")
	f.Close()
	before, _ := os.ReadFile(path)

	if _, err := OpenJournal(dir); err == nil {
		t.Fatal("open succeeded over an unreadable journal")
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("the journal was rewritten after a read error")
	}
}
