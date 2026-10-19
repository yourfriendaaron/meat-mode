package hub

import (
	"fmt"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// guard holds the request checks for the app API and the app's pages. The
// app API has no credentials of its own, so it must refuse requests another
// site makes from the user's browser (CSRF) and requests that reach it under
// a name an attacker controls (DNS rebinding). The bridge API has bearer
// tokens and /healthz is public, so neither is checked for those. Every
// request, those included, must first come from the private network.
type guard struct {
	net          *netGuard
	hosts        map[string]bool // allowed Host names, lower case, no port
	publicOrigin string          // scheme://host[:port] of MEAT_MODE_PUBLIC_URL, or ""
}

func newGuard(cfg Config) *guard {
	g := &guard{net: newNetGuard(cfg), hosts: map[string]bool{"localhost": true, "127.0.0.1": true, "::1": true}}
	if u, err := url.Parse(cfg.PublicURL); err == nil && u.Host != "" {
		g.hosts[strings.ToLower(u.Hostname())] = true
		g.publicOrigin = strings.ToLower(u.Scheme + "://" + u.Host)
	}
	for h := range strings.SplitSeq(cfg.AllowedHosts, ",") {
		if h = hostOnly(strings.TrimSpace(h)); h != "" {
			g.hosts[h] = true
		}
	}
	return g
}

// hostOnly lower-cases a Host value and strips its port and IPv6 brackets.
func hostOnly(h string) string {
	if host, _, err := net.SplitHostPort(h); err == nil {
		h = host
	}
	return strings.ToLower(strings.TrimSuffix(strings.TrimPrefix(h, "["), "]"))
}

// check reports whether the request may go on, writing the refusal when not.
func (g *guard) check(w http.ResponseWriter, r *http.Request) bool {
	if !g.net.check(w, r) {
		return false
	}
	p := r.URL.Path
	if p == "/healthz" || strings.HasPrefix(p, "/bridge/") {
		return true
	}
	isAPI := isAPIPath(p)
	if host := hostOnly(r.Host); !g.hosts[host] {
		msg := fmt.Sprintf("This hub does not answer to the name %q. If that is how you open the app, "+
			"add it to MEAT_MODE_ALLOWED_HOSTS (or set MEAT_MODE_PUBLIC_URL to it) and restart the hub.", host)
		if isAPI {
			writeError(w, http.StatusMisdirectedRequest, msg)
		} else {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(http.StatusMisdirectedRequest)
			fmt.Fprintln(w, msg)
		}
		return false
	}
	if isAPI && crossSite(r.Header.Get("Sec-Fetch-Site")) {
		// Even a GET from another site costs something: an event stream
		// holds one of a few slots for as long as the page stays open.
		writeError(w, http.StatusForbidden, "cross-site request refused")
		return false
	}
	o := r.Header.Get("Origin")
	if isAPI && o != "" && !g.originOK(o, r) {
		// Browsers without Sec-Fetch-Site still send Origin on a
		// cross-origin fetch or EventSource.
		writeError(w, http.StatusForbidden, "cross-origin request refused")
		return false
	}
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		return true
	}
	if o != "" && !g.originOK(o, r) {
		writeError(w, http.StatusForbidden, "cross-origin request refused")
		return false
	}
	if isAPI && !isJSON(r.Header.Get("Content-Type")) {
		writeError(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
		return false
	}
	return true
}

// isAPIPath reports whether p is in the app API: /api/... or bare /api.
func isAPIPath(p string) bool {
	return p == "/api" || strings.HasPrefix(p, "/api/")
}

// originOK reports whether origin is the request's own origin or the public
// URL's.
func (g *guard) originOK(origin string, r *http.Request) bool {
	origin = strings.ToLower(origin)
	if g.publicOrigin != "" && origin == g.publicOrigin {
		return true
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if p := r.Header.Get("X-Forwarded-Proto"); p != "" {
		p, _, _ = strings.Cut(p, ",")
		scheme = strings.ToLower(strings.TrimSpace(p))
	}
	return origin == scheme+"://"+strings.ToLower(r.Host)
}

// crossSite reports whether a Sec-Fetch-Site value names another origin.
// "none" is the user's own navigation, such as a notification click; an
// absent header is an older browser or a non-browser client.
func crossSite(v string) bool {
	return v != "" && v != "same-origin" && v != "none"
}

// isJSON accepts application/json with at most a charset parameter.
func isJSON(ct string) bool {
	mt, params, err := mime.ParseMediaType(ct)
	if err != nil || mt != "application/json" {
		return false
	}
	for k := range params {
		if k != "charset" {
			return false
		}
	}
	return true
}
