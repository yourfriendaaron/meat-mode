package bridge

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

// Icon timing (docs/api.md, List identity).
const (
	IconsEvery   = 10 * time.Minute // how often the bridge looks for a repo's icon, at most
	IconsBackoff = time.Hour        // how long it sends no icons to a hub that has no icons route
)

// Where FindIcon looks (docs/api.md, List identity, Icons).
var (
	iconNames   = []string{"favicon.svg", "icon.svg", "favicon.png", "apple-touch-icon.png", "icon.png", "favicon.ico"}
	iconDirs    = []string{"", "public", "static", "app", "src", filepath.Join("src", "app"), "assets", filepath.Join("web", "public")}
	iconPages   = []string{"index.html", filepath.Join("public", "index.html"), filepath.Join("src", "index.html"), filepath.Join("web", "index.html")}
	iconSubdirs = []string{"", "public", "static", "icons"}
	iconParents = []string{"apps", "packages"}
)

// maxIconPage is how much of an index.html FindIcon reads for its links.
const maxIconPage = 256 << 10

// FindIcon returns the icon of the repo in dir, or nil when it has none: the
// first candidate, in the order docs/api.md (List identity) gives, that is
// a regular file of at most api.MaxIconBytes inside the repo once symbolic
// links are resolved and that api.SniffIcon accepts. It never fails: a
// candidate it cannot read is skipped.
func FindIcon(dir string) []byte {
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil
	}
	f := iconFinder{dir: dir, root: root}
	for _, name := range iconNames {
		for _, sub := range iconDirs {
			if b := f.icon(filepath.Join(sub, name)); b != nil {
				return b
			}
		}
	}
	for _, page := range iconPages {
		doc := f.read(page, maxIconPage, false)
		if doc == nil {
			continue
		}
		for _, href := range iconLinks(doc) {
			rel, ok := linkPath(filepath.Dir(page), href)
			if !ok {
				continue
			}
			if b := f.icon(rel); b != nil {
				return b
			}
		}
	}
	var folders []string
	for _, parent := range iconParents {
		entries, err := os.ReadDir(filepath.Join(dir, parent))
		if err != nil {
			continue
		}
		for _, e := range entries {
			folders = append(folders, parent+"/"+e.Name())
		}
	}
	slices.Sort(folders)
	for _, folder := range folders {
		for _, name := range iconNames {
			for _, sub := range iconSubdirs {
				if b := f.icon(filepath.Join(filepath.FromSlash(folder), sub, name)); b != nil {
					return b
				}
			}
		}
	}
	return nil
}

// iconFinder reads candidates in one repo: dir as given and root, its real
// path.
type iconFinder struct{ dir, root string }

// icon returns the candidate at rel, a path inside the repo, when it is an
// icon.
func (f iconFinder) icon(rel string) []byte {
	b := f.read(rel, api.MaxIconBytes, true)
	if _, ok := api.SniffIcon(b); !ok {
		return nil
	}
	return b
}

// read returns the file at rel when its real path is inside the repo's and
// it is a regular file: all of it, or nil past limit bytes when whole is
// set, else its first limit bytes. The size is checked before the file is
// opened, and the read is capped whatever the file grows to.
func (f iconFinder) read(rel string, limit int64, whole bool) []byte {
	path, err := filepath.EvalSymlinks(filepath.Join(f.dir, rel))
	if err != nil || !inside(f.root, path) {
		return nil
	}
	fi, err := os.Lstat(path)
	if err != nil || !fi.Mode().IsRegular() || (whole && fi.Size() > limit) {
		return nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()
	if now, err := file.Stat(); err != nil || !os.SameFile(fi, now) {
		return nil // swapped since the check
	}
	b, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil
	}
	if int64(len(b)) > limit {
		if whole {
			return nil
		}
		b = b[:limit]
	}
	return b
}

