package bridge

import (
	"os"
	"path/filepath"
)

// writeFileAtomic replaces the file at path with data so a crash leaves
// either the old file or the new one, never a torn one. It writes a sibling
// temp file with mode perm, fsyncs it, renames it over path and syncs the
// directory so the rename survives a power cut. The directory must exist.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	// OpenFile applies the umask and keeps the mode of a leftover temp file.
	if err := f.Chmod(perm); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
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
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	return syncDir(filepath.Dir(path))
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
