package bridge

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

// Invented icons, one of each type, and bytes that are none.
var (
	svgIcon = []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16"><circle cx="8" cy="8" r="6"/></svg>`)
	pngIcon = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR leaf")
	icoIcon = []byte("\x00\x00\x01\x00\x01\x00\x10\x10 pot")
	notIcon = []byte("Water the plants, not an image.")
)

// iconOf is an SVG icon told apart from others by its title.
func iconOf(title string) []byte {
	return []byte(`<svg xmlns="http://www.w3.org/2000/svg"><title>` + title + `</title></svg>`)
}

// page is an index.html whose head holds the given tags.
func page(head string) []byte {
	return []byte("<!doctype html>\n<html><head>\n" + head + "\n</head><body>Garden</body></html>\n")
}

func TestFindIcon(t *testing.T) {
	big := append(append([]byte{}, svgIcon[:len(svgIcon)-6]...), bytes.Repeat([]byte(" "), api.MaxIconBytes)...)
	big = append(big, "</svg>"...)
	edge := append(append([]byte{}, svgIcon...), bytes.Repeat([]byte(" "), api.MaxIconBytes-len(svgIcon))...)
	for _, c := range []struct {
		name  string
		files map[string][]byte
		setup func(t *testing.T, repo string)
		want  string // the file whose bytes are the icon; "" for none
	}{
		{name: "none", files: map[string][]byte{"README.md": notIcon}},
		{name: "root favicon.svg", files: map[string][]byte{"favicon.svg": svgIcon}, want: "favicon.svg"},
		{name: "root before public", files: map[string][]byte{"favicon.svg": iconOf("root"), "public/favicon.svg": iconOf("public")}, want: "favicon.svg"},
		{name: "name before folder", files: map[string][]byte{"icon.svg": iconOf("root"), "web/public/favicon.svg": iconOf("deep")}, want: "web/public/favicon.svg"},
		{name: "every folder of step one", files: map[string][]byte{"src/app/favicon.ico": icoIcon}, want: "src/app/favicon.ico"},
		{name: "png then ico", files: map[string][]byte{"apple-touch-icon.png": pngIcon, "favicon.ico": icoIcon}, want: "apple-touch-icon.png"},
		{name: "not an image named favicon.png", files: map[string][]byte{"favicon.png": notIcon, "favicon.ico": icoIcon}, want: "favicon.ico"},
		{name: "too big", files: map[string][]byte{"favicon.svg": big, "icon.svg": svgIcon}, want: "icon.svg"},
		{name: "exactly the limit", files: map[string][]byte{"favicon.svg": edge}, want: "favicon.svg"},
		{name: "empty file", files: map[string][]byte{"favicon.svg": {}, "icon.png": pngIcon}, want: "icon.png"},
		{
			name: "directory named favicon.svg", files: map[string][]byte{"favicon.svg/inner.svg": svgIcon, "icon.svg": iconOf("file")},
			want: "icon.svg",
		},
		{
			name: "symlink out of the repo", files: map[string][]byte{"icon.png": pngIcon},
			setup: func(t *testing.T, repo string) {
				out := filepath.Join(filepath.Dir(repo), "outside.svg")
				write(t, out, svgIcon)
				symlink(t, out, filepath.Join(repo, "favicon.svg"))
			},
			want: "icon.png",
		},
		{
			name: "symlinked folder out of the repo", files: map[string][]byte{"icon.png": pngIcon},
			setup: func(t *testing.T, repo string) {
				out := filepath.Join(filepath.Dir(repo), "elsewhere")
				write(t, filepath.Join(out, "favicon.svg"), svgIcon)
				symlink(t, out, filepath.Join(repo, "public"))
			},
			want: "icon.png",
		},
		{
			name: "symlink inside the repo", files: map[string][]byte{"assets/logo.svg": svgIcon},
			setup: func(t *testing.T, repo string) {
				symlink(t, filepath.Join("assets", "logo.svg"), filepath.Join(repo, "favicon.svg"))
			},
			want: "assets/logo.svg",
		},
		{
			name: "link in index.html", files: map[string][]byte{
				"index.html":    page(`<link rel="stylesheet" href="garden.css"><link rel="icon" href="img/leaf.svg">`),
				"img/leaf.svg":  svgIcon,
				"img/other.svg": iconOf("other"),
			},
			want: "img/leaf.svg",
		},
		{
			name: "step one before step two", files: map[string][]byte{
				"index.html": page(`<link rel="icon" href="img/leaf.svg">`), "img/leaf.svg": iconOf("linked"), "assets/icon.png": pngIcon,
			},
			want: "assets/icon.png",
		},
		{
			name: "relative to the page's folder", files: map[string][]byte{
				"public/index.html": page(`<link rel="icon" href="leaf.png">`), "public/leaf.png": pngIcon, "leaf.png": iconOf("root"),
			},
			want: "public/leaf.png",
		},
		{
			name: "../ that stays inside", files: map[string][]byte{
				"src/index.html": page(`<link rel="icon" href="../brand/leaf.svg">`), "brand/leaf.svg": svgIcon,
			},
			want: "brand/leaf.svg",
		},
		{
			name: "../ out of the repo", files: map[string][]byte{
				"index.html": page(`<link rel="icon" href="../outside.svg"><link rel="icon" href="ok.png">`), "ok.png": pngIcon,
			},
			setup: func(t *testing.T, repo string) { write(t, filepath.Join(filepath.Dir(repo), "outside.svg"), svgIcon) },
			want:  "ok.png",
		},
		{
			name: "scheme, data: URL and leading slash", files: map[string][]byte{
				"index.html": page(`<link rel="icon" href="https://example.com/leaf.svg">
<link rel="icon" href="data:image/svg+xml,%3Csvg%3E%3C/svg%3E">
<link rel="icon" href="/leaf.svg">
<link rel="icon" href="//example.com/leaf.svg">
<link rel="icon" href="leaf.png">`),
				"leaf.svg": iconOf("root but linked with a slash"), "leaf.png": pngIcon,
			},
			want: "leaf.png",
		},
		{
			name: "query and fragment", files: map[string][]byte{
				"index.html": page(`<link rel="icon" href="leaf.svg?v=2#top">`), "leaf.svg": svgIcon,
			},
			want: "leaf.svg",
		},
		{
			name: "shortcut icon, upper case, unquoted", files: map[string][]byte{
				"index.html": page(`<LINK REL="Shortcut Icon" HREF=leaf.ico>`), "leaf.ico": icoIcon,
			},
			want: "leaf.ico",
		},
		{
			name: "apple-touch-icon in single quotes", files: map[string][]byte{
				"web/index.html": page(`<link href='touch.png' rel='apple-touch-icon' sizes="180x180" />`), "web/touch.png": pngIcon,
			},
			want: "web/touch.png",
		},
		{
			name: "rel that only contains the letters", files: map[string][]byte{
				"index.html": page(`<link rel="iconic" href="a.svg"><link rel="mask-icon" href="b.svg">`), "a.svg": svgIcon, "b.svg": svgIcon,
			},
		},
		{
			name: "commented link skipped", files: map[string][]byte{
				"index.html": page(`<!-- <link rel="icon" href="old.svg"> --><link rel="icon" href="new.svg">`), "old.svg": iconOf("old"), "new.svg": iconOf("new"),
			},
			want: "new.svg",
		},
		{
			name: "pages in order", files: map[string][]byte{
				"index.html": page(`<link rel="icon" href="gone.svg">`), "public/index.html": page(`<link rel="icon" href="here.svg">`),
				"public/here.svg": svgIcon,
			},
			want: "public/here.svg",
		},
		{
			name: "link past 256 KiB not read", files: map[string][]byte{
				"index.html": append(bytes.Repeat([]byte(" "), maxIconPage), page(`<link rel="icon" href="leaf.svg">`)...), "leaf.svg": svgIcon,
			},
		},
		{
			name: "monorepo apps", files: map[string][]byte{
				"apps/web/public/favicon.svg": svgIcon, "packages/ui/favicon.svg": iconOf("package"), "apps/README.md": notIcon,
			},
			want: "apps/web/public/favicon.svg",
		},
		{
			name: "monorepo folders in byte order", files: map[string][]byte{
				"apps/b/favicon.svg": iconOf("b"), "apps/a/icons/icon.png": pngIcon,
			},
			want: "apps/a/icons/icon.png",
		},
		{
			name: "monorepo after the pages", files: map[string][]byte{
				"apps/web/favicon.svg": iconOf("app"), "index.html": page(`<link rel="icon" href="leaf.png">`), "leaf.png": pngIcon,
			},
			want: "leaf.png",
		},
		{
			name: "monorepo two levels down not read", files: map[string][]byte{"apps/web/src/favicon.svg": svgIcon},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			repo := filepath.Join(t.TempDir(), "garden")
			if err := os.Mkdir(repo, 0o755); err != nil {
				t.Fatal(err)
			}
			for name, b := range c.files {
				write(t, filepath.Join(repo, filepath.FromSlash(name)), b)
			}
			if c.setup != nil {
				c.setup(t, repo)
			}
			got := FindIcon(repo)
			var want []byte
			if c.want != "" {
				want = c.files[c.want]
			}
			if !bytes.Equal(got, want) || (want == nil) != (got == nil) {
				t.Errorf("FindIcon = %.60q; want %.60q (%s)", got, want, c.want)
			}
		})
	}
}