// inside reports whether path is root or below it; both are real paths.
func inside(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// linkPath is a link's href as a path inside the repo, read relative to
// folder, the page's folder inside the repo. ok is false for an href with
// a scheme (a data: URL among them), one starting with / and one that
// leaves the repo.
func linkPath(folder, href string) (string, bool) {
	if i := strings.IndexAny(href, "?#"); i >= 0 {
		href = href[:i]
	}
	if href == "" || strings.HasPrefix(href, "/") || strings.HasPrefix(href, `\`) {
		return "", false
	}
	if i := strings.IndexAny(href, ":/"); i >= 0 && href[i] == ':' {
		return "", false // a scheme, before any slash
	}
	p, err := url.PathUnescape(href)
	if err != nil {
		return "", false
	}
	rel := filepath.Join(folder, filepath.FromSlash(p))
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", false
	}
	return rel, true
}

// iconLinks returns, in document order, the href of each <link> in doc
// whose rel holds the token icon or apple-touch-icon, in any case. It is a
// tolerant scan, not a parser: it skips comments, reads attributes quoted
// or not, and stops at a quote that never closes.
func iconLinks(doc []byte) []string {
	lower := asciiLower(doc)
	var out []string
	for i := 0; i < len(lower); {
		j := strings.IndexByte(lower[i:], '<')
		if j < 0 {
			break
		}
		i += j
		if strings.HasPrefix(lower[i:], "<!--") {
			end := strings.Index(lower[i+4:], "-->")
			if end < 0 {
				break
			}
			i += 4 + end + 3
			continue
		}
		if !strings.HasPrefix(lower[i:], "<link") || i+5 >= len(lower) || !strings.ContainsRune(" \t\r\n\f/>", rune(lower[i+5])) {
			i++
			continue
		}
		attrs, next, ok := tagAttrs(string(doc), lower, i+5)
		if !ok {
			break
		}
		i = next
		if href, has := attrs["href"]; has && iconRel(attrs["rel"]) {
			out = append(out, strings.TrimSpace(href))
		}
	}
	return out
}

// tagAttrs reads a tag's attributes from doc at i up to its closing >,
// with names from lower, doc in ASCII lower case. It returns the first
// value of each name, where the scan goes on, and false when the tag
// never ends.
func tagAttrs(doc, lower string, i int) (map[string]string, int, bool) {
	attrs := map[string]string{}
	space := func(c byte) bool { return c == ' ' || c == '\t' || c == '\r' || c == '\n' || c == '\f' }
	for {
		for i < len(lower) && (space(lower[i]) || lower[i] == '/') {
			i++
		}
		if i >= len(lower) {
			return nil, i, false
		}
		if lower[i] == '>' {
			return attrs, i + 1, true
		}
		start := i
		for i < len(lower) && !space(lower[i]) && lower[i] != '=' && lower[i] != '>' && lower[i] != '/' {
			i++
		}
		name := lower[start:i]
		for i < len(lower) && space(lower[i]) {
			i++
		}
		value := ""
		if i < len(lower) && lower[i] == '=' {
			i++
			for i < len(lower) && space(lower[i]) {
				i++
			}
			if i < len(lower) && (lower[i] == '"' || lower[i] == '\'') {
				end := strings.IndexByte(lower[i+1:], lower[i])
				if end < 0 {
					return nil, len(lower), false
				}
				value = doc[i+1 : i+1+end]
				i += end + 2
			} else {
				start := i
				for i < len(lower) && !space(lower[i]) && lower[i] != '>' {
					i++
				}
				value = doc[start:i]
			}
		}
		if _, seen := attrs[name]; !seen && name != "" {
			attrs[name] = value
		}
	}
}

// iconRel reports whether a rel value holds the token icon or
// apple-touch-icon.
func iconRel(rel string) bool {
	for _, tok := range strings.Fields(strings.ToLower(rel)) {
		if tok == "icon" || tok == "apple-touch-icon" {
			return true
		}
	}
	return false
}

// asciiLower is b with ASCII letters in lower case and every other byte
// as it is, so offsets in it are offsets in b.
func asciiLower(b []byte) string {
	out := make([]byte, len(b))
	for i, c := range b {
		if 'A' <= c && c <= 'Z' {
			c += 'a' - 'A'
		}
		out[i] = c
	}
	return string(out)
}

// iconState is what the icon pass knows of one repo.
type iconState struct {
	dir      string
	lookedAt time.Time // the last look
	took     bool      // the hub took an icon, or none, since the start
	sent     string    // api.IconHash of what it took; "" for none
	// refused holds the hashes the hub refused (400 or 413); none is sent
	// again, and each is logged once.
	refused map[string]bool
}

func (b *Bridge) iconsEvery() time.Duration {
	if b.Poll > 0 {
		return b.Poll
	}
	return IconsEvery
}

// syncIcons is the icon pass: for each repo in the last repo report the hub
// took, it looks for the repo's icon at most once per IconsEvery and sends
// it when its hash differs from the last one the hub took, so after a start
// every repo's icon goes once, an empty body for a repo with none. It runs
// after discover, so the hub holds each repo it sends. A 404, 409, 400 or
// 413 stops nothing else; other hub errors leave the icon unsent until the
// next look and are returned as items' are.
func (b *Bridge) syncIcons(ctx context.Context) error {
	if b.iconStates == nil {
		b.iconStates = map[string]*iconState{}
	}
	reported := map[string]bool{}
	names := make([]string, 0, len(b.reposSent))
	for _, r := range b.reposSent {
		if st := b.repos[r.Name]; st != nil {
			reported[r.Name] = true
			names = append(names, r.Name)
		}
	}
	for name := range b.iconStates {
		if !reported[name] {
			delete(b.iconStates, name)
		}
	}
	if b.now().Before(b.iconsOffUntil) {
		return nil
	}
	slices.Sort(names)
	for _, name := range names {
		dir := b.repos[name].Dir
		is := b.iconStates[name]
		if is == nil || is.dir != dir {
			is = &iconState{dir: dir, refused: map[string]bool{}}
			b.iconStates[name] = is
		}
		if !is.lookedAt.IsZero() && b.now().Sub(is.lookedAt) < b.iconsEvery() {
			continue
		}
		is.lookedAt = b.now()
		icon := FindIcon(dir)
		sum := ""
		if icon != nil {
			sum = api.IconHash(icon)
		}
		if is.refused[sum] || (is.took && sum == is.sent) {
			continue
		}
		err := b.Hub.PutIcon(ctx, name, icon)
		var h *HTTPError
		switch {
		case err == nil:
			is.took, is.sent = true, sum
		case errors.As(err, &h) && h.Status == http.StatusNotFound:
			if !b.iconsGoneLogged {
				b.log().Warn("the hub does not take icons; sending none to it for an hour", "err", err)
				b.iconsGoneLogged = true
			}
			is.lookedAt = time.Time{}
			b.iconsOffUntil = b.now().Add(IconsBackoff)
			return nil
		case errors.As(err, &h) && h.Status == http.StatusConflict:
			// The hub's repo list is not ours; report it again, then send.
			b.log().Warn("the hub does not hold this repo; reporting the repos again", "repo", name, "err", err)
			b.reposSent = nil
			is.took, is.sent, is.lookedAt = false, "", time.Time{}
		case refusedSet(err):
			b.log().Warn("the hub refused this repo's icon; not sending it again", "repo", name, "icon", sum, "err", err)
			is.refused[sum] = true
		default:
			return hubError{err}
		}
	}
	return nil
}
