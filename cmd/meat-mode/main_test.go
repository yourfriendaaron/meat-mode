package main

import "testing"

func TestVersionWithCommit(t *testing.T) {
	for _, c := range []struct{ version, commit, revision, want string }{
		{"v1.0.0", "99975ec", "", "v1.0.0 (99975ec)"},
		{"v1.0.0", "", "99975eccd10b4f79aa1707c1b996ccc38f4091d5", "v1.0.0 (99975ec)"},
		{"v1.0.0", "99975eccd10b4f79aa1707c1b996ccc38f4091d5", "", "v1.0.0 (99975ec)"},
		{"dev", "", "", "dev"},
		{"99975ec", "99975ec", "", "99975ec"},
	} {
		if got := versionWithCommit(c.version, c.commit, c.revision); got != c.want {
			t.Errorf("versionWithCommit(%q, %q, %q) = %q, want %q", c.version, c.commit, c.revision, got, c.want)
		}
	}
}
