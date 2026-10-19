package store

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

// zipEntry is one entry of a zip a test builds.
type zipEntry struct {
	name string
	data []byte
}

func buildZip(t *testing.T, entries []zipEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: e.name, Method: zip.Store})
		must(t, err)
		_, err = w.Write(e.data)
		must(t, err)
	}
	must(t, zw.Close())
	return buf.Bytes()
}

// hubWithFiles is a hub with tagged items on the personal list and a
// project, three attachments on them and one removed.
func hubWithFiles(t *testing.T) (*Store, map[string][]byte) {
	t.Helper()
	s, c := open(t)
	key := hubKey(addPersonal(t, s, id1, "Water the plants", nil).BeadID)
	// Tags straight into the row: setting them is the edit's work.
	tx, err := s.db.Begin()
	must(t, err)
	it, err := hubItem(tx, PersonalRepo, strings.TrimPrefix(key, "hub/personal/"))
	must(t, err)
	it.Labels = []string{"garden", "Weekly"}
	must(t, putHubItem(tx, PersonalRepo, it, it.FirstSeen, nil))
	must(t, tx.Commit())
	seed(t, s)
	project, _, err := s.AddPersonal(api.PersonalTodo{Title: "Order compost for the beds"}, "laptop", garden)
	must(t, err)
	files := map[string][]byte{}
	for _, f := range []struct {
		key, name string
		data      []byte
	}{
		{key, "ferns.png", pngBytes},
		{key, "Seed list ö.txt", []byte("Tomatoes, basil, chard")},
		{project, "empty.bin", nil},
		{project, "compost.pdf", []byte("%PDF-1.4 a quote for compost")},
	} {
		c.tick()
		a := attach(t, s, f.key, f.name, f.data)
		files[a.ID] = f.data
	}
	// Remove the last: an export leaves it out.
	var last string
	must(t, s.db.QueryRow(`SELECT id FROM attachments ORDER BY rowid DESC LIMIT 1`).Scan(&last))
	must(t, s.RemoveAttachment(last))
	delete(files, last)
	return s, files
}

func exportZip(t *testing.T, s *Store) (api.Export, []byte) {
	t.Helper()
	e, err := s.Export()
	must(t, err)
	var buf bytes.Buffer
	must(t, s.WriteExportZip(&buf, e))
	return e, buf.Bytes()
}

// An export with attachments restores into a fresh hub with the same
// files, byte for byte, and the same metadata and tags.
func TestExportZipRestoresIntoAFreshHub(t *testing.T) {
	s, files := hubWithFiles(t)
	first, file := exportZip(t, s)

	// The zip: export.json first, then each file as stored bytes.
	zr, err := zip.NewReader(bytes.NewReader(file), int64(len(file)))
	must(t, err)
	if zr.File[0].Name != api.ExportJSONName || len(zr.File) != 1+len(files) {
		t.Fatalf("zip entries %d, first %q", len(zr.File), zr.File[0].Name)
	}
	for _, f := range zr.File[1:] {
		id := strings.TrimPrefix(f.Name, api.ExportAttachmentDir)
		if f.Method != zip.Store || files[id] == nil && f.UncompressedSize64 != 0 {
			t.Errorf("entry %s method %d", f.Name, f.Method)
		}
	}
	n := 0
	for _, it := range first.Items {
		for _, a := range it.Attachments {
			n++
			if a.SHA256 != sumOf(files[a.ID]) || a.Size != int64(len(files[a.ID])) {
				t.Errorf("listed %+v", a)
			}
		}
		if it.Title == "Water the plants" && !slices.Equal(it.Labels, []string{"garden", "Weekly"}) {
			t.Errorf("labels %q", it.Labels)
		}
	}
	if n != len(files) {
		t.Fatalf("export lists %d files, want %d", n, len(files))
	}

	fresh, _ := open(t)
	res, err := fresh.Restore(bytes.NewReader(file))
	must(t, err)
	if res.Attachments != len(files) || res.Items != len(first.Items) {
		t.Fatalf("result %+v", res)
	}
	for id, data := range files {
		got, err := os.ReadFile(fresh.attachmentPath(id))
		must(t, err)
		if !bytes.Equal(got, data) {
			t.Errorf("file %s differs after the restore", id)
		}
	}
	if got := attachmentFiles(t, fresh); len(got) != len(files) {
		t.Errorf("fresh hub holds %v; the spooled zip is gone", got)
	}
	second, err := fresh.Export()
	must(t, err)
	// A restore pairs no machine; all else comes back as it was.
	second.ExportedAt = first.ExportedAt
	first.Machines = []string{}
	a, _ := json.Marshal(first)
	b, _ := json.Marshal(second)
	if string(a) != string(b) {
		t.Errorf("export after restore\n got %s\nwant %s", b, a)
	}
}

