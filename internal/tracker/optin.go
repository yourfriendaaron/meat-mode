package tracker

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// OptInFile is the repo-root file every tracker honours to switch a repo's
// syncing on, with the top-level line `sync: true`.
const OptInFile = ".meat-mode.yaml"

// RepoOptIn reports whether dir/.meat-mode.yaml has `sync: true`.
func RepoOptIn(dir string) bool {
	return ConfigTrue(filepath.Join(dir, OptInFile), "sync")
}

// ConfigTrue reports whether the YAML file at path sets the top-level key
// to true (true, yes or on). The last occurrence wins, as in YAML loaders;
// a missing file is false.
func ConfigTrue(path, key string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	on := false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if k, v, ok := yamlLine(sc.Text()); ok && k == key {
			on = isTrue(v)
		}
	}
	return on
}

// yamlLine splits a top-level `key: value` line, removing quotes and a
// trailing comment.
func yamlLine(line string) (key, val string, ok bool) {
	if line == "" || line[0] == ' ' || line[0] == '\t' || line[0] == '#' {
		return "", "", false
	}
	line = stripComment(line)
	var rest string
	switch line[0] {
	case '"', '\'':
		end := strings.IndexByte(line[1:], line[0])
		if end < 0 {
			return "", "", false
		}
		key, rest = line[1:1+end], line[2+end:]
		rest, ok = strings.CutPrefix(strings.TrimLeft(rest, " \t"), ":")
	default:
		key, rest, ok = strings.Cut(line, ":")
		key = strings.TrimSpace(key)
	}
	if !ok {
		return "", "", false
	}
	return key, unquote(strings.TrimSpace(rest)), true
}

// stripComment drops a # comment that is outside quotes and preceded by
// whitespace.
func stripComment(s string) string {
	var q byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case q != 0:
			if c == q {
				q = 0
			}
		case c == '"' || c == '\'':
			q = c
		case c == '#' && i > 0 && (s[i-1] == ' ' || s[i-1] == '\t'):
			return strings.TrimRight(s[:i], " \t")
		}
	}
	return s
}

func unquote(v string) string {
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
		return v[1 : len(v)-1]
	}
	return v
}

func isTrue(v string) bool {
	switch strings.ToLower(v) {
	case "true", "yes", "on":
		return true
	}
	return false
}