// TestFindIconMissingRepo: a repo folder that is gone has no icon.
func TestFindIconMissingRepo(t *testing.T) {
	if got := FindIcon(filepath.Join(t.TempDir(), "gone")); got != nil {
		t.Errorf("FindIcon = %q", got)
	}
}

func write(t *testing.T, path string, b []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func symlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skip("cannot make a symbolic link:", err)
	}
}

// iconsWorld is a bridge over one repo, with sync off, and a clock the test
// moves.
type iconsWorld struct {
	*world
	b    *Bridge
	dir  string
	now  time.Time
	log  *bytes.Buffer
	name string
}

func newIconsWorld(t *testing.T) *iconsWorld {
	t.Helper()
	w := newWorld(t, false)
	iw := &iconsWorld{world: w, now: time.Date(2030, 3, 1, 9, 0, 0, 0, time.UTC), log: &bytes.Buffer{}, name: "projects/garden"}
	iw.dir = w.repo(iw.name, false)
	iw.restart()
	return iw
}

// restart starts a new bridge on the same hub, as a new process would.
func (iw *iconsWorld) restart() {
	iw.b = iw.start()
	iw.b.Now = func() time.Time { return iw.now }
	iw.b.Log = slog.New(slog.NewTextHandler(iw.log, nil))
}