func TestExportStopsOnAMissingFile(t *testing.T) {
	s, files := hubWithFiles(t)
	for id := range files {
		must(t, os.Remove(s.attachmentPath(id)))
		break
	}
	if _, err := s.Export(); !errors.Is(err, ErrFileMissing) || !strings.Contains(err.Error(), "stops rather than leave it out") {
		t.Fatalf("export with a file missing: %v", err)
	}
}

func TestExportZipStopsOnAChangedFile(t *testing.T) {
	s, files := hubWithFiles(t)
	e, err := s.Export()
	must(t, err)
	for id, data := range files {
		if len(data) > 0 {
			must(t, os.WriteFile(s.attachmentPath(id), bytes.Repeat([]byte("x"), len(data)), 0o640))
			break
		}
	}
	if err := s.WriteExportZip(io.Discard, e); err == nil || !strings.Contains(err.Error(), "no longer matches") {
		t.Fatalf("export with a changed file: %v", err)
	}
}

// A zip that fails any check changes nothing and leaves no file behind.
func TestRestoreZipChecksEveryFile(t *testing.T) {
	src, files := hubWithFiles(t)
	e, err := src.Export()
	must(t, err)
	var ids []string
	for _, it := range e.Items {
		for _, a := range it.Attachments {
			ids = append(ids, a.ID)
		}
	}
	// zipOf builds the zip of ex with an entry per listed file, changed by
	// fn before it is written.
	zipOf := func(fn func(ex *api.Export, entries *[]zipEntry)) []byte {
		ex := e
		ex.Items = slices.Clone(e.Items)
		for i := range ex.Items {
			ex.Items[i].Attachments = slices.Clone(ex.Items[i].Attachments)
		}
		var entries []zipEntry
		for _, id := range ids {
			entries = append(entries, zipEntry{api.ExportAttachmentDir + id, files[id]})
		}
		fn(&ex, &entries)
		js, err := json.Marshal(ex)
		must(t, err)
		return buildZip(t, append([]zipEntry{{api.ExportJSONName, js}}, entries...))
	}
	first := func(ex *api.Export) *api.Attachment { return &ex.Items[0].Attachments[0] }
	cases := map[string]struct {
		file []byte
		says string
	}{
		"missing file": {zipOf(func(_ *api.Export, en *[]zipEntry) { *en = (*en)[1:] }), "has no file in the export"},
		"unlisted file": {zipOf(func(_ *api.Export, en *[]zipEntry) {
			*en = append(*en, zipEntry{api.ExportAttachmentDir + newAttachmentID(), []byte("x")})
		}), "which no item lists"},
		"odd name":     {zipOf(func(_ *api.Export, en *[]zipEntry) { *en = append(*en, zipEntry{"__MACOSX/._ferns.png", nil}) }), "not part of an export"},
		"path in name": {zipOf(func(_ *api.Export, en *[]zipEntry) { (*en)[0].name = "attachments/../../" + ids[0] }), "not part of an export"},
		"folder entry": {zipOf(func(_ *api.Export, en *[]zipEntry) { *en = append(*en, zipEntry{"attachments/", nil}) }), "not part of an export"},
		"twice":        {zipOf(func(_ *api.Export, en *[]zipEntry) { *en = append(*en, (*en)[0]) }), "twice"},
		"size":         {zipOf(func(ex *api.Export, _ *[]zipEntry) { first(ex).Size++ }), "bytes in the export"},
		"hash": {zipOf(func(_ *api.Export, en *[]zipEntry) {
			(*en)[0].data = bytes.Repeat([]byte("x"), len((*en)[0].data))
		}), "does not match the size and SHA-256"},
		"bad sha":      {zipOf(func(ex *api.Export, _ *[]zipEntry) { first(ex).SHA256 = "ABC" }), "sha256"},
		"bad id":       {zipOf(func(ex *api.Export, _ *[]zipEntry) { first(ex).ID = "ferns" }), "32 lowercase hex"},
		"dirty name":   {zipOf(func(ex *api.Export, _ *[]zipEntry) { first(ex).Name = "../ferns.png" }), "not a clean file name"},
		"file too big": {zipOf(func(ex *api.Export, _ *[]zipEntry) { first(ex).Size = api.MaxAttachmentBytes + 1 }), "a file can be at most 25 MB"},
		"id twice": {zipOf(func(ex *api.Export, _ *[]zipEntry) {
			ex.Items[1].Attachments = append(ex.Items[1].Attachments, *first(ex))
		}), "listed twice"},
		"too many": {zipOf(func(ex *api.Export, _ *[]zipEntry) {
			for range api.MaxAttachmentsPerItem {
				ex.Items[0].Attachments = append(ex.Items[0].Attachments, *first(ex))
			}
		}), "at most 20"},
		"bad tag":        {zipOf(func(ex *api.Export, _ *[]zipEntry) { ex.Items[0].Labels = []string{"two words"} }), "one word"},
		"no export.json": {buildZip(t, []zipEntry{{api.ExportAttachmentDir + ids[0], files[ids[0]]}}), "has no export.json"},
		"not a zip":      {append([]byte("PK\x03\x04"), []byte("Water the plants")...), "not a zip file"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			s, _ := open(t)
			before := snapshot(t, s)
			_, err := s.Restore(bytes.NewReader(c.file))
			if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), c.says) {
				t.Fatalf("got %v, want an invalid error saying %q", err, c.says)
			}
			if snapshot(t, s) != before {
				t.Fatal("the refused restore changed the hub")
			}
			if left := attachmentFiles(t, s); len(left) != 0 {
				t.Fatalf("the refused restore left %v", left)
			}
		})
	}
}

