package tracker

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// StoreIDer is what a tracker implements whose store has an identity that
// is the same in every clone of the repo, on any machine. The bridge sends
// a hash of it, so the hub can show clones of one store as one list
// (docs/api.md, Linked lists).
type StoreIDer interface {
	// StoreID returns the identity of the store in dir, or "" when it has
	// none. It reads files only, and must stay cheap: the bridge calls it
	// for every repo on each scan.
	StoreID(dir string) string
}

// maxStoreIDLen bounds a store identity read from a repo's files.
const maxStoreIDLen = 512

// CleanStoreID returns s trimmed when it is a usable identity: printable,
// without spaces, and at most maxStoreIDLen bytes; else "".
func CleanStoreID(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > maxStoreIDLen {
		return ""
	}
	for _, r := range s {
		if !unicode.IsPrint(r) || unicode.IsSpace(r) {
			return ""
		}
	}
	return s
}

// maxGitConfigBytes is how much of a Git config file GitStoreID reads.
const maxGitConfigBytes = 1 << 20

// GitStoreID is the identity of a store kept in Git files, as Backlog.md's
// and ticket's are: the repo's origin remote, without its scheme, user,
// port or trailing .git, then # and dir's path below the Git root, so two
// stores in one repo stay apart. It reads .git/config, from dir up to the
// root, and runs nothing. It is "" outside a Git repo, for a repo with no
// origin, and in a linked worktree, which the bridge skips anyway.
func GitStoreID(dir string) string {
	dir = filepath.Clean(dir)
	for root := dir; ; {
		gitDir := filepath.Join(root, ".git")
		if fi, err := os.Lstat(gitDir); err == nil {
			if !fi.IsDir() {
				return ""
			}
			origin := normaliseRemote(originURL(filepath.Join(gitDir, "config")))
			if origin == "" {
				return ""
			}
			rel, err := filepath.Rel(root, dir)
			if err != nil {
				return ""
			}
			return CleanStoreID(origin + "#" + filepath.ToSlash(rel))
		}
		up := filepath.Dir(root)
		if up == root {
			return ""
		}
		root = up
	}
}

// originURL is the url of [remote "origin"] in the Git config at path.
func originURL(path string) string {
	data, err := ReadRegular(path, maxGitConfigBytes)
	if err != nil {
		return ""
	}
	in := false
	url := ""
	sc := bufio.NewScanner(bytes.NewReader(data[:min(len(data), maxGitConfigBytes)]))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "[") {
			in = strings.EqualFold(strings.Join(strings.Fields(line), " "), `[remote "origin"]`)
			continue
		}
		if !in {
			continue
		}
		if k, v, ok := strings.Cut(line, "="); ok && strings.EqualFold(strings.TrimSpace(k), "url") {
			url = strings.Trim(strings.TrimSpace(v), `"`)
		}
	}
	return url
}

// normaliseRemote turns the forms one remote takes, such as
// https://user@example.com/team/garden.git and
// git@example.com:team/garden, into one: example.com/team/garden, with the
// host in lower case. It is "" for a local path.
func normaliseRemote(u string) string {
	u = strings.TrimSpace(u)
	if u == "" {
		return ""
	}
	if scheme, rest, ok := strings.Cut(u, "://"); ok {
		// A file URL is a local path, whatever its host says.
		if strings.EqualFold(scheme, "file") {
			return ""
		}
		u = rest
	} else if i, j := strings.IndexByte(u, ':'), strings.IndexByte(u, '/'); i > 1 && (j < 0 || i < j) {
		// scp-like user@host:path; one letter before the colon is a
		// Windows drive, a local path.
		u = u[:i] + "/" + u[i+1:]
	} else {
		return ""
	}
	if at := strings.LastIndexByte(strings.SplitN(u, "/", 2)[0], '@'); at >= 0 {
		u = u[at+1:]
	}
	host, path, _ := strings.Cut(u, "/")
	if c := strings.LastIndexByte(host, ':'); c >= 0 && !strings.Contains(host, "]") {
		host = host[:c]
	}
	path = strings.TrimSuffix(strings.Trim(path, "/"), ".git")
	if host == "" || path == "" {
		return ""
	}
	return strings.ToLower(host) + "/" + path
}