func (iw *iconsWorld) after(d time.Duration) {
	iw.t.Helper()
	iw.now = iw.now.Add(d)
	iw.tick(iw.b)
}

func (iw *iconsWorld) setIcon(b []byte) {
	iw.t.Helper()
	path := filepath.Join(iw.dir, "favicon.svg")
	if b == nil {
		if err := os.Remove(path); err != nil {
			iw.t.Fatal(err)
		}
		return
	}
	write(iw.t, path, b)
}

func (iw *iconsWorld) puts() int { return iw.hub.IconPuts(us, iw.name) }

func (iw *iconsWorld) held() []byte {
	b, _ := iw.hub.Icon(us, iw.name)
	return b
}

// TestIconsSent: after the report the repo's icon goes once, it is looked
// at again at most once every IconsEvery, and sent again only when it
// changes.
func TestIconsSent(t *testing.T) {
	iw := newIconsWorld(t)
	iw.setIcon(iconOf("one"))
	iw.tick(iw.b)
	if iw.puts() != 1 || !bytes.Equal(iw.held(), iconOf("one")) {
		t.Fatalf("first round: %d puts, hub holds %q", iw.puts(), iw.held())
	}
	iw.setIcon(iconOf("two"))
	iw.after(IconsEvery / 2)
	if iw.puts() != 1 {
		t.Errorf("looked again within IconsEvery: %d puts", iw.puts())
	}
	iw.after(IconsEvery / 2)
	if iw.puts() != 2 || !bytes.Equal(iw.held(), iconOf("two")) {
		t.Errorf("changed icon: %d puts, hub holds %q", iw.puts(), iw.held())
	}
	iw.after(IconsEvery)
	iw.after(IconsEvery)
	if iw.puts() != 2 {
		t.Errorf("unchanged icon sent again: %d puts", iw.puts())
	}
	iw.setIcon(nil)
	iw.after(IconsEvery)
	if _, ok := iw.hub.Icon(us, iw.name); ok || iw.puts() != 3 {
		t.Errorf("icon gone: %d puts, hub still holds one: %v", iw.puts(), ok)
	}
}

