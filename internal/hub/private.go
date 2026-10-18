package hub

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/netguard"
)

// forwardHeaders name the client's address in one value each, or in a
// comma-separated list. Forwarded has its own syntax and is read apart.
var forwardHeaders = []string{"X-Forwarded-For", "X-Real-IP", "CF-Connecting-IP", "True-Client-IP"}

// netLogEvery is how often the hub logs a refusal of one kind.
const netLogEvery = time.Minute

// trustTestPeer admits httptest's default peer, 192.0.2.1, as the direct
// peer, so the package's handler tests need not each set RemoteAddr. Only
// tests set it.
var trustTestPeer bool

// netGuard refuses requests from outside the allowed set (docs/api.md,
// Private network only). It only refuses: a forged header can make a
// request look more public, never less, so it trusts no proxy.
type netGuard struct {
	set netguard.Set
	log *slog.Logger
	now func() time.Time

	mu     sync.Mutex
	logged map[string]time.Time // kind of refusal: when it was last logged
}

func newNetGuard(cfg Config) *netGuard {
	// Validate has refused a bad entry already; a server built without
	// it keeps to the private set.
	set, _ := netguard.ParseTrusted(cfg.TrustedNetworks)
	return &netGuard{set: set, log: slog.Default(), now: time.Now, logged: map[string]time.Time{}}
}

// check reports whether the request may go on, writing the refusal when not.
func (n *netGuard) check(w http.ResponseWriter, r *http.Request) bool {
	kind, why := n.refusal(r)
	if kind == "" {
		return true
	}
	n.logRefusal(kind, why, r)
	msg := "This hub answers only on a private network. It refused this request because " + why + "."
	if isAPIPath(r.URL.Path) {
		writeError(w, http.StatusForbidden, msg)
	} else {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprintln(w, msg)
	}
	return false
}

// refusal names what puts the request outside the allowed set: its kind,
// for the log's rate limit, and a reason for the message. kind is "" when
// the request may go on.
func (n *netGuard) refusal(r *http.Request) (kind, why string) {
	if _, ok := r.Header["Tailscale-Funnel-Request"]; ok {
		return "Tailscale-Funnel-Request", "it came through Tailscale Funnel, from the internet"
	}
	for _, name := range forwardHeaders {
		for _, line := range r.Header.Values(name) {
			for v := range strings.SplitSeq(line, ",") {
				if why := n.judge(name, v); why != "" {
					return name, why
				}
			}
		}
	}
	for _, line := range r.Header.Values("Forwarded") {
		for elem := range strings.SplitSeq(line, ",") {
			for pair := range strings.SplitSeq(elem, ";") {
				k, v, _ := strings.Cut(pair, "=")
				if !strings.EqualFold(strings.TrimSpace(k), "for") {
					continue
				}
				if why := n.judge("Forwarded for=", v); why != "" {
					return "Forwarded", why
				}
			}
		}
	}
	a, ok := netguard.ParseAddr(r.RemoteAddr)
	if trustTestPeer && ok && a == netip.AddrFrom4([4]byte{192, 0, 2, 1}) {
		return "", ""
	}
	if !ok {
		return "peer", fmt.Sprintf("its peer address %s is not an IP address", quoteShort(r.RemoteAddr))
	}
	if !n.set.Contains(a) {
		return "peer", fmt.Sprintf("it came from %s, outside the private network", netguard.Normalize(a))
	}
	return "", ""
}

// judge returns why one value of a forwarding header is refused, or "".
// An empty entry, as a trailing comma leaves, names no one and passes.
func (n *netGuard) judge(header, v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	a, ok := netguard.ParseAddr(v)
	if !ok {
		return fmt.Sprintf("%s holds %s, which is not an IP address", header, quoteShort(v))
	}
	if !n.set.Contains(a) {
		return fmt.Sprintf("%s names %s, outside the private network", header, netguard.Normalize(a))
	}
	return ""
}

// logRefusal logs a refusal at most once every netLogEvery for each kind.
func (n *netGuard) logRefusal(kind, why string, r *http.Request) {
	now := n.now()
	n.mu.Lock()
	last, seen := n.logged[kind]
	if seen && now.Sub(last) < netLogEvery {
		n.mu.Unlock()
		return
	}
	n.logged[kind] = now
	n.mu.Unlock()
	n.log.Warn("refused a request from outside the private network: "+why,
		"kind", kind, "method", r.Method, "path", r.URL.Path, "peer", r.RemoteAddr)
}

// quoteShort quotes s for a message, cut to 64 bytes, since it comes from
// the request.
func quoteShort(s string) string {
	if len(s) > 64 {
		s = s[:64] + "..."
	}
	return fmt.Sprintf("%q", s)
}
