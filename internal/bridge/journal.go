package bridge

import (
	"bufio"
	"encoding/json"
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

// Done reports whether the bd step finished.
func (e JournalEntry) Done() bool { return e.Phase == "done" }

// OpenJournal opens (or creates) dir/replay.jsonl, dropping finished entries
// older than 30 days.
func OpenJournal(dir string) (*Journal, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	j := &Journal{path: filepath.Join(dir, "replay.jsonl"), entries: map[string]*JournalEntry{}}
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
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if err := j.compact(); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(j.path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	j.f = f
	return j, nil
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
	tmp := j.path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	enc := json.NewEncoder(w)
	cutoff := time.Now().Add(-journalKeep)
	for id, e := range j.entries {
		if e.Done() && e.At.Before(cutoff) {
			delete(j.entries, id)
			continue
		}
		in := *e
		in.Phase, in.At = "intent", e.Intent
		if err := enc.Encode(in); err != nil {
			f.Close()
			return err
		}
		if e.Done() {
			if err := enc.Encode(e); err != nil {
				f.Close()
				return err
			}
		}
	}
	if err := w.Flush(); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, j.path); err != nil {
		return err
	}
	return syncDir(filepath.Dir(j.path))
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	_ = d.Sync() // not supported everywhere; the rename is still atomic
	return nil
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

// Intent records that the bridge is about to run bd for a.
func (j *Journal) Intent(a api.Action, at time.Time) error {
	return j.write(JournalEntry{ID: a.ID, Phase: "intent", At: at.UTC(), Kind: a.Kind, Repo: a.Repo, BeadID: a.BeadID})
}

// Done records the outcome of the bd step for an action.
func (j *Journal) Done(id string, r api.ActionResult, at time.Time) error {
	return j.write(JournalEntry{ID: id, Phase: "done", At: at.UTC(), Result: r})
}

// Close closes the file.
func (j *Journal) Close() error { return j.f.Close() }