// TestIconsResentHourly: an unchanged icon goes again once an hour, so a
// hub restored from a backup that lacks it gets it back.
func TestIconsResentHourly(t *testing.T) {
	iw := newIconsWorld(t)
	iw.setIcon(iconOf("one"))
	iw.tick(iw.b)
	iw.hub.DropIcon(us, iw.name) // the hub lost it
	for range IconsResend - 1 {
		iw.after(IconsEvery)
	}
	if iw.puts() != 1 {
		t.Fatalf("resent within the hour: %d puts", iw.puts())
	}
	iw.after(IconsEvery)
	if iw.puts() != 2 || !bytes.Equal(iw.held(), iconOf("one")) {
		t.Errorf("after an hour: %d puts, hub holds %q", iw.puts(), iw.held())
	}
}

// TestIconsAfterStart: a new bridge sends every repo's icon once, an empty
// body for a repo with none, so an icon removed while it was down goes.
func TestIconsAfterStart(t *testing.T) {
	iw := newIconsWorld(t)
	iw.setIcon(svgIcon)
	iw.tick(iw.b)
	iw.setIcon(nil)
	iw.restart()
	iw.after(time.Second)
	if _, ok := iw.hub.Icon(us, iw.name); ok || iw.puts() != 2 {
		t.Errorf("after the start: %d puts, hub still holds one: %v", iw.puts(), ok)
	}
	iw.after(IconsEvery)
	if iw.puts() != 2 {
		t.Errorf("no icon sent again: %d puts", iw.puts())
	}
}

// TestIconsOldHub: a hub without the icons route is logged once and sent no
// icons for an hour, and the round carries on without a new hello.
func TestIconsOldHub(t *testing.T) {
	iw := newIconsWorld(t)
	iw.setIcon(svgIcon)
	iw.hub.SetNoIcons(true)
	iw.tick(iw.b)
	iw.after(IconsEvery)
	iw.after(30 * time.Minute)
	if iw.puts() != 1 {
		t.Errorf("within the hour: %d puts, want 1", iw.puts())
	}
	iw.after(30 * time.Minute)
	if iw.puts() != 2 {
		t.Errorf("after the hour: %d puts, want 2", iw.puts())
	}
	if n := strings.Count(iw.log.String(), "does not take icons"); n != 1 {
		t.Errorf("logged %d times:\n%s", n, iw.log)
	}
	if n := len(iw.hub.Hellos()); n != 1 {
		t.Errorf("%d hellos, want 1", n)
	}
	iw.hub.SetNoIcons(false)
	iw.after(IconsBackoff)
	if !bytes.Equal(iw.held(), svgIcon) {
		t.Errorf("an upgraded hub holds %q", iw.held())
	}
}

// TestIconsConflict: a 409 forgets what was sent, so the repos are reported
// again and the icon goes after that report.
func TestIconsConflict(t *testing.T) {
	iw := newIconsWorld(t)
	iw.setIcon(iconOf("one"))
	iw.tick(iw.b)
	// The hub loses the repo list behind the bridge's back, and the icon
	// changes; the bridge looks again before its next report is due.
	iw.hub.ForgetRepos(us)
	iw.setIcon(iconOf("two"))
	iw.b.iconStates[iw.name].lookedAt = time.Time{}
	iw.after(time.Minute)
	if iw.puts() != 2 || iw.hub.Repos(us) != nil {
		t.Fatalf("after the conflict: %d puts, repos %+v", iw.puts(), iw.hub.Repos(us))
	}
	iw.after(time.Second)
	if len(iw.hub.Repos(us)) != 1 || !bytes.Equal(iw.held(), iconOf("two")) {
		t.Errorf("not sent again after the report: repos %+v, hub holds %q", iw.hub.Repos(us), iw.held())
	}
	if n := len(iw.hub.Hellos()); n != 1 {
		t.Errorf("%d hellos, want 1", n)
	}
}

