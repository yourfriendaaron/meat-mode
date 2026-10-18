package hub

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

// setupRoutes registers the setup wizard's routes (docs/api.md, Setup).
func (s *Server) setupRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /api/setup", s.handleSetup)
	m.HandleFunc("POST /api/setup", s.handleRecordSetup)
	m.HandleFunc("GET /api/setup/status", s.handleSetupStatus)
	m.HandleFunc("POST /api/setup/test-event", s.handleTestEvent)
}

func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	status, err := s.store.SetupStatus()
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	h, err := s.store.Settings()
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	out := api.Setup{
		Status:        status,
		User:          s.nameFrom(h),
		TimeZone:      h.TimeZone,
		Password:      "none",
		TailscaleOnly: h.TailscaleOnly,
	}
	switch {
	case s.cfg.PasswordHash != "":
		out.Password = "env"
	case h.PasswordHash != "":
		out.Password = "app"
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleRecordSetup(w http.ResponseWriter, r *http.Request) {
	var body api.SetupRecord
	if !readJSON(w, r, maxBody, &body) {
		return
	}
	if err := s.store.RecordSetup(body.Status); err != nil {
		s.storeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleSetupStatus reports what the connection check needs from the hub's
// side (docs/api.md, Setup).
func (s *Server) handleSetupStatus(w http.ResponseWriter, r *http.Request) {
	h, err := s.store.Settings()
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	out := api.SetupStatus{
		Login:       "none",
		HostMatches: s.hostMatchesPublicURL(r),
		PublicURL:   s.cfg.PublicURL,
		PushKeys:    s.push != nil && s.push.PublicKey() != "",
		Ntfy:        s.cfg.NtfyURL != "",
		Time:        s.now().UTC(),

		PublicDNS:         s.publicDNS(),
		TailscaleIdentity: r.Header.Get("Tailscale-User-Login") != "",
	}
	switch {
	case s.cfg.PasswordHash != "":
		out.Login = "environment"
	case h.PasswordHash != "":
		out.Login = "app"
	}
	writeJSON(w, http.StatusOK, out)
}

// hostMatchesPublicURL reports whether the request came in under the host
// of MEAT_MODE_PUBLIC_URL. The guard already refused a name the hub does not
// allow, so false means an allowed name that is not the public address.
func (s *Server) hostMatchesPublicURL(r *http.Request) bool {
	u, err := url.Parse(s.cfg.PublicURL)
	if err != nil || u.Host == "" {
		return false
	}
	return canonicalHost(r.Host, "") == canonicalHost(u.Host, u.Scheme)
}

// canonicalHost lower-cases host and drops a port that is the default for
// scheme. An empty scheme leaves the port alone, which suits a request's
// Host, whose scheme the hub may not see behind a proxy.
func canonicalHost(host, scheme string) string {
	host = strings.ToLower(host)
	for _, d := range []struct{ scheme, suffix string }{{"https", ":443"}, {"http", ":80"}} {
		if scheme == d.scheme {
			host = strings.TrimSuffix(host, d.suffix)
		}
	}
	return host
}

// handleTestEvent sends a change event down every open event stream, so the
// connection check can see whether a stream delivers events or a proxy
// holds them back.
func (s *Server) handleTestEvent(w http.ResponseWriter, r *http.Request) {
	s.events.publish()
	w.WriteHeader(http.StatusNoContent)
}
