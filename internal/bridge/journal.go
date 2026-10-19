package bridge

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

// journalKeep is how long finished entries stay in the replay journal.
const journalKeep = 30 * 24 * time.Hour

// Journal is the bridge's replay journal: one JSON line per step, fsynced,
// so a restart knows which actions it started and which it finished.
type Journal struct {
	path    string
	f       *os.File
	lock    *os.File // held while open, so only one bridge replays from this state
	entries map[string]*JournalEntry
}

// JournalEntry is what the journal knows about one action.
type JournalEntry struct {
	ID     string           `json:"id"`
	Phase  string           `json:"phase"` // intent or done
	At     time.Time        `json:"at"`
	Kind   string           `json:"kind,omitempty"`
	Repo   string           `json:"repo,omitempty"`
	BeadID string           `json:"beadId,omitempty"`
	Result api.ActionResult `json:"result"`
	Intent time.Time        `json:"-"` // when the intent was written
}

// Done reports whether the tracker write finished.
func (e JournalEntry) Done() bool { return e.Phase == "done" }

// errLocked means another bridge process holds the journal.
var errLocked = errors.New("another bridge is already running with this state directory; stop it first (it may be the user service)")

// OpenJournal opens (or creates) dir/replay.jsonl, dropping finished entries
// older than 30 days. It holds a lock on dir until Close, so a second bridge
// on the same machine cannot replay the same actions.
func OpenJournal(dir string) (*Journal, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(filepath.Join(dir, "bridge.lock"), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	if err := lockFile(lock); err != nil {
		lock.Close()
		return nil, err
	}
	j := &Journal{path: filepath.Join(dir, "replay.jsonl"), entries: map[string]*JournalEntry{}, lock: lock}
	if err := j.open(); err != nil {
		lock.Close()
		return nil, err
	}
	return j, nil
}

func (j *Journal) open() error {
	if f, err := os.Open(j.path); err == nil {
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 64<<10), 4<<20)
		for sc.Scan() {
			var e JournalEntry
			if json.Unmarshal(sc.Bytes(), &e) != nil || e.ID == "" {
				continue // a torn last line from a crash mid-write
			}
			j.apply(e)
		}
		f.Close()
		if err := sc.Err(); err != nil {
			// Compacting now would drop every entry after the failure.
			return fmt.Errorf("read %s: %w", j.path, err)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := j.compact(); err != nil {
		return err
	}
	f, err := os.OpenFile(j.path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	j.f = f
	return nil
}

func (j *Journal) apply(e JournalEntry) {
	prev := j.entries[e.ID]
	if e.Phase == "intent" {
		e.Intent = e.At
	} else if prev != nil {
		e.Intent = prev.Intent
		if e.Kind == "" {
			e.Kind, e.Repo, e.BeadID = prev.Kind, prev.Repo, prev.BeadID
		}
	}
	j.entries[e.ID] = &e
}

// compact rewrites the file with the live entries only.
func (j *Journal) compact() error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	cutoff := time.Now().Add(-journalKeep)
	for id, e := range j.entries {
		if e.Done() && e.At.Before(cutoff) {
			delete(j.entries, id)
			continue
		}
		in := *e
		in.Phase, in.At = "intent", e.Intent
		if err := enc.Encode(in); err != nil {
			return err
		}
		if e.Done() {
			if err := enc.Encode(e); err != nil {
				return err
			}
		}
	}
	return writeFileAtomic(j.path, buf.Bytes(), 0o600)
}

func (j *Journal) write(e JournalEntry) error {
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	if _, err := j.f.Write(append(b, '\n')); err != nil {
		return err
	}
	if err := j.f.Sync(); err != nil {
		return err
	}
	j.apply(e)
	return nil
}

// Get returns the entry for an action id.
func (j *Journal) Get(id string) (JournalEntry, bool) {
	e, ok := j.entries[id]
	if !ok {
		return JournalEntry{}, false
	}
	return *e, true
}

// Intent records that the bridge is about to write a through the tracker.
func (j *Journal) Intent(a api.Action, at time.Time) error {
	return j.write(JournalEntry{ID: a.ID, Phase: "intent", At: at.UTC(), Kind: a.Kind, Repo: a.Repo, BeadID: a.BeadID})
}

// Done records the outcome of the tracker write for an action.
func (j *Journal) Done(id string, r api.ActionResult, at time.Time) error {
	return j.write(JournalEntry{ID: id, Phase: "done", At: at.UTC(), Result: r})
}

// Close closes the file and releases the lock.
func (j *Journal) Close() error {
	err := j.f.Close()
	j.lock.Close()
	return err
}