// TestIconsRefused: an icon the hub refuses is logged once and never sent
// again, even after another icon came between.
func TestIconsRefused(t *testing.T) {
	iw := newIconsWorld(t)
	iw.hub.RefuseIcons(func(b []byte) bool { return bytes.Equal(b, iconOf("bad")) })
	iw.setIcon(iconOf("bad"))
	iw.tick(iw.b)
	iw.after(IconsEvery)
	iw.after(IconsEvery)
	if iw.puts() != 1 || iw.held() != nil {
		t.Errorf("refused icon: %d puts, hub holds %q", iw.puts(), iw.held())
	}
	if n := strings.Count(iw.log.String(), "refused this repo's icon"); n != 1 {
		t.Errorf("logged %d times:\n%s", n, iw.log)
	}
	iw.setIcon(iconOf("good"))
	iw.after(IconsEvery)
	if iw.puts() != 2 || !bytes.Equal(iw.held(), iconOf("good")) {
		t.Errorf("changed icon: %d puts, hub holds %q", iw.puts(), iw.held())
	}
	iw.setIcon(iconOf("bad"))
	iw.after(IconsEvery)
	if iw.puts() != 2 {
		t.Errorf("a refused icon was sent again: %d puts", iw.puts())
	}
}

// TestIconsUnsent: a 5xx or a 401 leaves the icon unsent, fails the round
// as other hub errors do, and the icon goes at the next look.
func TestIconsUnsent(t *testing.T) {
	for _, status := range []int{http.StatusServiceUnavailable, http.StatusUnauthorized} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			iw := newIconsWorld(t)
			iw.setIcon(svgIcon)
			iw.hub.FailIcons(status)
			var h *HTTPError
			if err := iw.b.Tick(iw.ctx, true); !errors.As(err, &h) || h.Status != status {
				t.Fatalf("Tick = %v; want the hub's %d", err, status)
			}
			iw.hub.FailIcons(0)
			iw.after(time.Minute)
			if iw.puts() != 1 || iw.held() != nil {
				t.Errorf("before the next look: %d puts, hub holds %q", iw.puts(), iw.held())
			}
			iw.after(IconsEvery)
			if !bytes.Equal(iw.held(), svgIcon) {
				t.Errorf("at the next look: %d puts, hub holds %q", iw.puts(), iw.held())
			}
		})
	}
}

// TestFakeHubIcons: the fake hub's icons route keeps the real hub's rules.
func TestFakeHubIcons(t *testing.T) {
	iw := newIconsWorld(t)
	iw.tick(iw.b)
	ctx := context.Background()
	status := func(err error) int {
		var h *HTTPError
		if errors.As(err, &h) {
			return h.Status
		}
		if err != nil {
			t.Fatal(err)
		}
		return http.StatusNoContent
	}
	tooBig := append(append([]byte{}, svgIcon...), bytes.Repeat([]byte(" "), api.MaxIconBytes)...)
	for _, c := range []struct {
		repo string
		icon []byte
		want int
	}{
		{iw.name, tooBig, http.StatusRequestEntityTooLarge},
		{iw.name, notIcon, http.StatusBadRequest},
		{"projects/shed", svgIcon, http.StatusConflict},
		{iw.name, pngIcon, http.StatusNoContent},
	} {
		if got := status(iw.b.Hub.PutIcon(ctx, c.repo, c.icon)); got != c.want {
			t.Errorf("PUT %d bytes to %s = %d; want %d", len(c.icon), c.repo, got, c.want)
		}
	}
	if !bytes.Equal(iw.held(), pngIcon) {
		t.Errorf("hub holds %q", iw.held())
	}
	if got := status(iw.b.Hub.PutIcon(ctx, iw.name, nil)); got != http.StatusNoContent || iw.held() != nil {
		t.Errorf("empty body: %d, hub holds %q", got, iw.held())
	}
	iw.setIcon(svgIcon)
	iw.after(IconsEvery)
	if err := iw.b.Hub.PutRepos(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := iw.hub.Icon(us, iw.name); ok {
		t.Error("the icon outlived its repo row")
	}
}