// A bare JSON file of version 2 that lists attachments is refused, naming
// how many, so the user restores the zip; one that lists none restores.
func TestRestoreBareJSONOfVersion2(t *testing.T) {
	src, _ := hubWithFiles(t)
	e, err := src.Export()
	must(t, err)
	js, err := json.Marshal(e)
	must(t, err)
	s, _ := open(t)
	_, err = s.Restore(bytes.NewReader(js))
	if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "lists 3 attached files") || !strings.Contains(err.Error(), "zip") {
		t.Fatalf("bare JSON with attachments: %v", err)
	}

	for i := range e.Items {
		e.Items[i].Attachments = nil
	}
	js, err = json.Marshal(e)
	must(t, err)
	res, err := s.Restore(bytes.NewReader(js))
	must(t, err)
	if res.Items != len(e.Items) || res.Attachments != 0 {
		t.Fatalf("result %+v", res)
	}
}

// Tags in a file are cleaned by the tag rules: human and repeats dropped.
func TestRestoreCleansTags(t *testing.T) {
	s, _ := open(t)
	_, err := s.Restore(bytes.NewReader(changeExport(t, func(ex map[string]any) {
		exportList(ex, "items", 0)["labels"] = []any{"garden", "human", "Garden", " weekly "}
	})))
	must(t, err)
	tx, err := s.db.Begin()
	must(t, err)
	defer tx.Rollback()
	it, err := hubItem(tx, PersonalRepo, "mm-1")
	must(t, err)
	if !slices.Equal(it.Labels, []string{"garden", "weekly"}) {
		t.Fatalf("labels %q", it.Labels)
	}
}

func TestRestoreRefusesTooMuchJSON(t *testing.T) {
	s, _ := open(t)
	_, err := s.Restore(io.LimitReader(spaces{}, MaxExportJSON+1))
	if !errors.Is(err, ErrTooLarge) || !strings.Contains(err.Error(), "64 MB") {
		t.Fatalf("%v", err)
	}
}

type spaces struct{}

func (spaces) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = ' '
	}
	return len(p), nil
}

// A hub with data refuses a zip before reading it.
func TestRestoreZipRefusesAHubWithData(t *testing.T) {
	src, _ := hubWithFiles(t)
	_, file := exportZip(t, src)
	if _, err := src.Restore(bytes.NewReader(file)); !errors.Is(err, ErrHubInUse) {
		t.Fatalf("%v", err)
	}
}

// Two restores of one zip at once: one loads it, the other finds the hub
// in use, and every file the first wrote stays.
func TestRestoresRunOneAtATime(t *testing.T) {
	src, files := hubWithFiles(t)
	_, file := exportZip(t, src)
	s, _ := open(t)
	errs := make(chan error, 2)
	for range 2 {
		go func() {
			_, err := s.Restore(bytes.NewReader(file))
			errs <- err
		}()
	}
	a, b := <-errs, <-errs
	if (a == nil) == (b == nil) || !errors.Is(errors.Join(a, b), ErrHubInUse) {
		t.Fatalf("restores answered %v and %v", a, b)
	}
	for id, data := range files {
		got, err := os.ReadFile(s.attachmentPath(id))
		must(t, err)
		if !bytes.Equal(got, data) {
			t.Errorf("file %s differs", id)
		}
	}
}
