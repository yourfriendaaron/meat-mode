package store

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

// pngBytes starts like a PNG file, so the hub sniffs image/png.
var pngBytes = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR the rest of a picture of the ferns")

func attach(t *testing.T, s *Store, key, name string, body []byte) api.Attachment {
	t.Helper()
	a, err := s.AddAttachment(key, name, int64(len(body)), bytes.NewReader(body))
	must(t, err)
	return a
}

// attachmentFiles lists the names in the attachments folder.
func attachmentFiles(t *testing.T, s *Store) []string {
	t.Helper()
	entries, err := os.ReadDir(s.attachmentDir())
	must(t, err)
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

func sumOf(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func TestAddAttachment(t *testing.T) {
	s, c := open(t)
	key := hubKey(addPersonal(t, s, id1, "Water the plants", nil).BeadID)

	a := attach(t, s, key, "ferns.png", pngBytes)
	if !validAttachmentID(a.ID) || a.Name != "ferns.png" || a.Type != "image/png" || a.Size != int64(len(pngBytes)) ||
		a.SHA256 != sumOf(pngBytes) || !a.CreatedAt.Equal(c.t) {
		t.Fatalf("attachment %+v", a)
	}
	got, err := os.ReadFile(filepath.Join(s.dir, AttachmentDir, a.ID))
	must(t, err)
	if !bytes.Equal(got, pngBytes) {
		t.Fatal("the file on disk is not the bytes sent")
	}
	if files := attachmentFiles(t, s); !slices.Equal(files, []string{a.ID}) {
		t.Fatalf("attachments folder holds %v", files)
	}

	// The type comes from the bytes, never from the name.
	c.tick()
	note := attach(t, s, key, "notes.png", []byte("<html><script>alert(1)</script>"))
	if note.Type != "text/html" {
		t.Fatalf("sniffed %q", note.Type)
	}

	st, err := s.State()
	must(t, err)
	it := item(t, st, key)
	if len(it.Attachments) != 2 || it.Attachments[0] != a || it.Attachments[1] != note {
		t.Fatalf("state attachments %+v", it.Attachments)
	}
	// The stored item never carries them: they come from their own table.
	var data string
	must(t, s.db.QueryRow(`SELECT data FROM items WHERE id = ?`, it.ID).Scan(&data))
	if strings.Contains(data, "attachments") {
		t.Fatalf("item data holds attachments: %s", data)
	}
}

func TestAttachmentsOnAnAgentsTodo(t *testing.T) {
	s, _ := open(t)
	seed(t, s)
	key, _, err := s.AddPersonal(api.PersonalTodo{Title: "Approve the footer text"}, "laptop", "")
	must(t, err)
	a := attach(t, s, key, "footer.txt", []byte("Made with care"))
	_, _, id, _ := SplitKey(key)
	it, err := s.PersonalItem("laptop", "", id)
	must(t, err)
	if len(it.Attachments) != 1 || it.Attachments[0] != a {
		t.Fatalf("PersonalItem attachments %+v", it.Attachments)
	}
}

func TestAttachmentRefusals(t *testing.T) {
	s, _ := open(t)
	seed(t, s)
	key := hubKey(addPersonal(t, s, id1, "Water the plants", nil).BeadID)
	for name, c := range map[string]struct {
		key, name string
		want      error
	}{
		"agent item": {"laptop/garden/gd-1", "a.txt", ErrInvalid},
		"bad key":    {"nothing", "a.txt", ErrInvalid},
		"no item":    {hubKey("mm-99"), "a.txt", ErrNotFound},
		"not UTF-8":  {key, "a\xff.txt", ErrInvalid},
	} {
		if _, err := s.AddAttachment(c.key, c.name, 3, strings.NewReader("abc")); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", name, err, c.want)
		}
	}
	if files := attachmentFiles(t, s); len(files) != 0 {
		t.Fatalf("refused uploads left %v", files)
	}
}

func TestCleanAttachmentName(t *testing.T) {
	long := strings.Repeat("ö", 200) // 400 bytes
	for in, want := range map[string]string{
		"ferns.png":            "ferns.png",
		"  ferns.png ":         "ferns.png",
		"../../etc/passwd":     ".._.._etc_passwd",
		`notes\garden\a.txt`:   "notes_garden_a.txt",
		"line\nbreak\x00.txt":  "linebreak.txt",
		"":                     "file",
		" \t\x7f ":             "file",
		long:                   strings.Repeat("ö", 127),
		"Vacation photo.jpeg":  "Vacation photo.jpeg",
		"tab\there.txt":        "tabhere.txt",
		"emoji \U0001F331.txt": "emoji \U0001F331.txt",
	} {
		got, err := cleanAttachmentName(in)
		must(t, err)
		if got != want {
			t.Errorf("cleanAttachmentName(%q) = %q, want %q", in, got, want)
		}
		if len(got) > api.MaxAttachmentName {
			t.Errorf("%q is %d bytes", got, len(got))
		}
	}
}

func TestFileSize(t *testing.T) {
	for n, want := range map[int64]string{
		api.MaxAttachmentBytes:      "25 MB",
		api.MaxAttachmentBytes + 1:  "25.1 MB",
		32925286:                    "31.4 MB",
		api.MaxAttachmentsBytes:     "1 GB",
		api.MaxAttachmentsBytes + 1: "1.01 GB",
		1095216660:                  "1.02 GB",
	} {
		if got := fileSize(n); got != want {
			t.Errorf("fileSize(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestAttachmentCaps(t *testing.T) {
	s, _ := open(t)
	key := hubKey(addPersonal(t, s, id1, "Water the plants", nil).BeadID)

	// A size given up front is refused before a byte is read.
	_, err := s.AddAttachment(key, "Vacation.mov", 32925286, strings.NewReader(""))
	if !errors.Is(err, ErrTooLarge) || err.Error() != "Vacation.mov is 31.4 MB; a file can be at most 25 MB." {
		t.Fatalf("declared too large: %v", err)
	}
	// One that turns out longer than it said is cut off at the cap.
	_, err = s.AddAttachment(key, "Vacation.mov", -1, io.LimitReader(zeros{}, api.MaxAttachmentBytes+1))
	if !errors.Is(err, ErrTooLarge) || !strings.Contains(err.Error(), "a file can be at most 25 MB") {
		t.Fatalf("streamed too large: %v", err)
	}
	// Exactly the cap is fine.
	big, err := s.AddAttachment(key, "big.bin", -1, io.LimitReader(zeros{}, api.MaxAttachmentBytes))
	must(t, err)
	if big.Size != api.MaxAttachmentBytes {
		t.Fatalf("size %d", big.Size)
	}
	if files := attachmentFiles(t, s); !slices.Equal(files, []string{big.ID}) {
		t.Fatalf("attachments folder holds %v", files)
	}

	// Removed files count toward the hub's total until purged.
	must(t, s.RemoveAttachment(big.ID))
	_, err = s.db.Exec(`UPDATE attachments SET size = ? WHERE id = ?`, api.MaxAttachmentsBytes-10, big.ID)
	must(t, err)
	_, err = s.AddAttachment(key, "notes.txt", 30<<20/1024, strings.NewReader(strings.Repeat("a", 30<<20/1024)))
	if !errors.Is(err, ErrTooLarge) || err.Error() != "The hub's files would come to 1.01 GB; they can be at most 1 GB." {
		t.Fatalf("total: %v", err)
	}
	_, err = s.AddAttachment(key, "notes.txt", -1, strings.NewReader(strings.Repeat("a", 11)))
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("total, size not given: %v", err)
	}
	_, err = s.db.Exec(`DELETE FROM attachments`)
	must(t, err)

	for i := range api.MaxAttachmentsPerItem {
		attach(t, s, key, "note.txt", []byte{byte('a' + i)})
	}
	_, err = s.AddAttachment(key, "one-more.txt", 1, strings.NewReader("z"))
	if !errors.Is(err, ErrTooLarge) || err.Error() != "An item can hold at most 20 files." {
		t.Fatalf("count: %v", err)
	}
}

// zeros reads as an endless run of zero bytes.
type zeros struct{}

func (zeros) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

func TestRemoveRestoreAndPurge(t *testing.T) {
	s, c := open(t)
	key := hubKey(addPersonal(t, s, id1, "Water the plants", nil).BeadID)
	a := attach(t, s, key, "ferns.png", pngBytes)
	c.tick()
	b := attach(t, s, key, "seeds.txt", []byte("Tomatoes, basil"))

	must(t, s.RemoveAttachment(a.ID))
	must(t, s.RemoveAttachment(a.ID)) // again: nothing changes
	if _, _, err := s.OpenAttachment(a.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a removed file is served: %v", err)
	}
	st, err := s.State()
	must(t, err)
	if got := item(t, st, key).Attachments; len(got) != 1 || got[0] != b {
		t.Fatalf("after remove: %+v", got)
	}

	back, err := s.RestoreAttachment(a.ID)
	must(t, err)
	if back != a {
		t.Fatalf("restored %+v, want %+v", back, a)
	}
	// Back in its place, oldest first.
	st, err = s.State()
	must(t, err)
	if got := item(t, st, key).Attachments; len(got) != 2 || got[0] != a || got[1] != b {
		t.Fatalf("after restore: %+v", got)
	}
	_, f, err := s.OpenAttachment(a.ID)
	must(t, err)
	f.Close()

	// No room to put it back.
	must(t, s.RemoveAttachment(a.ID))
	for range api.MaxAttachmentsPerItem - 1 {
		attach(t, s, key, "note.txt", []byte("x"))
	}
	if _, err := s.RestoreAttachment(a.ID); !errors.Is(err, ErrConflict) || err.Error() != "An item can hold at most 20 files." {
		t.Fatalf("restore past the cap: %v", err)
	}

	// Purged 24 hours after removal, not before.
	c.t = c.t.Add(AttachmentPurgeAfter - time.Second)
	rep, err := s.SweepAttachments()
	must(t, err)
	if rep.Purged != 0 {
		t.Fatalf("purged early: %+v", rep)
	}
	c.t = c.t.Add(time.Second)
	rep, err = s.SweepAttachments()
	must(t, err)
	if rep.Purged != 1 || len(rep.Strays) != 0 || len(rep.Missing) != 0 {
		t.Fatalf("sweep %+v", rep)
	}
	if _, err := os.Stat(s.attachmentPath(a.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("purged file still there: %v", err)
	}
	if _, err := s.RestoreAttachment(a.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("restore after purge: %v", err)
	}
	if err := s.RemoveAttachment(a.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("remove after purge: %v", err)
	}
}

func TestSweepAttachments(t *testing.T) {
	s, _ := open(t)
	key := hubKey(addPersonal(t, s, id1, "Water the plants", nil).BeadID)
	kept := attach(t, s, key, "ferns.png", pngBytes)
	lost := attach(t, s, key, "seeds.txt", []byte("Tomatoes"))
	must(t, os.Remove(s.attachmentPath(lost.ID)))

	dir := s.attachmentDir()
	write := func(name string) {
		must(t, os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o640))
	}
	orphan, busy := newAttachmentID(), newAttachmentID()
	write(tempPrefix + newAttachmentID()) // a crash mid-upload
	write(orphan)                         // a file whose row went
	write(tempPrefix + busy)              // an upload in flight
	write(busy)
	done := s.track(busy)

	rep, err := s.SweepAttachments()
	must(t, err)
	slices.Sort(rep.Strays)
	if rep.Purged != 0 || len(rep.Strays) != 2 || !slices.Equal(rep.Missing, []string{lost.ID}) {
		t.Fatalf("sweep %+v", rep)
	}
	files := attachmentFiles(t, s)
	want := []string{tempPrefix + busy, busy, kept.ID}
	slices.Sort(want)
	if !slices.Equal(files, want) {
		t.Fatalf("left %v, want %v", files, want)
	}

	// Once out of flight with no row, the next sweep takes it.
	done()
	rep, err = s.SweepAttachments()
	must(t, err)
	if len(rep.Strays) != 2 {
		t.Fatalf("second sweep %+v", rep)
	}
}

// Should an item row ever go, its attachment rows go with it, and the sweep
// deletes their files.
func TestAttachmentsGoWithTheirItem(t *testing.T) {
	s, _ := open(t)
	add := addPersonal(t, s, id1, "Water the plants", nil)
	a := attach(t, s, hubKey(add.BeadID), "ferns.png", pngBytes)
	_, err := s.db.Exec(`DELETE FROM items WHERE bridge = ? AND id = ?`, HubBridge, add.BeadID)
	must(t, err)
	var n int
	must(t, s.db.QueryRow(`SELECT COUNT(*) FROM attachments`).Scan(&n))
	if n != 0 {
		t.Fatalf("%d attachment rows outlived their item", n)
	}
	rep, err := s.SweepAttachments()
	must(t, err)
	if !slices.Equal(rep.Strays, []string{a.ID}) {
		t.Fatalf("sweep %+v", rep)
	}
}

// A bridge cannot put attachments on its items by sending them.
func TestAttachmentsFromABridgeAreDropped(t *testing.T) {
	s, _ := open(t)
	seed(t, s)
	must(t, s.ReplaceItems("laptop", "garden", []api.Item{{ID: "gd-1", Title: "Water the plants", Status: "open",
		Attachments: []api.Attachment{{ID: strings.Repeat("a", 32), Name: "x.png", Type: "image/png"}}}}))
	st, err := s.State()
	must(t, err)
	if got := item(t, st, "laptop/garden/gd-1").Attachments; got != nil {
		t.Fatalf("bridge item shows %+v", got)
	}
}
